// Package romconfig reads shareable ROM disassembly annotations from INI files.
package romconfig

import (
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

// Vector identifies an NES interrupt vector.
type Vector uint8

const (
	NMI Vector = iota
	Reset
	IRQ
)

// Config contains annotations in CPU address space.
type Config struct {
	PRGCRC32 *uint32
	CHRCRC32 *uint32

	Vectors map[Vector]VectorValue
	Code    []Range
	Data    []Range

	Constants map[uint16]string
	Variables map[uint16]string
	Labels    map[uint16]string
	Symbols   map[string]uint16

	Comments       map[uint16]string
	CommentsBefore map[uint16]string

	BlankLines   map[uint16]int
	SymbolGroups []SymbolGroup

	Operands map[uint16]string
	Bytes    map[uint16]Data
	Words    map[uint16]Data
}

// SymbolGroup orders related definitions under a heading.
type SymbolGroup struct {
	Heading string
	Names   []string
}

// Range describes an inclusive interval of CPU addresses.
type Range struct {
	Start uint16
	End   uint16
}

// VectorValue either names a handler or asserts its stored address.
type VectorValue struct {
	Name    string
	Address *uint16
}

// Data either names element expressions or preserves a count of literal ROM elements.
type Data struct {
	Expressions []string
	Count       uint16
}

// ValidateROM checks the optional PRG and CHR CRC32 checksums.
func (cfg *Config) ValidateROM(prg, chr []byte) error {
	for _, part := range []struct {
		name     string
		data     []byte
		expected *uint32
	}{{"prg_crc32", prg, cfg.PRGCRC32}, {"chr_crc32", chr, cfg.CHRCRC32}} {
		if part.expected != nil {
			actual := crc32.ChecksumIEEE(part.data)
			if actual != *part.expected {
				return fmt.Errorf("%s mismatch: expected %08X, got %08X", part.name, *part.expected, actual)
			}
		}
	}
	return nil
}

func parseAddress(value string) (uint16, error) {
	value = strings.TrimSpace(value)
	base := 10
	switch {
	case strings.HasPrefix(value, "$"):
		base, value = 16, value[1:]
	case strings.HasPrefix(value, "%"):
		base, value = 2, value[1:]
	case strings.HasPrefix(strings.ToLower(value), "0x"):
		base, value = 16, value[2:]
	}
	address, err := strconv.ParseUint(value, base, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", value, err)
	}
	return uint16(address), nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		if ch != '_' && (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') &&
			(i == 0 || ch < '0' || ch > '9') {

			return false
		}
	}
	return true
}
