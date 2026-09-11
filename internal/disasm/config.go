package disasm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/romconfig"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/set"
)

// ApplyAnnotations loads ROM annotations before Process is called.
func (dis *Disasm) ApplyAnnotations(filename string) error {
	if dis.config != nil || len(dis.offsetsParsed) != 0 {
		return errors.New("annotations must be applied once, before disassembly")
	}
	cfg, err := romconfig.Load(filename)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}
	if err := cfg.ValidateROM(dis.cart.PRG, dis.cart.CHR); err != nil {
		return fmt.Errorf("validating ROM: %w", err)
	}
	if err := dis.resolveConfigVectors(cfg); err != nil {
		return err
	}
	if err := dis.validateConfigAddresses(cfg); err != nil {
		return err
	}
	if err := dis.validateConfigNames(cfg); err != nil {
		return err
	}

	dis.config = cfg
	for address, name := range cfg.Constants {
		dis.constants.Set(address, consts.Constant{Address: address, Read: name, Write: name})
	}
	dis.vars.AssignNames(cfg.Variables)
	for address, label := range cfg.Labels {
		dis.mapper.OffsetInfo(address).Label = label
	}
	dis.assignConfigRanges(cfg.Code, program.CodeOffset)
	dis.assignConfigRanges(cfg.Data, program.DataOffset)
	dis.renameConfiguredHandlers()
	return nil
}

func (dis *Disasm) resolveConfigVectors(cfg *romconfig.Config) error {
	if len(cfg.Vectors) == 0 {
		return nil
	}
	if dis.options.System != arch.NES || dis.options.Binary {
		return errors.New("[vectors] requires an NES ROM with interrupt vectors")
	}
	for _, vector := range []struct {
		kind    romconfig.Vector
		address uint16
	}{{romconfig.NMI, cpu6502.NMIAddress}, {romconfig.Reset, cpu6502.ResetAddress}, {romconfig.IRQ, cpu6502.IrqAddress}} {
		value, ok := cfg.Vectors[vector.kind]
		if !ok {
			continue
		}
		address, err := dis.ReadMemoryWord(vector.address)
		if err != nil {
			return err
		}
		// Numeric values assert the stored vector without changing ROM bytes.
		if value.Address != nil {
			if *value.Address != address {
				return fmt.Errorf("vector at $%04X is $%04X, not $%04X", vector.address, address, *value.Address)
			}
			continue
		}
		if label := cfg.Labels[address]; label != "" && label != value.Name {
			return fmt.Errorf("conflicting names for shared vector at $%04X", address)
		}
		cfg.Labels[address] = value.Name
	}
	return nil
}

