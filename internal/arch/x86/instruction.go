package x86

import "github.com/retroenv/retrogolib/arch/cpu/x86"

// Instruction wraps an x86 instruction to implement the instruction.Instruction interface.
type Instruction struct {
	ins *x86.Instruction
}

// IsCall returns true if this instruction is a CALL instruction.
func (i Instruction) IsCall() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Name == x86.CallName
}

// IsNil returns true if the instruction is nil.
func (i Instruction) IsNil() bool {
	return i.ins == nil
}

// Name returns the instruction mnemonic.
func (i Instruction) Name() string {
	if i.ins == nil {
		return ""
	}
	return i.ins.Name
}

// Unofficial returns true if this is an unofficial instruction.
// x86 doesn't have unofficial instructions like 6502.
func (i Instruction) Unofficial() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Unofficial
}

// IsJump returns true if this instruction is an unconditional jump.
func (i Instruction) IsJump() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Name == x86.JmpName
}

// IsReturn returns true if this instruction is a return instruction.
func (i Instruction) IsReturn() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Name == x86.RetName || i.ins.Name == x86.RetfName || i.ins.Name == x86.IretName
}

// IsConditionalJump returns true if this instruction is a conditional jump.
func (i Instruction) IsConditionalJump() bool {
	if i.ins == nil {
		return false
	}
	return x86.ConditionalJumpInstructions.Contains(i.ins.Name)
}

// IsBranching returns true if this instruction can change control flow.
func (i Instruction) IsBranching() bool {
	if i.ins == nil {
		return false
	}
	return x86.BranchingInstructions.Contains(i.ins.Name)
}

// IsUnconditionalBranch returns true if this instruction never executes the following opcode.
func (i Instruction) IsUnconditionalBranch() bool {
	if i.ins == nil {
		return false
	}
	return x86.NotExecutingFollowingOpcodeInstructions.Contains(i.ins.Name)
}

// IsInterrupt returns true if this instruction is an INT instruction.
func (i Instruction) IsInterrupt() bool {
	if i.ins == nil {
		return false
	}
	return i.ins.Name == x86.IntName
}
