package x86

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// formatOffsetCode formats the instruction code string for display in NASM Intel syntax.
func (ar *ArchX86) formatOffsetCode(offsetInfo *offset.DisasmOffset, instr instruction.Instruction) {
	name := instr.Name()
	if name == "" {
		offsetInfo.Code = formatDataBytes(offsetInfo.Data)
		return
	}

	opcode, ok := offsetInfo.Opcode.(Opcode)
	if !ok {
		offsetInfo.Code = name
		return
	}

	params := ar.formatOperands(offsetInfo, opcode)
	if params != "" {
		offsetInfo.Code = fmt.Sprintf("%s %s", name, params)
	} else {
		offsetInfo.Code = name
	}

	// Add DOS interrupt annotation if applicable
	if instr.Name() == x86.IntName && len(offsetInfo.Data) >= 2 {
		intNum := offsetInfo.Data[1]
		if comment := ar.getDOSInterruptComment(intNum); comment != "" {
			offsetInfo.Comment = comment
		}
	}
}

// formatOperands formats instruction operands based on addressing mode.
func (ar *ArchX86) formatOperands(offsetInfo *offset.DisasmOffset, opcode Opcode) string {
	data := offsetInfo.Data
	if len(data) == 0 {
		return ""
	}

	addressing := x86.AddressingMode(opcode.Addressing())

	switch addressing {
	case x86.ImpliedAddressing:
		return ""

	case x86.RegisterAddressing:
		return ar.formatRegisterOperand(opcode)

	case x86.ImmediateAddressing:
		return ar.formatImmediateOperand(data, opcode)

	case x86.RelativeAddressing:
		return ar.formatRelativeOperand(data, offsetInfo)

	case x86.DirectAddressing:
		return ar.formatDirectOperand(data)

	case x86.ModRMRegisterAddressing, x86.ModRMMemoryAddressing, x86.ModRMImmediateAddressing:
		return ar.formatModRMOperand(data, opcode)

	case x86.StringAddressing:
		return "" // String operations have implicit operands

	case x86.SegmentOffsetAddressing:
		return ar.formatSegmentOffset(data)
	}

	return ""
}

// formatRegisterOperand formats a register operand.
func (ar *ArchX86) formatRegisterOperand(opcode Opcode) string {
	reg := opcode.Register()
	if name, ok := x86.RegisterParamNames[reg]; ok {
		return name
	}
	return ""
}

// formatImmediateOperand formats an immediate value operand.
func (ar *ArchX86) formatImmediateOperand(data []byte, opcode Opcode) string {
	reg := opcode.Register()

	// Check if this is a register + immediate instruction
	if reg != 0 {
		regName := x86.RegisterParamNames[reg]

		// Determine immediate size based on register size
		if reg.Is8Bit() && len(data) >= 2 {
			return fmt.Sprintf("%s, 0x%02x", regName, data[1])
		}
		if reg.Is16Bit() && len(data) >= 3 {
			imm := uint16(data[2])<<8 | uint16(data[1])
			return fmt.Sprintf("%s, 0x%04x", regName, imm)
		}
	}

	// Pure immediate instruction
	if len(data) >= 2 {
		return fmt.Sprintf("0x%02x", data[1])
	}
	return ""
}

// formatRelativeOperand formats a relative address operand (for jumps/calls).
func (ar *ArchX86) formatRelativeOperand(data []byte, offsetInfo *offset.DisasmOffset) string {
	pc := ar.dis.ProgramCounter()

	if len(data) == 2 {
		// 8-bit relative offset
		rel := int8(data[1])
		target := pc + uint16(len(data)) + uint16(int16(rel))
		// Check if there's a label for this address
		if offsetInfo.BranchingTo != "" {
			return offsetInfo.BranchingTo
		}
		return fmt.Sprintf("0x%04x", target)
	}

	if len(data) >= 3 {
		// 16-bit relative offset
		rel := int16(uint16(data[2])<<8 | uint16(data[1]))
		target := pc + uint16(len(data)) + uint16(rel)
		if offsetInfo.BranchingTo != "" {
			return offsetInfo.BranchingTo
		}
		return fmt.Sprintf("0x%04x", target)
	}

	return ""
}

// formatDirectOperand formats a direct memory address operand.
func (ar *ArchX86) formatDirectOperand(data []byte) string {
	if len(data) >= 3 {
		addr := uint16(data[2])<<8 | uint16(data[1])
		return fmt.Sprintf("[0x%04x]", addr)
	}
	return ""
}

