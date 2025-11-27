package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// Opcode wraps an x86 opcode to implement the instruction.Opcode interface.
type Opcode struct {
	op x86.Opcode
}

// Addressing returns the addressing mode as an int.
func (o Opcode) Addressing() int {
	return int(o.op.Addressing)
}

// Instruction returns the instruction wrapper.
func (o Opcode) Instruction() instruction.Instruction {
	return Instruction{ins: o.op.Instruction}
}

// ReadsMemory returns true if the instruction reads from memory.
func (o Opcode) ReadsMemory() bool {
	return o.op.ReadsMemory(x86.MemoryReadInstructions)
}

// WritesMemory returns true if the instruction writes to memory.
func (o Opcode) WritesMemory() bool {
	return o.op.WritesMemory(x86.MemoryWriteInstructions)
}

// ReadWritesMemory returns true if the instruction both reads and writes memory.
func (o Opcode) ReadWritesMemory() bool {
	return o.op.ReadWritesMemory(x86.MemoryReadWriteInstructions)
}

// Size returns the instruction size in bytes.
func (o Opcode) Size() uint8 {
	return o.op.Size
}

// HasModRM returns true if the instruction uses a ModR/M byte.
func (o Opcode) HasModRM() bool {
	return o.op.HasModRM
}

// Register returns the register parameter for register-specific opcodes.
func (o Opcode) Register() x86.RegisterParam {
	return o.op.Register
}
