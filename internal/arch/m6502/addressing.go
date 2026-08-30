package m6502

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
)

// AddressingParam returns the address of the param if it references an address.
func (ar *Arch6502) AddressingParam(param any) (uint16, bool) {
	switch val := param.(type) {
	case cpu6502.Absolute:
		return uint16(val), true
	case cpu6502.AbsoluteX:
		return uint16(val), true
	case cpu6502.AbsoluteY:
		return uint16(val), true
	case cpu6502.Indirect:
		return uint16(val), true
	case cpu6502.IndirectX:
		return uint16(val), true
	case cpu6502.IndirectY:
		return uint16(val), true
	case cpu6502.ZeroPage:
		return uint16(val), true
	case cpu6502.ZeroPageX:
		return uint16(val), true
	case cpu6502.ZeroPageY:
		return uint16(val), true
	default:
		return 0, false
	}
}

// IsAddressingIndexed returns if the opcode is using indexed addressing.
func (ar *Arch6502) IsAddressingIndexed(opcode instruction.Opcode) bool {
	addressing := cpu6502.AddressingMode(opcode.Addressing())
	switch addressing {
	case cpu6502.ZeroPageXAddressing, cpu6502.ZeroPageYAddressing,
		cpu6502.AbsoluteXAddressing, cpu6502.AbsoluteYAddressing,
		cpu6502.IndirectXAddressing, cpu6502.IndirectYAddressing:
		return true
	default:
		return false
	}
}
