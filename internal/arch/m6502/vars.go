package m6502

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
)

func (ar *Arch6502) ProcessVariableUsage(offsetInfo *offset.DisasmOffset, reference string) error {
	addressing := cpu6502.AddressingMode(offsetInfo.Opcode.Addressing())
	converted, err := parameter.String(ar.converter, addressing, reference)
	if err != nil {
		return fmt.Errorf("getting parameter as string: %w", err)
	}

	name := offsetInfo.Opcode.Instruction().Name()
	switch addressing {
	case cpu6502.ZeroPageAddressing, cpu6502.ZeroPageXAddressing, cpu6502.ZeroPageYAddressing:
		offsetInfo.Code = fmt.Sprintf("%s %s", name, converted)
	case cpu6502.AbsoluteAddressing, cpu6502.AbsoluteXAddressing, cpu6502.AbsoluteYAddressing:
		offsetInfo.Code = fmt.Sprintf("%s %s", name, converted)
	case cpu6502.IndirectAddressing, cpu6502.IndirectXAddressing, cpu6502.IndirectYAddressing:
		offsetInfo.Code = fmt.Sprintf("%s %s", name, converted)
	}

	return nil
}
