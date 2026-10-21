package m6502

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/assert"
)

func knownValue(value byte) knownByte {
	return knownByte{
		value: value,
		known: true,
	}
}

type applyTest struct {
	name    string
	opcode  byte
	operand uint16
	regs    registers
	want    registers
}

func runApplyTests(t *testing.T, tests []applyTest) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ins := flowInstruction{
				op:      cpu6502.Opcodes[tt.opcode],
				operand: tt.operand,
			}

			got := ins.apply(nil, mapper.Snapshot{}, tt.regs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFlowInstructionApplyAccumulator(t *testing.T) {
	runApplyTests(t, []applyTest{
		{
			name:    "lda immediate",
			opcode:  0xa9,
			operand: 0x12,
			want:    registers{a: knownValue(0x12)},
		},
		{
			name:    "and immediate",
			opcode:  0x29,
			operand: 0x0f,
			regs:    registers{a: knownValue(0xab)},
			want:    registers{a: knownValue(0x0b)},
		},
		{
			name:    "ora immediate",
			opcode:  0x09,
			operand: 0x0f,
			regs:    registers{a: knownValue(0xa0)},
			want:    registers{a: knownValue(0xaf)},
		},
		{
			name:    "eor immediate",
			opcode:  0x49,
			operand: 0xff,
			regs:    registers{a: knownValue(0x0f)},
			want:    registers{a: knownValue(0xf0)},
		},
		{
			name:    "and with unknown accumulator",
			opcode:  0x29,
			operand: 0x0f,
			want:    registers{},
		},
		{
			name:   "lsr accumulator",
			opcode: 0x4a,
			regs:   registers{a: knownValue(0x03)},
			want:   registers{a: knownValue(0x01)},
		},
	})
}

func TestFlowInstructionApplyRegisters(t *testing.T) {
	runApplyTests(t, []applyTest{
		{
			name:   "tax",
			opcode: 0xaa,
			regs:   registers{a: knownValue(0x05)},
			want: registers{
				a: knownValue(0x05),
				x: knownValue(0x05),
			},
		},
		{
			name:   "inx wraps",
			opcode: 0xe8,
			regs:   registers{x: knownValue(0xff)},
			want:   registers{x: knownValue(0x00)},
		},
		{
			name:    "adc forgets accumulator",
			opcode:  0x69,
			operand: 0x01,
			regs:    registers{a: knownValue(0x05)},
			want:    registers{},
		},
		{
			name:   "unofficial forgets all registers",
			opcode: 0x1a, // unofficial NOP
			regs: registers{
				a: knownValue(0x05),
				x: knownValue(0x06),
				y: knownValue(0x07),
			},
			want: registers{},
		},
	})
}

func TestFlowInstructionStore(t *testing.T) {
	ins := flowInstruction{
		op:      cpu6502.Opcodes[0x9d], // STA abs,X
		operand: 0x8000,
	}

	regs := registers{
		a: knownValue(0x03),
		x: knownValue(0x10),
	}

	address, value, known := ins.store(regs)
	assert.True(t, known)
	assert.Equal(t, uint16(0x8010), address)
	assert.Equal(t, knownValue(0x03), value)

	_, _, known = ins.store(registers{a: knownValue(0x03)})
	assert.False(t, known)
}

func TestJoinRegisters(t *testing.T) {
	left := registers{
		a: knownValue(1),
		x: knownValue(2),
		y: knownValue(3),
	}
	right := registers{
		a: knownValue(1),
		x: knownValue(9),
	}

	got := joinRegisters(left, right)
	assert.Equal(t, registers{a: knownValue(1)}, got)
}
