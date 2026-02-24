package mapper

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
)

// ClassifyRemainingAsData marks all unclassified bytes as data.
// It iterates through all banks and marks bytes that have not been identified as code or data.
func (m *Mapper) ClassifyRemainingAsData() {
	for _, bnk := range m.banks {
		for i, offsetInfo := range bnk.offsets {
			if offsetInfo.IsType(program.CodeOffset) ||
				offsetInfo.IsType(program.DataOffset) ||
				offsetInfo.IsType(program.FunctionReference) {

				continue
			}

			bnk.offsets[i].Data = []byte{bnk.prg[i]}
		}
	}
}

// SetProgramBanks creates program banks and coordinates with variable and constant managers.
// It processes all mapper banks, creates corresponding program banks, and populates them
// with offset information, variables, and constants.
func (m *Mapper) SetProgramBanks(app *program.Program) error {
	for bnkIndex, bnk := range m.banks {
		prgBank := program.NewPRGBank(len(bnk.offsets))

		for i := range len(bnk.offsets) {
			offsetInfo := bnk.offsets[i]
			programOffsetInfo, err := m.getProgramOffset(m.codeBaseAddress+uint16(i), offsetInfo)
			if err != nil {
				return err
			}

			prgBank.Offsets[i] = programOffsetInfo
		}

		m.consts.AssignBankConstants(bnkIndex, prgBank)
		m.vars.AssignBankVariables(bnkIndex, prgBank)

		setBankName(prgBank, bnkIndex, len(m.banks))
		setBankVectors(bnk, prgBank)

		app.PRG = append(app.PRG, prgBank)
	}
	m.addMissingSymbolAliases(app)
	return nil
}

func (m *Mapper) addMissingSymbolAliases(app *program.Program) {
	if len(app.PRG) == 0 {
		return
	}

	defined := m.collectDefinedSymbols(app)
	missing := m.collectMissingSymbols(app, defined)

	if len(missing) == 0 {
		return
	}
	if app.PRG[0].Constants == nil {
		app.PRG[0].Constants = map[string]uint16{}
	}
	if app.Constants == nil {
		app.Constants = map[string]uint16{}
	}
	for name, address := range missing {
		app.PRG[0].Constants[name] = address
		app.Constants[name] = address
	}
}

// collectDefinedSymbols builds the set of all labels, constants and variables already defined.
func (m *Mapper) collectDefinedSymbols(app *program.Program) map[string]struct{} {
	defined := map[string]struct{}{}
	for _, prgBank := range app.PRG {
		for label := range emittedLabels(prgBank, m.dis.Options()) {
			defined[label] = struct{}{}
		}
		for name := range prgBank.Constants {
			defined[name] = struct{}{}
		}
		for name := range prgBank.Variables {
			defined[name] = struct{}{}
		}
	}
	return defined
}

// collectMissingSymbols scans all offsets for referenced symbols not present in defined.
func (m *Mapper) collectMissingSymbols(app *program.Program, defined map[string]struct{}) map[string]uint16 {
	missing := map[string]uint16{}
	for _, prgBank := range app.PRG {
		for i := range prgBank.Offsets {
			off := &prgBank.Offsets[i]
			symbol, ok := referencedSymbol(off.Code)
			if !ok {
				continue
			}
			if _, exists := defined[symbol]; exists {
				continue
			}
			address, ok := symbolAddress(symbol)
			if !ok {
				continue
			}
			if isRelativeBranchCode(off.Code) && rewriteRelativeBranchAsBytes(off) {
				continue
			}
			missing[symbol] = address
		}
	}
	return missing
}

func emittedLabels(prgBank *program.PRGBank, opts options.Disassembler) map[string]struct{} {
	labels := map[string]struct{}{}
	endIndex := prgBank.LastNonZeroByte(opts)
	for i := 0; i < endIndex; i++ {
		offset := prgBank.Offsets[i]
		if offset.Label != "" {
			labels[offset.Label] = struct{}{}
		}
		i += emittedOffsetAdjustment(prgBank, i, endIndex, offset)
	}
	return labels
}

func emittedOffsetAdjustment(prgBank *program.PRGBank, startIndex, endIndex int, offset program.Offset) int {
	if offset.IsType(program.CodeOffset) && len(offset.Data) == 0 {
		return 0
	}
	if offset.IsType(program.FunctionReference) {
		return 1
	}
	if offset.IsType(program.DataOffset) {
		count := contiguousDataByteCount(prgBank, startIndex, endIndex)
		if count > 0 {
			return count - 1
		}
		return 0
	}
	if len(offset.Data) > 0 {
		return len(offset.Data) - 1
	}
	return 0
}

