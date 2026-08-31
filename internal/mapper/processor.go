package mapper

import (
	"fmt"
	"strings"

	"github.com/retroenv/retrodisasm/internal/offset"
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
		// Banked PRG segments share the CPU window base even though assemblers
		// emit each bank into a separate output region.
		prgBank.BaseAddress = m.codeBaseAddress

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
	return nil
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
