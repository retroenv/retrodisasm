package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// Compile-time check to ensure Instruction implements instruction.Instruction.
var _ instruction.Instruction = &Instruction{}

// Instruction wraps an x86 instruction for use with the disassembler.
type Instruction struct {
	ins *x86.Instruction
}

// Name returns the instruction name.
func (i Instruction) Name() string {
	if i.ins == nil {
		return ""
	}
	return i.ins.Name
}

// IsCall returns true if this is a call instruction.
func (i Instruction) IsCall() bool {
	return i.Name() == x86.CallName
}

// IsJump returns true if this is a branching instruction.
func (i Instruction) IsJump() bool {
	if i.ins == nil {
		return false
	}
	return x86.BranchingInstructions.Contains(i.ins.Name)
}

// IsReturn returns true if this is a return instruction.
func (i Instruction) IsReturn() bool {
	if i.ins == nil {
		return false
	}
	return x86.NotExecutingFollowingOpcodeInstructions.Contains(i.ins.Name)
}

// IsNil returns true if the instruction is nil.
func (i Instruction) IsNil() bool {
	return i.ins == nil
}

// Unofficial returns true if this is an unofficial/undocumented instruction.
func (i Instruction) Unofficial() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Unofficial
}
