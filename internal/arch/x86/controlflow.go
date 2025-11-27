package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
)

// handleControlFlow processes control flow based on instruction type.
// It queues branch targets and fall-through addresses for processing.
func (ar *ArchX86) handleControlFlow(address uint16, offsetInfo *offset.DisasmOffset, ins instruction.Instruction, instr Instruction) {
	pc := ar.dis.ProgramCounter()
	instrLen := uint16(len(offsetInfo.Data))
	nextAddr := pc + instrLen

	switch {
	case instr.IsJump():
		// Unconditional jump - only follow the jump target, not the next instruction
		if target, ok := ar.extractJumpTarget(offsetInfo); ok {
			ar.dis.AddAddressToParse(target, offsetInfo.Context, address, ins, true)
		}

	case instr.IsConditionalJump():
		// Conditional jump - follow both the jump target and the next instruction
		if target, ok := ar.extractJumpTarget(offsetInfo); ok {
			ar.dis.AddAddressToParse(target, offsetInfo.Context, address, ins, true)
		}
		ar.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, ins, false)

	case ins.IsCall():
		// Call - follow the call target and continue after the call
		if target, ok := ar.extractJumpTarget(offsetInfo); ok {
			ar.dis.AddAddressToParse(target, offsetInfo.Context, address, ins, true)
		}
		ar.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, ins, false)

	case instr.IsReturn():
		// Return instruction - don't follow any addresses

	case instr.IsInterrupt():
		// Interrupt - continue after the interrupt unless it's a program termination
		if ar.isTerminatingInterrupt(offsetInfo) {
			return
		}
		ar.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, ins, false)

	default:
		// Regular instruction - continue to next instruction
		if !instr.IsUnconditionalBranch() {
			ar.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, ins, false)
		}
	}
}

// isTerminatingInterrupt checks if an interrupt instruction terminates the program.
func (ar *ArchX86) isTerminatingInterrupt(offsetInfo *offset.DisasmOffset) bool {
	if len(offsetInfo.Data) < 2 {
		return false
	}

	intNum := offsetInfo.Data[1]

	// INT 20h (0x20) terminates the program
	if intNum == 0x20 {
		return true
	}

	// Check for INT 21h AH=00h or AH=4Ch (Terminate with Return Code)
	if intNum == 0x21 {
		funcNum := ar.findAHValue()
		return funcNum == 0x00 || funcNum == 0x4C
	}

	return false
}

// extractJumpTarget extracts the target address from a jump/call instruction.
func (ar *ArchX86) extractJumpTarget(offsetInfo *offset.DisasmOffset) (uint16, bool) {
	data := offsetInfo.Data
	if len(data) < 2 {
		return 0, false
	}

	opcode := data[0]
	pc := ar.dis.ProgramCounter()
	instrLen := uint16(len(data))

	// Handle different jump/call encodings
	switch {
	case opcode >= 0x70 && opcode <= 0x7F:
		// Short conditional jump (Jcc rel8)
		rel := int8(data[1])
		target := pc + instrLen + uint16(int16(rel))
		return target, isValidTarget(target)

	case opcode == 0xE8:
		// CALL rel16
		if len(data) >= 3 {
			rel := int16(uint16(data[2])<<8 | uint16(data[1]))
			target := pc + instrLen + uint16(rel)
			return target, isValidTarget(target)
		}

	case opcode == 0xE9:
		// JMP rel16
		if len(data) >= 3 {
			rel := int16(uint16(data[2])<<8 | uint16(data[1]))
			target := pc + instrLen + uint16(rel)
			return target, isValidTarget(target)
		}

	case opcode == 0xEB:
		// JMP rel8
		rel := int8(data[1])
		target := pc + instrLen + uint16(int16(rel))
		return target, isValidTarget(target)

	case opcode == 0x9A:
		// CALL far ptr16:16 (direct intersegment call)
		// For .com files, we typically don't follow far calls
		return 0, false

	case opcode == 0xEA:
		// JMP far ptr16:16 (direct intersegment jump)
		// For .com files, we typically don't follow far jumps
		return 0, false
	}

	return 0, false
}

// isValidTarget checks if the target address is within the valid range for .com files.
func isValidTarget(target uint16) bool {
	return target >= ProgramStart && target <= MaxAddress
}
