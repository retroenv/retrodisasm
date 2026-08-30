// Package chip8 provides CHIP-8 instruction wrapper implementation.
package chip8

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/chip8"
)

// Compile-time check to ensure Instruction implements instruction.Instruction.
var _ instruction.Instruction = (*Instruction)(nil)

// Instruction represents a CHIP-8 instruction wrapper that implements arch.Instruction.
// It provides a bridge between the retrogolib CHIP-8 instruction definitions
// and the disassembler's architecture interface.
type Instruction struct {
	ins *chip8.Instruction
}

// IsCall returns true if the instruction is a call instruction.
func (i Instruction) IsCall() bool {
	return i.ins == chip8.CallInst
}

// IsNil returns true if the instruction is nil.
func (i Instruction) IsNil() bool {
	return i.ins == nil
}

// Name returns the instruction name.
func (i Instruction) Name() string {
	if i.ins == nil {
		return ""
	}
	return i.ins.Name
}

// Unofficial returns true if the instruction is not official.
// CHIP-8 has a well-defined instruction set with no unofficial opcodes.
func (i Instruction) Unofficial() bool {
	return false
}

// IsJump returns true if the instruction is a jump instruction.
func (i Instruction) IsJump() bool {
	return i.ins == chip8.JpInst
}

// IsReturn returns true if the instruction is a return instruction.
func (i Instruction) IsReturn() bool {
	return i.ins == chip8.RetInst
}

// IsSkip returns true if the instruction is a conditional skip instruction.
func (i Instruction) IsSkip() bool {
	if i.ins == nil {
		return false
	}
	return chip8.SkipInstructions.Contains(i.ins.Name)
}

// IsDataReference returns true if the instruction references data (LD I, addr).
func (i Instruction) IsDataReference(data []byte) bool {
	if i.ins != chip8.LdInst || len(data) < 2 {
		return false
	}
	opcode := uint16(data[0])<<8 | uint16(data[1])
	// Only ld I, address instructions (0xAXXX)
	return (opcode & 0xF000) == 0xA000
}
