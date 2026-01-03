package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// Compile-time check to ensure Instruction implements instruction.Instruction.
var _ instruction.Instruction = (*Instruction)(nil)

// Instruction wraps an x86 instruction for use with the disassembler.
type Instruction struct {
	*x86.Instruction
}

// Name returns the instruction name.
func (i Instruction) Name() string {
	return i.Instruction.Name
}

// IsCall returns true if this is a call instruction.
func (i Instruction) IsCall() bool {
	return i.Name() == x86.CallName
}

// IsJump returns true if this is a jump instruction.
func (i Instruction) IsJump() bool {
	name := i.Name()
	return name == x86.JmpName ||
		name == x86.JbName || name == x86.JbeName ||
		name == x86.JlName || name == x86.JleName ||
		name == x86.JnbName || name == x86.JnbeName ||
		name == x86.JnlName || name == x86.JnleName ||
		name == x86.JnoName || name == x86.JnpName ||
		name == x86.JnsName || name == x86.JnzName ||
		name == x86.JoName || name == x86.JpName ||
		name == x86.JsName || name == x86.JzName
}

// IsReturn returns true if this is a return instruction.
func (i Instruction) IsReturn() bool {
	name := i.Name()
	return name == x86.RetName || name == x86.RetfName || name == x86.IretName
}

// IsNil returns true if the instruction is nil.
func (i Instruction) IsNil() bool {
	return i.Instruction == nil
}

// Unofficial returns true if this is an unofficial/undocumented instruction.
func (i Instruction) Unofficial() bool {
	if i.Instruction == nil {
		return false
	}
	return i.Instruction.Unofficial
}

// IsUnofficialOpcode returns true if this is an unofficial/undocumented opcode.
func (i Instruction) IsUnofficialOpcode() bool {
	return i.Unofficial()
}

// Length returns the instruction length in bytes (base size, not including ModR/M, displacement, immediate).
func (i Instruction) Length() int {
	// This is just a base - actual size computed during decode
	return 1
}
