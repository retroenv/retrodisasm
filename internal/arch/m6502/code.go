package m6502

import (
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
)

// HandleDisambiguousInstructions translates disambiguous instructions into data bytes as it
// has multiple opcodes for the same addressing mode which can result in different
// bytes being assembled and make the resulting ROM not matching the original.
func (ar *Arch6502) HandleDisambiguousInstructions(address uint16, offsetInfo *offset.DisasmOffset) bool {
	instruction := offsetInfo.Opcode.Instruction()
	if !instruction.Unofficial() || address >= cpu6502.InterruptVectorStartAddress {
		return false
	}

	opts := ar.dis.Options()

	// Output as mnemonic if explicitly requested
	if opts.OutputUnofficialAsMnemonics {
		return false
	}

	if instruction.Name() != cpu6502.NopName &&
		instruction.Name() != cpu6502.SbcName &&
		opts.AssemblerSupportsUnofficial {

		return false
	}

	code := offsetInfo.Code
	if code == "" { // in case of branch into unofficial nop instruction detected
		offsetInfo.Comment = "disambiguous instruction: " + offsetInfo.Comment
	} else {
		offsetInfo.Comment = "disambiguous instruction: " + offsetInfo.Code
	}

	offsetInfo.Code = ""
	offsetInfo.SetType(program.CodeAsData)
	ar.dis.ChangeAddressRangeToCodeAsData(address, offsetInfo.Data)
	return true
}
