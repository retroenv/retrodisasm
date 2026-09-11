package romconfig

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/retroenv/retrogolib/config"
	"github.com/retroenv/retrogolib/set"
)

const (
	sectionBlankLines     = "blank_lines"
	sectionBytes          = "bytes"
	sectionCode           = "code"
	sectionComments       = "comments"
	sectionCommentsBefore = "comments_before"
	sectionConstants      = "constants"
	sectionData           = "data"
	sectionLabels         = "labels"
	sectionOperands       = "operands"
	sectionROM            = "rom"
	sectionSymbolGroups   = "symbol_groups"
	sectionSymbols        = "symbols"
	sectionVariables      = "variables"
	sectionVectors        = "vectors"
	sectionWords          = "words"
)

var sections = set.NewFromSlice([]string{
	sectionBlankLines, sectionBytes, sectionCode, sectionComments, sectionCommentsBefore, sectionConstants, sectionData,
	sectionLabels, sectionOperands, sectionROM, sectionSymbolGroups, sectionSymbols, sectionVariables, sectionVectors,
	sectionWords,
})

var parserOptions = config.Options{
	CaseSensitive:         true,
	RawValues:             true,
	CommentPrefixes:       ";#",
	InlineComments:        true,
	LiteralSections:       []string{sectionComments, sectionCommentsBefore},
	AllowRepeatedSections: true,
}

// Load reads and validates an INI file.
func Load(filename string) (*Config, error) {
	input, err := config.Open(filename, parserOptions)
	if err != nil {
		return nil, fmt.Errorf("opening INI file: %w", err)
	}

	cfg, err := parseConfig(input)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return cfg, nil
}

// Parse reads annotations, reporting malformed entries with their line number.
func Parse(reader io.Reader) (*Config, error) {
	input, err := config.Parse(reader, parserOptions)
	if err != nil {
		return nil, fmt.Errorf("reading INI: %w", err)
	}
	return parseConfig(input)
}

type parser struct {
	cfg   *Config
	names map[string]uint16
}

func (pr *parser) validateGroups() error {
	seen := set.New[string]()
	definitions := set.New[string]()
	for name := range pr.cfg.Symbols {
		definitions.Add(name)
	}
	for _, entries := range []map[uint16]string{pr.cfg.Constants, pr.cfg.Variables} {
		for _, name := range entries {
			definitions.Add(name)
		}
	}
	for _, group := range pr.cfg.SymbolGroups {
		for _, name := range group.Names {
			if !definitions.Contains(name) {
				return fmt.Errorf("unknown group symbol %q", name)
			}
			if seen.Contains(name) {
				return fmt.Errorf("duplicate group symbol %q", name)
			}
			seen.Add(name)
		}
	}
	return nil
}

func (pr *parser) parseEntry(section, key, value string) error {
	if section == "" {
		return errors.New("expected key = value inside a section")
	}
	if key == "" || value == "" {
		return errors.New("empty key or value")
	}

	switch section {
	case sectionROM:
		return pr.parseChecksum(key, value)
	case sectionVectors:
		return pr.parseVector(key, value)
	case sectionCode, sectionData:
		return pr.parseRange(section, key, value)
	case sectionSymbols:
		return pr.parseSymbol(key, value)
	case sectionBytes, sectionWords:
		return pr.parseDataExpressions(section, key, value)
	case sectionBlankLines, sectionSymbolGroups:
		return pr.parsePresentation(section, key, value)
	default:
		return pr.parseAnnotation(section, key, value)
	}
}

func (pr *parser) parsePresentation(section, key, value string) error {
	if section == sectionSymbolGroups {
		names := strings.Split(value, ",")
		for i, name := range names {
			names[i] = strings.TrimSpace(name)
			if !validName(names[i]) {
				return fmt.Errorf("invalid group symbol %q", names[i])
			}
		}
		pr.cfg.SymbolGroups = append(pr.cfg.SymbolGroups, SymbolGroup{Heading: key, Names: names})
		return nil
	}
	address, err := parseAddress(key)
	if err != nil {
		return err
	}
	if _, exists := pr.cfg.BlankLines[address]; exists {
		return fmt.Errorf("duplicate blank-line address $%04X", address)
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 || count > 8 {
		return errors.New("blank-line count must be between 0 and 8")
	}
	pr.cfg.BlankLines[address] = count
	return nil
}

func (pr *parser) parseDataExpressions(section, key, value string) error {
	address, err := parseAddress(key)
	if err != nil {
		return err
	}
	entries := pr.cfg.Bytes
	if section == sectionWords {
		entries = pr.cfg.Words
	}
	if _, ok := entries[address]; ok {
		return fmt.Errorf("duplicate data expression at $%04X", address)
	}
	if strings.HasPrefix(value, "@") {
		count, err := parseAddress(value[1:])
		if err != nil || count == 0 {
			return fmt.Errorf("invalid data element count %q", value)
		}
		entries[address] = Data{Count: count}
		return nil
	}
	expressions := strings.Split(value, ",")
	for i, expression := range expressions {
		expressions[i] = strings.TrimSpace(expression)
		if expressions[i] == "" {
			return fmt.Errorf("empty data expression at $%04X", address)
		}
	}
	entries[address] = Data{Expressions: expressions}
	return nil
}

func (pr *parser) parseSymbol(name, value string) error {
	if !validName(name) {
		return fmt.Errorf("invalid symbol name %q", name)
	}
	address, err := parseAddress(value)
	if err != nil {
		return err
	}
	if previous, ok := pr.names[strings.ToLower(name)]; ok && previous != address {
		return fmt.Errorf("symbol %q already names $%04X", name, previous)
	}
	pr.names[strings.ToLower(name)] = address
	pr.cfg.Symbols[name] = address
	return nil
}

func (pr *parser) parseChecksum(key, value string) error {
	if key != "prg_crc32" && key != "chr_crc32" {
		return fmt.Errorf("unknown ROM key %q", key)
	}
	value = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(value), "0x"), "$")
	checksum, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return fmt.Errorf("invalid CRC32: %w", err)
	}
	crc := uint32(checksum)
	if key == "prg_crc32" {
		pr.cfg.PRGCRC32 = &crc
	} else {
		pr.cfg.CHRCRC32 = &crc
	}
	return nil
}

