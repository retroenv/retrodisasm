package m6502

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
)

var _ instruction.Instruction = &Instruction{}

// Instruction represents a 6502 CPU instruction.
type Instruction struct {
	ins *cpu6502.Instruction
}

// IsCall returns true if the instruction is a call.
func (i Instruction) IsCall() bool {
	return i.ins.Name == cpu6502.JsrName
}

// IsNil returns true if the instruction is nil.
func (i Instruction) IsNil() bool {
	return i.ins == nil
}

// Name returns the instruction name.
func (i Instruction) Name() string {
	return i.ins.Name
}

// Unofficial returns true if the instruction is not official.
func (i Instruction) Unofficial() bool {
	return i.ins.Unofficial
}
