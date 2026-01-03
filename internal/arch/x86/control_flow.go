package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	x86cpu "github.com/retroenv/retrogolib/arch/cpu/x86"
)

// handleControlFlow processes control flow based on instruction type.
func (a *X86) handleControlFlow(address uint16, offsetInfo *offset.DisasmOffset, instruction instruction.Instruction, instr Instruction) {
	pc := a.dis.ProgramCounter()
	name := instr.Name()

	switch {
	case name == x86cpu.CallName:
		// CALL: add target and continue to next instruction
		if target, ok := a.extractBranchTarget(address, offsetInfo.Data); ok {
			a.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		}
		nextAddr := pc + uint16(len(offsetInfo.Data))
		a.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)

	case name == x86cpu.JmpName:
		// Unconditional JMP: add target only, don't continue
		if target, ok := a.extractBranchTarget(address, offsetInfo.Data); ok {
			a.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		}

	case x86cpu.ConditionalJumpInstructions.Contains(name):
		// Conditional jumps: add target AND continue to next instruction
		if target, ok := a.extractBranchTarget(address, offsetInfo.Data); ok {
			a.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		}
		nextAddr := pc + uint16(len(offsetInfo.Data))
		a.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)

	case !instr.IsReturn():
		// Normal instructions (not RET/RETF/IRET/HLT/JMP): continue to next instruction
		nextAddr := pc + uint16(len(offsetInfo.Data))
		a.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)
	}
	// Terminal instructions (RET/RETF/IRET/HLT/JMP) - don't add any addresses
}

// extractBranchTarget extracts the target address from a branch instruction.
func (a *X86) extractBranchTarget(address uint16, data []byte) (uint16, bool) {
	if len(data) < 2 {
		return 0, false
	}

	opcode := data[0]
	instructionLen := uint16(len(data))

	// Conditional jumps: 0x70-0x7F (Jcc rel8) - 2 bytes
	if opcode >= 0x70 && opcode <= 0x7F {
		offset := int8(data[1])
		target := address + instructionLen + uint16(int16(offset))
		return target, true
	}

	switch opcode {
	case 0xE8: // CALL rel16 - 3 bytes
		if len(data) >= 3 {
			offset := int16(uint16(data[1]) | (uint16(data[2]) << 8))
			target := address + instructionLen + uint16(offset)
			return target, true
		}

	case 0xE9: // JMP rel16 - 3 bytes
		if len(data) >= 3 {
			offset := int16(uint16(data[1]) | (uint16(data[2]) << 8))
			target := address + instructionLen + uint16(offset)
			return target, true
		}

	case 0xEB: // JMP rel8 - 2 bytes
		offset := int8(data[1])
		target := address + instructionLen + uint16(int16(offset))
		return target, true

	case 0x9A: // CALL ptr16:16 (far call) - 5 bytes
		if len(data) >= 5 {
			// For near segment (same CS), just return the offset
			offset := uint16(data[1]) | (uint16(data[2]) << 8)
			// Note: segment is in data[3:5], ignored for now
			return offset, true
		}

	case 0xEA: // JMP ptr16:16 (far jump) - 5 bytes
		if len(data) >= 5 {
			// For near segment (same CS), just return the offset
			offset := uint16(data[1]) | (uint16(data[2]) << 8)
			// Note: segment is in data[3:5], ignored for now
			return offset, true
		}
	}

	return 0, false
}