func (pr *parser) parseVector(key, value string) error {
	var vector Vector
	switch key {
	case "nmi":
		vector = NMI
	case "reset":
		vector = Reset
	case "irq":
		vector = IRQ
	default:
		return fmt.Errorf("unknown vector %q", key)
	}
	entry := VectorValue{Name: value}
	if !validName(value) {
		address, err := parseAddress(value)
		if err != nil {
			return fmt.Errorf("invalid vector value %q", value)
		}
		entry = VectorValue{Address: &address}
	}
	pr.cfg.Vectors[vector] = entry
	return nil
}

func (pr *parser) parseRange(section, key, value string) error {
	start, err := parseAddress(key)
	if err != nil {
		return err
	}
	end, err := parseAddress(value)
	if err != nil {
		return err
	}
	if end < start {
		return errors.New("range end precedes start")
	}
	rng := Range{Start: start, End: end}
	for _, ranges := range [][]Range{pr.cfg.Code, pr.cfg.Data} {
		for _, existing := range ranges {
			if start <= existing.End && end >= existing.Start {
				return errors.New("overlapping code/data ranges")
			}
		}
	}
	if section == sectionCode {
		pr.cfg.Code = append(pr.cfg.Code, rng)
	} else {
		pr.cfg.Data = append(pr.cfg.Data, rng)
	}
	return nil
}

func (pr *parser) parseAnnotation(section, key, value string) error {
	if section == sectionConstants || section == sectionVariables {
		key, value = value, key
	}
	address, err := parseAddress(key)
	if err != nil {
		return err
	}
	if !isCommentSection(section) && section != sectionOperands {
		if err := pr.registerName(value, address); err != nil {
			return err
		}
	}
	entries := pr.annotationEntries(section)
	if entries == nil {
		return fmt.Errorf("unknown section %q", section)
	}
	if _, exists := entries[address]; exists {
		return fmt.Errorf("duplicate address $%04X in [%s]", address, section)
	}
	entries[address] = value
	return nil
}

func (pr *parser) annotationEntries(section string) map[uint16]string {
	switch section {
	case sectionConstants:
		return pr.cfg.Constants
	case sectionVariables:
		return pr.cfg.Variables
	case sectionLabels:
		return pr.cfg.Labels
	case sectionComments:
		return pr.cfg.Comments
	case sectionCommentsBefore:
		return pr.cfg.CommentsBefore
	case sectionOperands:
		return pr.cfg.Operands
	default:
		return nil
	}
}

func (pr *parser) registerName(name string, address uint16) error {
	if !validName(name) {
		return fmt.Errorf("invalid symbol name %q", name)
	}
	if previous, ok := pr.names[strings.ToLower(name)]; ok && previous != address {
		return fmt.Errorf("symbol %q already names $%04X", name, previous)
	}
	pr.names[strings.ToLower(name)] = address
	return nil
}

func parseConfig(input *config.Config) (*Config, error) {
	for section := range input.Sections() {
		if !sections.Contains(section.Name) {
			return nil, fmt.Errorf("line %d: unknown section %q", section.Line, section.Name)
		}
	}
	pr := parser{
		cfg: &Config{
			Vectors:   make(map[Vector]VectorValue),
			Constants: make(map[uint16]string), Variables: make(map[uint16]string),
			Labels: make(map[uint16]string), Comments: make(map[uint16]string),
			Symbols:        make(map[string]uint16),
			CommentsBefore: make(map[uint16]string), BlankLines: make(map[uint16]int),
			Operands: make(map[uint16]string),
			Bytes:    make(map[uint16]Data), Words: make(map[uint16]Data),
		},
		names: make(map[string]uint16),
	}

	for entry := range input.Entries() {
		if err := pr.parseEntry(entry.Section, entry.Key, entry.Value.Raw); err != nil {
			return nil, fmt.Errorf("line %d: %w", entry.Line, err)
		}
	}
	if err := pr.validateGroups(); err != nil {
		return nil, err
	}
	return pr.cfg, nil
}

func isCommentSection(section string) bool {
	return section == sectionComments || section == sectionCommentsBefore
}
