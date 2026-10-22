package mapper

import (
	"fmt"
	"slices"
	"strings"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
)

// ClassifyRemainingAsData marks all unowned bytes as data.
func (m *Mapper) ClassifyRemainingAsData() {
	for _, bnk := range slices.Concat(m.banks, m.views) {
		if !bnk.mapped {
			continue
		}
		// Empty code offsets represent operand bytes only while an owning encoding covers them.
		owned := make([]bool, len(bnk.offsets))
		for i, offsetInfo := range bnk.offsets {
			// A function reference can span two physical banks. Each bank must
			// own only its stored bytes, even when the CPU reads one word.
			if remaining := len(bnk.offsets) - i; len(offsetInfo.Data) > remaining {
				offsetInfo.Data = offsetInfo.Data[:remaining]
				offsetInfo.ClearType(program.CodeOffset | program.FunctionReference | program.ExpressionData)
				offsetInfo.SetType(program.CodeAsData | program.DataOffset)
				offsetInfo.Code = ""
			}
			for j := 1; j < len(offsetInfo.Data) && i+j < len(owned); j++ {
				owned[i+j] = true
			}
		}

		for i, offsetInfo := range bnk.offsets {
			if len(offsetInfo.Data) > 0 || owned[i] {
				continue
			}

			offsetInfo.Data = []byte{bnk.prg[i]}
			offsetInfo.ClearType(program.CodeOffset | program.FunctionReference | program.JumpTable)
			offsetInfo.SetType(program.DataOffset)
		}
	}
}

// SetProgramBanks creates program banks and coordinates with variable and constant managers.
// It processes all mapper banks, creates corresponding program banks, and populates them
// with offset information, variables, and constants.
func (m *Mapper) SetProgramBanks(app *program.Program) error {
	for _, bnk := range slices.Concat(m.banks, m.views) {
		size := 0
		if bnk.mapped {
			size = len(bnk.offsets)
		}
		prgBank := program.NewPRGBank(size)
		prgBank.ID = bnk.id
		prgBank.ROMOffset = bnk.romOffset
		prgBank.BaseAddress = m.codeBaseAddress
		if bnk.baseAddress != 0 {
			prgBank.BaseAddress = bnk.baseAddress
		}
		prgBank.HasVectors = bnk.hasVectors

		for i := range size {
			offsetInfo := bnk.offsets[i]
			programOffsetInfo, err := m.getProgramOffset(prgBank.BaseAddress+uint16(i), offsetInfo)
			if err != nil {
				return err
			}

			prgBank.Offsets[i] = programOffsetInfo
		}

		m.consts.AssignBankConstants(bnk.id, prgBank)
		m.vars.AssignBankVariables(bnk.id, prgBank)

		setBankName(prgBank, bnk.id, len(m.banks))
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