func contiguousDataByteCount(prgBank *program.PRGBank, startIndex, endIndex int) int {
	var count int
	for i := startIndex; i < endIndex; i++ {
		offset := prgBank.Offsets[i]
		if !offset.IsType(program.DataOffset) || len(offset.Data) == 0 {
			break
		}
		if i > startIndex && (offset.IsType(program.CodeOffset|program.CodeAsData) || offset.Label != "") {
			break
		}
		if offset.WriteCallback != nil && i != startIndex {
			break
		}
		count += len(offset.Data)
	}
	return count
}

func referencedSymbol(code string) (string, bool) {
	fields := strings.Fields(code)
	if len(fields) < 2 {
		return "", false
	}

	operand := fields[1]
	operand = strings.TrimPrefix(operand, "(")
	operand = strings.TrimSuffix(operand, ")")
	operand = strings.TrimSuffix(operand, ",X")
	operand = strings.TrimSuffix(operand, ",Y")
	if plus := strings.IndexByte(operand, '+'); plus > 0 {
		operand = operand[:plus]
	}
	if operand == "" || operand[0] != '_' {
		return "", false
	}
	return operand, true
}

func symbolAddress(symbol string) (uint16, bool) {
	parse := func(prefix string) (uint16, bool) {
		if !strings.HasPrefix(symbol, prefix) {
			return 0, false
		}

		start := len(prefix)
		if len(symbol) < start+4 {
			return 0, false
		}
		hexPart := symbol[start : start+4]
		for _, c := range hexPart {
			isHex := (c >= '0' && c <= '9') ||
				(c >= 'a' && c <= 'f') ||
				(c >= 'A' && c <= 'F')
			if !isHex {
				return 0, false
			}
		}

		value, err := strconv.ParseUint(hexPart, 16, 16)
		if err != nil {
			return 0, false
		}
		return uint16(value), true
	}

	if address, ok := parse("_func_"); ok {
		return address, true
	}
	if address, ok := parse("_label_"); ok {
		return address, true
	}
	if address, ok := parse("_jump_engine_"); ok {
		return address, true
	}
	return 0, false
}

func isRelativeBranchCode(code string) bool {
	fields := strings.Fields(code)
	if len(fields) < 2 {
		return false
	}

	switch strings.ToUpper(fields[0]) {
	case "BCC", "BCS", "BEQ", "BMI", "BNE", "BPL", "BVC", "BVS":
		return true
	default:
		return false
	}
}

func rewriteRelativeBranchAsBytes(off *program.Offset) bool {
	if len(off.Data) < 2 {
		return false
	}
	off.Code = fmt.Sprintf(".byte $%02X, $%02X", off.Data[0], off.Data[1])
	return true
}

// getProgramOffset converts a disassembly offset to a program offset.
// It handles code formatting, branch targets, and comment generation.
func (m *Mapper) getProgramOffset(address uint16, offsetInfo *offset.DisasmOffset) (program.Offset, error) {
	programOffset := offsetInfo.Offset
	programOffset.Address = address

	if offsetInfo.BranchingTo != "" {
		programOffset.Code = fmt.Sprintf("%s %s", offsetInfo.Code, offsetInfo.BranchingTo)
	}

	if offsetInfo.IsType(program.CodeOffset | program.CodeAsData | program.FunctionReference) {
		if len(programOffset.Data) == 0 && programOffset.Label == "" {
			return programOffset, nil
		}

		if offsetInfo.IsType(program.FunctionReference) {
			programOffset.Code = ".word " + offsetInfo.BranchingTo
		}

		if err := m.setComment(address, &programOffset); err != nil {
			return program.Offset{}, err
		}
	} else {
		programOffset.SetType(program.DataOffset)
	}

	return programOffset, nil
}

// setComment generates and sets comments for program offsets based on disassembler options.
// It can add offset addresses, hex code, and preserve existing comments.
func (m *Mapper) setComment(address uint16, programOffset *program.Offset) error {
	var comments []string

	opts := m.dis.Options()
	if opts.OffsetComments {
		programOffset.HasAddressComment = true
		comments = []string{fmt.Sprintf("$%04X", address)}
	}

	if opts.HexComments {
		hexComment, err := programOffset.HexCodeComment()
		if err != nil {
			return fmt.Errorf("generating hex comment: %w", err)
		}
		comments = append(comments, hexComment)
	}

	if programOffset.Comment != "" {
		comments = append(comments, programOffset.Comment)
	}
	programOffset.Comment = strings.Join(comments, "  ")
	return nil
}
