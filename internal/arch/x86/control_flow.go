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

	// Conditional jumps: 0x70-0x7F (Jcc rel8) - 2 bytes
	if opcode >= 0x70 && opcode <= 0x7F {
		return calcRel8Target(address, data), true
	}

	// LOOP/JCXZ: 0xE0-0xE3 (rel8) - 2 bytes
	if opcode >= 0xE0 && opcode <= 0xE3 {
		return calcRel8Target(address, data), true
	}

	switch opcode {
	case 0xE8, 0xE9: // CALL/JMP rel16 - 3 bytes
		return calcRel16Target(address, data)

	case 0xEB: // JMP rel8 - 2 bytes
		return calcRel8Target(address, data), true

	case 0x9A, 0xEA: // CALL/JMP ptr16:16 (far) - 5 bytes
		return calcFarTarget(data)
	}

	return 0, false
}

// calcRel8Target calculates target address for rel8 (8-bit signed) branches.
func calcRel8Target(address uint16, data []byte) uint16 {
	instructionLen := uint16(len(data))
	offset := int8(data[1])
	return address + instructionLen + uint16(int16(offset))
}

// calcRel16Target calculates target address for rel16 (16-bit signed) branches.
func calcRel16Target(address uint16, data []byte) (uint16, bool) {
	if len(data) < 3 {
		return 0, false
	}
	instructionLen := uint16(len(data))
	offset := int16(uint16(data[1]) | (uint16(data[2]) << 8))
	return address + instructionLen + uint16(offset), true
}

// calcFarTarget calculates target address for far (ptr16:16) branches.
func calcFarTarget(data []byte) (uint16, bool) {
	if len(data) < 5 {
		return 0, false
	}
	// For near segment (same CS), just return the offset
	// Note: segment is in data[3:5], ignored for now
	offset := uint16(data[1]) | (uint16(data[2]) << 8)
	return offset, true
}
