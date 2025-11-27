package x86

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	programpkg "github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// initializeOffsetInfo reads and decodes the instruction at the current offset.
// Returns true if the offset should be inspected as code, false otherwise.
func (ar *ArchX86) initializeOffsetInfo(offsetInfo *offset.DisasmOffset) (bool, error) {
	// Skip if already processed as code (e.g., from CDL or previous pass)
	if offsetInfo.IsType(programpkg.CodeOffset) {
		return false, nil
	}
	// Skip if already marked as data
	if offsetInfo.IsType(programpkg.DataOffset) {
		return false, nil
	}

	pc := ar.dis.ProgramCounter()

	// Read the opcode byte
	opcodeByte, err := ar.dis.ReadMemory(pc)
	if err != nil {
		return false, fmt.Errorf("reading opcode at %04X: %w", pc, err)
	}

	// Look up opcode in the table
	opcodeInfo, ok := x86.GetOpcodeInfo(opcodeByte)
	if !ok || opcodeInfo.Instruction == nil {
		// Unknown opcode - treat as data
		offsetInfo.SetType(programpkg.DataOffset)
		offsetInfo.Data = []byte{opcodeByte}
		return false, nil
	}

	// Calculate instruction size
	size := ar.calculateInstructionSize(pc, opcodeByte, opcodeInfo)

	// Read all instruction bytes
	data := make([]byte, size)
	for i := range size {
		b, err := ar.dis.ReadMemory(pc + uint16(i))
		if err != nil {
			return false, fmt.Errorf("reading instruction byte at %04X: %w", pc+uint16(i), err)
		}
		data[i] = b
	}

	offsetInfo.Data = data
	offsetInfo.SetType(programpkg.CodeOffset)
	offsetInfo.Opcode = Opcode{op: opcodeInfo}

	return true, nil
}

// calculateInstructionSize determines the total size of an instruction including operands.
func (ar *ArchX86) calculateInstructionSize(pc uint16, opcodeByte uint8, opcodeInfo x86.Opcode) uint8 {
	baseSize := opcodeInfo.Size
	if baseSize == 0 {
		baseSize = 1 // Minimum is 1 byte for opcode
	}

	// If instruction has ModR/M, we need to parse it for displacement size
	if opcodeInfo.HasModRM {
		modrmByte, err := ar.dis.ReadMemory(pc + 1)
		if err != nil {
			return baseSize
		}

		var modrm x86.ModRM
		modrm.FromByte(modrmByte)

		// Calculate additional bytes based on Mod field
		additionalBytes := uint8(0)
		switch modrm.Mod {
		case 0:
			// No displacement, except for special case [disp16] when R/M=6
			if modrm.RM == 6 {
				additionalBytes = 2 // 16-bit displacement
			}
		case 1:
			additionalBytes = 1 // 8-bit displacement
		case 2:
			additionalBytes = 2 // 16-bit displacement
		case 3:
			// Register mode, no displacement
		}

		// Handle immediate operands for certain opcodes
		if opcodeInfo.Addressing == x86.ModRMImmediateAddressing {
			// Group opcodes with immediate operands
			switch opcodeByte {
			case 0xC0, 0xC1: // Shift/rotate with imm8
				additionalBytes++
			case 0xC6: // MOV r/m8, imm8
				additionalBytes++
			case 0xC7: // MOV r/m16, imm16
				additionalBytes += 2
			}
		}

		return baseSize + additionalBytes
	}

	return baseSize
}