// formatModRMOperand formats operands that use ModR/M encoding.
func (ar *ArchX86) formatModRMOperand(data []byte, opcode Opcode) string {
	if len(data) < 2 {
		return ""
	}

	var modrm x86.ModRM
	modrm.FromByte(data[1])

	// Get instruction name to determine operand order
	name := opcode.op.Instruction.Name

	// Decode operands based on ModR/M
	regOp := formatRegister(modrm.Reg, is16BitInstruction(data[0]))
	rmOp := ar.formatRMOperand(data, modrm, is16BitInstruction(data[0]))

	// Determine operand order based on opcode direction bit (bit 1)
	// d=0: r/m is destination, reg is source
	// d=1: reg is destination, r/m is source
	directionBit := (data[0] >> 1) & 1

	if directionBit == 0 {
		// r/m, reg
		return fmt.Sprintf("%s, %s", rmOp, regOp)
	}

	// reg, r/m (or special cases like LEA, LES, LDS)
	switch name {
	case x86.LeaName:
		return fmt.Sprintf("%s, %s", regOp, rmOp)
	default:
		return fmt.Sprintf("%s, %s", regOp, rmOp)
	}
}

// formatRMOperand formats the R/M operand based on ModR/M byte.
func (ar *ArchX86) formatRMOperand(data []byte, modrm x86.ModRM, is16Bit bool) string {
	switch modrm.Mod {
	case 3:
		// Register mode
		return formatRegister(modrm.RM, is16Bit)

	case 0:
		// Memory mode, no displacement (except R/M=6 is direct address)
		if modrm.RM == 6 && len(data) >= 4 {
			addr := uint16(data[3])<<8 | uint16(data[2])
			return fmt.Sprintf("[0x%04x]", addr)
		}
		return formatMemoryRef(modrm.RM)

	case 1:
		// Memory mode, 8-bit displacement
		if len(data) >= 3 {
			disp := int8(data[2])
			return formatMemoryRefWithDisp(modrm.RM, int16(disp))
		}
		return formatMemoryRef(modrm.RM)

	case 2:
		// Memory mode, 16-bit displacement
		if len(data) >= 4 {
			disp := int16(uint16(data[3])<<8 | uint16(data[2]))
			return formatMemoryRefWithDisp(modrm.RM, disp)
		}
		return formatMemoryRef(modrm.RM)
	}

	return ""
}

// formatRegister returns the register name for a 3-bit register field.
func formatRegister(reg uint8, is16Bit bool) string {
	if is16Bit {
		regs := []string{"ax", "cx", "dx", "bx", "sp", "bp", "si", "di"}
		if int(reg) < len(regs) {
			return regs[reg]
		}
	} else {
		regs := []string{"al", "cl", "dl", "bl", "ah", "ch", "dh", "bh"}
		if int(reg) < len(regs) {
			return regs[reg]
		}
	}
	return ""
}

// formatMemoryRef formats a memory reference without displacement.
func formatMemoryRef(rm uint8) string {
	refs := []string{
		"[bx+si]", "[bx+di]", "[bp+si]", "[bp+di]",
		"[si]", "[di]", "[bp]", "[bx]",
	}
	if int(rm) < len(refs) {
		return refs[rm]
	}
	return ""
}

// formatMemoryRefWithDisp formats a memory reference with displacement.
func formatMemoryRefWithDisp(rm uint8, disp int16) string {
	bases := []string{
		"bx+si", "bx+di", "bp+si", "bp+di",
		"si", "di", "bp", "bx",
	}
	if int(rm) >= len(bases) {
		return ""
	}

	if disp == 0 {
		return fmt.Sprintf("[%s]", bases[rm])
	} else if disp > 0 {
		return fmt.Sprintf("[%s+0x%x]", bases[rm], disp)
	}
	return fmt.Sprintf("[%s-0x%x]", bases[rm], -disp)
}

// formatSegmentOffset formats a far pointer (segment:offset).
func (ar *ArchX86) formatSegmentOffset(data []byte) string {
	if len(data) >= 5 {
		offset := uint16(data[2])<<8 | uint16(data[1])
		segment := uint16(data[4])<<8 | uint16(data[3])
		return fmt.Sprintf("0x%04x:0x%04x", segment, offset)
	}
	return ""
}

// is16BitInstruction determines if the instruction operates on 16-bit operands.
func is16BitInstruction(opcode uint8) bool {
	// Bit 0 of many opcodes indicates operand size:
	// 0 = 8-bit, 1 = 16-bit
	return (opcode & 1) == 1
}

// formatDataBytes formats raw data bytes for display.
func formatDataBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) == 1 {
		return fmt.Sprintf("db 0x%02x", data[0])
	}
	return fmt.Sprintf("db 0x%02x", data[0])
}
