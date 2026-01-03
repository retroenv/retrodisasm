package x86

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// Opcode wraps an x86 opcode for use with the disassembler.
type Opcode struct {
	*x86.Opcode
}

// Instruction returns the instruction interface.
func (o Opcode) Instruction() instruction.Instruction {
	return Instruction{o.Opcode.Instruction}
}

// Addressing returns the addressing mode (returns as int).
func (o Opcode) Addressing() int {
	return int(o.Opcode.Addressing)
}

// ReadsMemory returns true if the opcode reads memory.
func (o Opcode) ReadsMemory() bool {
	// TODO: Implement memory access analysis
	return false
}

// ReadWritesMemory returns true if the opcode reads and writes memory.
func (o Opcode) ReadWritesMemory() bool {
	// TODO: Implement memory access analysis
	return false
}

// WritesMemory returns true if the opcode writes memory.
func (o Opcode) WritesMemory() bool {
	// TODO: Implement memory access analysis
	return false
}
