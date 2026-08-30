package m6502

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
)

var _ instruction.Opcode = &Opcode{}

type Opcode struct {
	op cpu6502.Opcode
}

func (o Opcode) Addressing() int {
	return int(o.op.Addressing)
}

func (o Opcode) Instruction() instruction.Instruction {
	return Instruction{ins: o.op.Instruction}
}

func (o Opcode) ReadsMemory() bool {
	return o.op.ReadsMemory(cpu6502.MemoryReadInstructions)
}

func (o Opcode) WritesMemory() bool {
	return o.op.WritesMemory(cpu6502.MemoryWriteInstructions)
}

func (o Opcode) ReadWritesMemory() bool {
	return o.op.ReadWritesMemory(cpu6502.MemoryReadWriteInstructions)
}
