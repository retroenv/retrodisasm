package m6502

import (
	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
)

// knownByte is a register or memory value that is only valid if known is set.
type knownByte struct {
	value byte
	known bool
}

// registers tracks the values of the CPU registers along one control-flow path.
type registers struct {
	a, x, y knownByte
}

// flowInstruction is a decoded instruction with its operand, read from a mapper state.
type flowInstruction struct {
	op      cpu6502.Opcode
	operand uint16
	size    int
}

func (ins flowInstruction) store(regs registers) (uint16, knownByte, bool) {
	var value knownByte

	switch ins.op.Instruction.Name {
	case cpu6502.StaName:
		value = regs.a
	case cpu6502.StxName:
		value = regs.x
	case cpu6502.StyName:
		value = regs.y
	default:
		return 0, knownByte{}, false
	}

	address, known := ins.address(regs)

	return address, value, known
}

func (ins flowInstruction) address(regs registers) (uint16, bool) {
	switch ins.op.Addressing {
	case cpu6502.AbsoluteAddressing, cpu6502.ZeroPageAddressing:
		return ins.operand, true
	case cpu6502.AbsoluteXAddressing:
		return ins.operand + uint16(regs.x.value), regs.x.known
	case cpu6502.AbsoluteYAddressing:
		return ins.operand + uint16(regs.y.value), regs.y.known
	default:
		return 0, false
	}
}

func (ins flowInstruction) apply(mem *mapper.Mapper, state mapper.Snapshot, regs registers) registers {
	value := ins.value(mem, state, regs)

	switch ins.op.Instruction.Name {
	case cpu6502.LdaName:
		regs.a = value
	case cpu6502.LdxName:
		regs.x = value
	case cpu6502.LdyName:
		regs.y = value
	case cpu6502.TaxName:
		regs.x = regs.a
	case cpu6502.TayName:
		regs.y = regs.a
	case cpu6502.TxaName:
		regs.a = regs.x
	case cpu6502.TyaName:
		regs.a = regs.y
	case cpu6502.InxName:
		regs.x.value++
	case cpu6502.DexName:
		regs.x.value--
	case cpu6502.InyName:
		regs.y.value++
	case cpu6502.DeyName:
		regs.y.value--
	default:
		regs = ins.applyAccumulator(regs, value)
	}

	return normalizeRegisters(regs)
}

func (ins flowInstruction) applyAccumulator(regs registers, value knownByte) registers {
	switch ins.op.Instruction.Name {
	case cpu6502.AndName:
		regs.a = combineKnown(regs.a, value, regs.a.value&value.value)
	case cpu6502.OraName:
		regs.a = combineKnown(regs.a, value, regs.a.value|value.value)
	case cpu6502.EorName:
		regs.a = combineKnown(regs.a, value, regs.a.value^value.value)
	case cpu6502.LsrName, cpu6502.AslName:
		regs.a = ins.shiftAccumulator(regs.a)
	case cpu6502.AdcName, cpu6502.SbcName, cpu6502.PlaName, cpu6502.RolName, cpu6502.RorName:
		regs.a = knownByte{}
	case cpu6502.TsxName:
		regs.x = knownByte{}

	default:
		if ins.op.Instruction.Unofficial {
			return registers{}
		}
	}

	return regs
}

func (ins flowInstruction) value(mem *mapper.Mapper, state mapper.Snapshot, regs registers) knownByte {
	if ins.op.Addressing == cpu6502.ImmediateAddressing {
		return knownByte{
			value: byte(ins.operand),
			known: true,
		}
	}

	address, known := ins.address(regs)
	if !known {
		return knownByte{}
	}

	value, ok := mem.ReadSnapshot(state, address)

	return knownByte{
		value: value,
		known: ok,
	}
}

func (ins flowInstruction) shiftAccumulator(value knownByte) knownByte {
	if ins.op.Addressing == cpu6502.AccumulatorAddressing {
		if ins.op.Instruction.Name == cpu6502.LsrName {
			value.value >>= 1
		} else {
			value.value <<= 1
		}
	}

	return value
}

func readFlowInstruction(mem *mapper.Mapper, state mapper.Snapshot, address uint16) (flowInstruction, bool) {
	code, ok := mem.ReadSnapshot(state, address)

	op := cpu6502.Opcodes[code]
	if !ok || op.Instruction == nil {
		return flowInstruction{}, false
	}

	ins := flowInstruction{
		op:   op,
		size: 2,
	}

	switch op.Addressing {
	case cpu6502.ImpliedAddressing, cpu6502.AccumulatorAddressing:
		ins.size = 1
	case cpu6502.AbsoluteAddressing, cpu6502.AbsoluteXAddressing, cpu6502.AbsoluteYAddressing, cpu6502.IndirectAddressing:
		ins.size = 3
	}

	if int(address)+ins.size > cpu6502.InterruptVectorStartAddress {
		return flowInstruction{}, false
	}

	for i := 1; i < ins.size; i++ {
		value, valid := mem.ReadSnapshot(state, address+uint16(i))
		if !valid {
			return flowInstruction{}, false
		}
		ins.operand |= uint16(value) << (8 * (i - 1))
	}

	return ins, true
}

// combineKnown returns a value that is only known if both inputs are known.
func combineKnown(left, right knownByte, value byte) knownByte {
	return knownByte{
		value: value,
		known: left.known && right.known,
	}
}

// joinRegisters merges the registers of two paths and drops values that differ.
func joinRegisters(left, right registers) registers {
	if left.a != right.a {
		left.a = knownByte{}
	}
	if left.x != right.x {
		left.x = knownByte{}
	}
	if left.y != right.y {
		left.y = knownByte{}
	}
	return left
}

func normalizeRegisters(regs registers) registers {
	if !regs.a.known {
		regs.a.value = 0
	}
	if !regs.x.known {
		regs.x.value = 0
	}
	if !regs.y.known {
		regs.y.value = 0
	}
	return regs
}