func (dis *Disasm) validateConfigAddresses(cfg *romconfig.Config) error {
	for _, entries := range []map[uint16]string{cfg.Labels, cfg.Comments, cfg.CommentsBefore, cfg.Operands} {
		for address := range entries {
			if err := dis.validateAnnotationAddress(address); err != nil {
				return err
			}
		}
	}
	for address := range cfg.BlankLines {
		if err := dis.validateAnnotationAddress(address); err != nil {
			return err
		}
	}
	for address := range cfg.Variables {
		if address >= dis.codeBaseAddress {
			return fmt.Errorf("variable $%04X is in ROM; use [labels]", address)
		}
		if _, ok := cfg.Constants[address]; ok {
			return fmt.Errorf("$%04X is both a variable and a constant", address)
		}
	}
	for _, ranges := range [][]romconfig.Range{cfg.Code, cfg.Data} {
		for _, rng := range ranges {
			for address := uint32(rng.Start); address <= uint32(rng.End); address++ {
				if err := dis.validateAnnotationAddress(uint16(address)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (dis *Disasm) validateAnnotationAddress(address uint16) error {
	if address < dis.codeBaseAddress || address >= dis.arch.LastCodeAddress() ||
		uint32(address) >= uint32(dis.codeBaseAddress)+uint32(len(dis.cart.PRG)) ||
		dis.mapper.OffsetInfo(address) == nil {

		return fmt.Errorf("annotation address $%04X is outside mapped program bytes", address)
	}
	return nil
}

func (dis *Disasm) validateConfigNames(cfg *romconfig.Config) error {
	names := make(map[string]uint16)
	for _, entries := range []map[uint16]string{cfg.Constants, cfg.Variables, cfg.Labels} {
		for address, name := range entries {
			key := strings.ToLower(name)
			if previous, ok := names[key]; ok && previous != address {
				return fmt.Errorf("symbol %q names both $%04X and $%04X", name, previous, address)
			}
			names[key] = address
		}
	}
	for address := uint32(dis.codeBaseAddress); address < uint32(dis.arch.LastCodeAddress()); address++ {
		info := dis.mapper.OffsetInfo(uint16(address))
		if info == nil || cfg.Labels[uint16(address)] != "" || info.Label == "" {
			continue
		}
		if other, ok := names[strings.ToLower(info.Label)]; ok && other != uint16(address) {
			return fmt.Errorf("symbol %q conflicts with existing label at $%04X", info.Label, address)
		}
	}
	return nil
}

func (dis *Disasm) assignConfigRanges(ranges []romconfig.Range, kind program.OffsetType) {
	for _, rng := range ranges {
		for address := uint32(rng.Start); address <= uint32(rng.End); address++ {
			info := dis.mapper.OffsetInfo(uint16(address))
			info.CodeHint = kind
			if kind == program.DataOffset {
				info.SetType(program.DataOffset)
			} else {
				info.ClearType(program.DataOffset)
			}
		}
	}
}

func (dis *Disasm) renameConfiguredHandlers() {
	if dis.options.System != arch.NES || dis.options.Binary {
		return
	}
	for _, vector := range []struct {
		kind    romconfig.Vector
		address uint16
		handler *string
	}{{romconfig.NMI, cpu6502.NMIAddress, &dis.handlers.NMI}, {romconfig.Reset, cpu6502.ResetAddress, &dis.handlers.Reset},
		{romconfig.IRQ, cpu6502.IrqAddress, &dis.handlers.IRQ}} {
		if value := dis.config.Vectors[vector.kind]; value.Address != nil {
			*vector.handler = fmt.Sprintf("$%04X", *value.Address)
			continue
		}
		address := uint16(dis.mapper.ReadMemory(vector.address)) |
			uint16(dis.mapper.ReadMemory(vector.address+1))<<8
		if name := dis.config.Labels[address]; name != "" {
			*vector.handler = name
		}
	}
}

func (dis *Disasm) traceConfiguredCode(ctx context.Context) error {
	if dis.config == nil {
		return nil
	}
	for _, rng := range dis.config.Code {
		for address := uint32(rng.Start); address <= uint32(rng.End); address++ {
			info := dis.mapper.OffsetInfo(uint16(address))
			if info.IsType(program.CodeOffset | program.CodeAsData) {
				continue
			}
			info.ClearType(program.DataOffset | program.FunctionReference | program.JumpTable)
			dis.offsetsParsed.Remove(uint16(address))
			dis.offsetsToParse = append(dis.offsetsToParse, uint16(address))
			if err := dis.followExecutionFlow(ctx); err != nil {
				return err
			}
			if !info.IsType(program.CodeOffset | program.CodeAsData) {
				return fmt.Errorf("cannot decode configured code at $%04X", address)
			}
		}
	}
	return nil
}

func (dis *Disasm) validateInstructionHints(address uint16, info *offset.DisasmOffset) error {
	for i := 1; i < len(info.Data); i++ {
		operand := dis.mapper.OffsetInfo(address + uint16(i))
		if operand != nil && operand.CodeHint == program.DataOffset {
			return fmt.Errorf("instruction at $%04X overlaps configured data at $%04X", address, int(address)+i)
		}
	}
	return nil
}

func (dis *Disasm) applyAnnotations() {
	if dis.config == nil {
		return
	}
	// Apply every annotation before repairing jump targets, because several
	// annotation kinds can share an address and need a single repair pass.
	addresses := set.New[uint16]()
	for address, comment := range dis.config.CommentsBefore {
		dis.mapper.OffsetInfo(address).CommentBefore = comment
		addresses.Add(address)
	}
	for address, count := range dis.config.BlankLines {
		dis.mapper.OffsetInfo(address).BlankLines = &count
		addresses.Add(address)
	}
	for address, comment := range dis.config.Comments {
		info := dis.mapper.OffsetInfo(address)
		if info.Comment != "" {
			info.Comment += " | "
		}
		info.Comment += comment
		addresses.Add(address)
	}
	for address := range dis.config.Labels {
		addresses.Add(address)
	}
	for _, address := range set.Sorted(addresses) {
		info := dis.mapper.OffsetInfo(address)
		if len(info.Data) == 0 && info.IsType(program.CodeOffset|program.CodeAsData|program.FunctionReference) {
			dis.handleJumpIntoInstruction(address)
		}
	}
}

func (dis *Disasm) assignConfiguredSymbols(app *program.Program) error {
	for _, group := range dis.config.SymbolGroups {
		app.SymbolGroups = append(app.SymbolGroups, program.SymbolGroup{Heading: group.Heading, Names: group.Names})
	}
	for _, name := range slices.Sorted(maps.Keys(dis.config.Symbols)) {
		value := dis.config.Symbols[name]
		if dis.config.Labels[value] != "" {
			for _, bank := range app.PRG {
				index := int(value) - int(bank.BaseAddress)
				if index >= 0 && index < len(bank.Offsets) && bank.Offsets[index].Label == dis.config.Labels[value] {
					bank.Offsets[index].Aliases = append(bank.Offsets[index].Aliases, name)
					break
				}
			}
			continue
		}
		if address, ok := app.Variables[name]; ok {
			if address != value {
				return fmt.Errorf("symbol %q conflicts with variable value", name)
			}
			continue
		}
		if address, ok := app.Constants[name]; ok && address != value {
			return fmt.Errorf("symbol %q conflicts with constant value", name)
		}
		app.Constants[name] = value
		if len(app.PRG) != 0 {
			app.PRG[0].Constants[name] = value
		}
	}
	return nil
}

func (dis *Disasm) applyConfiguredOperands() error {
	if dis.config == nil || len(dis.config.Operands) == 0 {
		return nil
	}
	formatter, ok := dis.arch.(interface {
		ApplyOperand(uint16, *offset.DisasmOffset, string, uint16) error
	})
	if !ok {
		return errors.New("operand hints are not supported by this architecture")
	}
	symbols := dis.configSymbols()
	for _, address := range slices.Sorted(maps.Keys(dis.config.Operands)) {
		expression := dis.config.Operands[address]
		value, err := romconfig.Evaluate(expression, symbols)
		if err != nil {
			return fmt.Errorf("operand at $%04X: %w", address, err)
		}
		if err := formatter.ApplyOperand(address, dis.mapper.OffsetInfo(address), expression, value); err != nil {
			return fmt.Errorf("applying operand: %w", err)
		}
	}
	return nil
}

func (dis *Disasm) configSymbols() map[string]uint16 {
	symbols := maps.Clone(dis.config.Symbols)
	for _, entries := range []map[uint16]string{dis.config.Labels, dis.config.Constants, dis.config.Variables} {
		for address, name := range entries {
			symbols[name] = address
		}
	}
	return symbols
}

func (dis *Disasm) pruneUnusedConfigSymbols(app *program.Program) {
	if len(dis.config.Operands) == 0 {
		return
	}
	used := set.NewFromSlice([]string{app.Handlers.NMI, app.Handlers.Reset, app.Handlers.IRQ})
	for name := range dis.config.Symbols {
		used.Add(name)
	}
	for _, group := range dis.config.SymbolGroups {
		for _, name := range group.Names {
			used.Add(name)
		}
	}
	for _, bank := range app.PRG {
		for _, info := range bank.Offsets {
			for _, name := range strings.FieldsFunc(info.Code, isSymbolSeparator) {
				used.Add(name)
			}
		}
	}
	for _, aliases := range []map[string]uint16{app.Constants, app.Variables} {
		deleteUnusedAliases(aliases, used)
	}
	for _, bank := range app.PRG {
		deleteUnusedAliases(bank.Constants, used)
		deleteUnusedAliases(bank.Variables, used)
		for i := range bank.Offsets {
			info := &bank.Offsets[i]
			if dis.config.Labels[info.Address] == "" && len(info.Aliases) == 0 && !used.Contains(info.Label) {
				info.Label = ""
			}
		}
	}
}

func (dis *Disasm) applyConfiguredData() error {
	if dis.config == nil || len(dis.config.Bytes)+len(dis.config.Words) == 0 {
		return nil
	}
	dis.removeUnusedDataLabels()
	symbols := dis.configSymbols()
	for width, entries := range []map[uint16]romconfig.Data{dis.config.Bytes, dis.config.Words} {
		for _, address := range slices.Sorted(maps.Keys(entries)) {
			if err := dis.assignDataExpressions(address, width+1, entries[address], symbols); err != nil {
				return err
			}
		}
	}
	return nil
}

func (dis *Disasm) removeUnusedDataLabels() {
	// Explicit symbolic data replaces inferred data labels unless a configured
	// expression still references the generated name.
	used := dis.referencedConfigNames()
	for address := uint32(dis.codeBaseAddress); address < uint32(dis.arch.LastCodeAddress()); address++ {
		info := dis.mapper.OffsetInfo(uint16(address))
		if info != nil && info.IsType(program.DataOffset) && dis.config.Labels[uint16(address)] == "" &&
			!used.Contains(info.Label) {

			info.Label = ""
		}
	}
}

func (dis *Disasm) referencedConfigNames() set.Set[string] {
	used := set.New[string]()
	for address := uint32(dis.codeBaseAddress); address < uint32(dis.arch.LastCodeAddress()); address++ {
		info := dis.mapper.OffsetInfo(uint16(address))
		if info == nil {
			continue
		}
		for _, name := range strings.FieldsFunc(info.Code+" "+info.BranchingTo, isSymbolSeparator) {
			used.Add(name)
		}
	}
	for _, entries := range []map[uint16]romconfig.Data{dis.config.Bytes, dis.config.Words} {
		for _, data := range entries {
			for _, expression := range data.Expressions {
				for _, name := range strings.FieldsFunc(expression, isSymbolSeparator) {
					used.Add(name)
				}
			}
		}
	}
	return used
}

func (dis *Disasm) assignDataExpressions(address uint16, width int, layout romconfig.Data, symbols map[string]uint16) error {
	expressions := layout.Expressions
	if layout.Count != 0 {
		var err error
		expressions, err = dis.literalDataExpressions(address, width, int(layout.Count))
		if err != nil {
			return err
		}
	}
	data, err := encodeExpressions(width, expressions, symbols)
	if err != nil {
		return fmt.Errorf("data at $%04X: %w", address, err)
	}
	if err := dis.validateDataExpressions(address, data); err != nil {
		return err
	}
	// The first offset owns the complete byte slice so the writer emits one
	// directive; clear the remaining offsets to prevent duplicate output.
	for i := range data {
		info := dis.mapper.OffsetInfo(address + uint16(i))
		info.Data = nil
		info.SetType(program.ExpressionData)
	}
	info := dis.mapper.OffsetInfo(address)
	info.Data = data
	directive := ".byte "
	if width == 2 {
		directive = ".word "
	}
	formatted := make([]string, len(expressions))
	for i, expression := range expressions {
		formatted[i] = assembler.FormatExpression(dis.options.Assembler, expression)
	}
	info.Code = directive + strings.Join(formatted, ", ")
	return nil
}

func (dis *Disasm) literalDataExpressions(address uint16, width, count int) ([]string, error) {
	end := uint32(address) + uint32(width*count)
	if end > uint32(dis.arch.LastCodeAddress()) {
		return nil, errors.New("literal data extends beyond program bytes")
	}
	expressions := make([]string, 0, count)
	for current := uint32(address); current < end; current += uint32(width) {
		var value uint16
		for i := range width {
			addr := uint16(current) + uint16(i)
			if err := dis.validateAnnotationAddress(addr); err != nil {
				return nil, err
			}
			value |= uint16(dis.mapper.ReadMemory(addr)) << (8 * i)
		}
		expressions = append(expressions, fmt.Sprintf("$%0*x", width*2, value))
	}
	return expressions, nil
}

func (dis *Disasm) validateDataExpressions(address uint16, data []byte) error {
	for i, value := range data {
		current := uint32(address) + uint32(i)
		if current > 0xffff {
			return errors.New("symbolic data crosses address-space boundary")
		}
		if err := dis.validateAnnotationAddress(uint16(current)); err != nil {
			return err
		}
		info := dis.mapper.OffsetInfo(uint16(current))
		if !info.IsType(program.DataOffset) || info.IsType(program.ExpressionData|program.CodeOffset|program.CodeAsData) {
			return fmt.Errorf("symbolic data at $%04X overlaps code or other symbolic data", current)
		}
		if i > 0 && (info.Label != "" || info.Comment != "" || info.CommentBefore != "" || info.BlankLines != nil) {
			return fmt.Errorf("symbolic data crosses annotation at $%04X", current)
		}
		if value != dis.mapper.ReadMemory(uint16(current)) {
			return fmt.Errorf("symbolic data at $%04X does not match ROM byte", current)
		}
	}
	return nil
}

func encodeExpressions(width int, expressions []string, symbols map[string]uint16) ([]byte, error) {
	data := make([]byte, 0, width*len(expressions))
	for _, expression := range expressions {
		value, err := romconfig.Evaluate(expression, symbols)
		if err != nil {
			return nil, fmt.Errorf("evaluating data: %w", err)
		}
		if width == 1 && value > 255 {
			return nil, errors.New("byte expression exceeds 255")
		}
		data = append(data, byte(value))
		if width == 2 {
			data = append(data, byte(value>>8))
		}
	}
	return data, nil
}

func isSymbolSeparator(ch rune) bool {
	return ch != '_' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9')
}

func deleteUnusedAliases(aliases map[string]uint16, used set.Set[string]) {
	for name := range aliases {
		if !used.Contains(name) {
			delete(aliases, name)
		}
	}
}

func validateConfiguredSymbols(app *program.Program) error {
	names := set.New[string]()
	register := func(name string) error {
		key := strings.ToLower(name)
		if names.Contains(key) {
			return fmt.Errorf("annotations produce duplicate assembly symbol %q", name)
		}
		names.Add(key)
		return nil
	}

	for _, aliases := range []map[string]uint16{app.Constants, app.Variables} {
		for name := range aliases {
			if err := register(name); err != nil {
				return err
			}
		}
	}
	for _, bank := range app.PRG {
		for _, info := range bank.Offsets {
			for _, alias := range info.Aliases {
				if err := register(alias); err != nil {
					return err
				}
			}
			if info.Label != "" {
				if err := register(info.Label); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
