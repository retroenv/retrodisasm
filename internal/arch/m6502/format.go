package m6502

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
)

// FormatBranchReference formats an instruction with a branch destination label.
func (ar *Arch6502) FormatBranchReference(offsetInfo *offset.DisasmOffset, label string) string {
	return offset.FormatBranchReference(offsetInfo, label)
}

// ApplyOperand uses a configured expression only when it matches the encoded operand.
func (ar *Arch6502) ApplyOperand(address uint16, info *offset.DisasmOffset, expression string, value uint16) error {
	if info.Opcode == nil || !info.IsType(program.CodeOffset) || len(info.Data) < 2 {
		return fmt.Errorf("operand hint at $%04X does not name an operand-bearing instruction", address)
	}
	mode := cpu6502.AddressingMode(info.Opcode.Addressing())
	expected := uint16(info.Data[1])
	if len(info.Data) == 3 {
		expected |= uint16(info.Data[2]) << 8
	}
	if mode == cpu6502.RelativeAddressing {
		expected = uint16(int(address) + 2 + int(int8(info.Data[1])))
	}
	if value != expected {
		return fmt.Errorf("operand hint at $%04X evaluates to $%04X; ROM encodes $%04X", address, value, expected)
	}
	expression = assembler.FormatExpression(ar.options.Assembler, expression)
	formatted, err := ar.formatConfiguredOperand(mode, expression)
	if err != nil {
		return fmt.Errorf("formatting configured operand: %w", err)
	}
	info.Code = info.Opcode.Instruction().Name() + " " + formatted
	info.BranchingTo = ""
	return nil
}

func (ar *Arch6502) formatConfiguredOperand(mode cpu6502.AddressingMode, expression string) (string, error) {
	switch mode {
	case cpu6502.ImmediateAddressing:
		return "#" + expression, nil
	case cpu6502.RelativeAddressing:
		return expression, nil
	default:
		result, err := parameter.String(ar.converter, mode, expression)
		if err != nil {
			return "", fmt.Errorf("formatting operand: %w", err)
		}
		return result, nil
	}
}
