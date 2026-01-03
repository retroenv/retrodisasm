package x86

import (
	"fmt"

	x86cpu "github.com/retroenv/retrogolib/arch/cpu/x86"
)

// formatOperands formats the operands for an x86 instruction.
func (a *X86) formatOperands(opcode Opcode, data []byte) string {
	if opcode.op == nil || opcode.op.Instruction == nil {
		return ""
	}

	// Handle register + immediate instructions (like MOV AH, imm8)
	if opcode.op.Register > 0 {
		regName := a.registerParamToString(opcode.op.Register)
		if len(data) > 1 {
			// Has immediate operand
			imm := a.formatImmediate(data[1:], int(opcode.op.Size)-1)
			return fmt.Sprintf("%s, %s", regName, imm)
		}
		return regName
	}

	// Handle pure immediate instructions (like INT imm8)
	addrMode := opcode.op.Addressing.String()
	if addrMode == "immediate" && len(data) > 1 {
		return a.formatImmediate(data[1:], int(opcode.op.Size)-1)
	}

	// Handle ModR/M based instructions
	if opcode.op.HasModRM && len(data) >= 2 {
		return a.formatModRM(data)
	}

	return ""
}

// registerParamToString converts a RegisterParam to its string representation.
func (a *X86) registerParamToString(reg x86cpu.RegisterParam) string {
	return reg.String()
}

// formatImmediate formats an immediate value from instruction bytes.
func (a *X86) formatImmediate(data []byte, size int) string {
	if len(data) == 0 {
		return "0x00"
	}

	switch size {
	case 1:
		return fmt.Sprintf("0x%02X", data[0])
	case 2:
		if len(data) >= 2 {
			// Little-endian word
			val := uint16(data[0]) | (uint16(data[1]) << 8)
			return fmt.Sprintf("0x%04X", val)
		}
		return fmt.Sprintf("0x%02X", data[0])
	default:
		return fmt.Sprintf("0x%02X", data[0])
	}
}

// formatModRM formats operands based on ModR/M byte.
func (a *X86) formatModRM(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	var modrm x86cpu.ModRM
	modrm.FromByte(data[1])

	// Get register names
	regName := a.getRegisterName(modrm.Reg, false) // assuming word registers for now
	rmOperand := a.getRMOperand(modrm.Mod, modrm.RM, data[2:])

	return fmt.Sprintf("%s, %s", regName, rmOperand)
}

// getRegisterName returns the register name for a register number.
func (a *X86) getRegisterName(reg byte, isByte bool) string {
	if isByte {
		byteRegs := []string{"al", "cl", "dl", "bl", "ah", "ch", "dh", "bh"}
		if reg < 8 {
			return byteRegs[reg]
		}
	}
	wordRegs := []string{"ax", "cx", "dx", "bx", "sp", "bp", "si", "di"}
	if reg < 8 {
		return wordRegs[reg]
	}
	return "??"
}

// getRMOperand formats the R/M operand based on mod and r/m fields.
func (a *X86) getRMOperand(mod, rm byte, dispBytes []byte) string {
	switch mod {
	case 0: // Memory, no displacement (except rm=110)
		if rm == 6 {
			// Direct address
			if len(dispBytes) >= 2 {
				addr := uint16(dispBytes[0]) | (uint16(dispBytes[1]) << 8)
				return fmt.Sprintf("[0x%04X]", addr)
			}
			return "[disp16]"
		}
		return a.getEffectiveAddress(rm, 0, nil)
	case 1: // Memory + 8-bit displacement
		if len(dispBytes) >= 1 {
			return a.getEffectiveAddress(rm, 1, dispBytes[:1])
		}
		return a.getEffectiveAddress(rm, 1, nil)
	case 2: // Memory + 16-bit displacement
		if len(dispBytes) >= 2 {
			return a.getEffectiveAddress(rm, 2, dispBytes[:2])
		}
		return a.getEffectiveAddress(rm, 2, nil)
	case 3: // Register
		return a.getRegisterName(rm, false)
	}
	return "??"
}

// getEffectiveAddress formats an effective address from r/m and displacement.
func (a *X86) getEffectiveAddress(rm byte, dispSize int, dispBytes []byte) string {
	baseAddr := []string{
		"bx+si", "bx+di", "bp+si", "bp+di",
		"si", "di", "bp", "bx",
	}

	if rm >= 8 {
		return "[??]"
	}

	base := baseAddr[rm]

	switch dispSize {
	case 0:
		return fmt.Sprintf("[%s]", base)
	case 1:
		if len(dispBytes) >= 1 {
			disp := int8(dispBytes[0])
			if disp >= 0 {
				return fmt.Sprintf("[%s+0x%02X]", base, disp)
			}
			return fmt.Sprintf("[%s-0x%02X]", base, -disp)
		}
		return fmt.Sprintf("[%s+disp8]", base)
	case 2:
		if len(dispBytes) >= 2 {
			disp := uint16(dispBytes[0]) | (uint16(dispBytes[1]) << 8)
			return fmt.Sprintf("[%s+0x%04X]", base, disp)
		}
		return fmt.Sprintf("[%s+disp16]", base)
	}

	return fmt.Sprintf("[%s]", base)
}
