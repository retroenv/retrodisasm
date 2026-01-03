package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// Compile-time check to ensure Opcode implements instruction.Opcode.
var _ instruction.Opcode = &Opcode{}

// Opcode wraps an x86 opcode for use with the disassembler.
type Opcode struct {
	op *x86.Opcode
}

// Instruction returns the instruction interface.
func (o Opcode) Instruction() instruction.Instruction {
	if o.op == nil || o.op.Instruction == nil {
		return Instruction{}
	}
	return Instruction{ins: o.op.Instruction}
}

// Addressing returns the addressing mode.
func (o Opcode) Addressing() int {
	if o.op == nil {
		return 0
	}
	return int(o.op.Addressing)
}

// ReadsMemory returns true if the opcode reads memory.
func (o Opcode) ReadsMemory() bool {
	if o.op == nil {
		return false
	}
	return o.op.ReadsMemory(x86.MemoryReadInstructions)
}

// ReadWritesMemory returns true if the opcode reads and writes memory.
func (o Opcode) ReadWritesMemory() bool {
	if o.op == nil {
		return false
	}
	return o.op.ReadWritesMemory(x86.MemoryReadWriteInstructions)
}

// WritesMemory returns true if the opcode writes memory.
func (o Opcode) WritesMemory() bool {
	if o.op == nil {
		return false
	}
	return o.op.WritesMemory(x86.MemoryWriteInstructions)
}
