package disasm

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/set"
)

const (
	funcNaming       = "_func_%04x"
	jumpEngineNaming = "_jump_engine_%04x"
	labelNaming      = "_label_%04x"
)

// processJumpDestinations processes all jump destinations and updates the callers with
// the generated jump destination label name.
func (dis *Disasm) processJumpDestinations() {
	for _, address := range set.Sorted(dis.branchDestinations) {
		offsetInfo := dis.mapper.OffsetInfo(address)

		name := offsetInfo.Label
		if name == "" {
			switch {
			case offsetInfo.IsType(program.JumpEngine):
				name = fmt.Sprintf(jumpEngineNaming, address)
			case offsetInfo.IsType(program.CallDestination):
				name = fmt.Sprintf(funcNaming, address)
			default:
				name = fmt.Sprintf(labelNaming, address)
			}
			offsetInfo.Label = name
		}

		// An empty owned offset places the destination inside a multi-byte
		// instruction or function-pointer record, which must be split for its label.
		if offsetInfo.IsType(program.CodeOffset|program.CodeAsData|program.FunctionReference) &&
			len(offsetInfo.Data) == 0 {

			dis.splitInstructionAt(address, "branch into instruction detected")
		}

		for _, bankRef := range offsetInfo.BranchFrom {
			offsetInfo = bankRef.Mapped.OffsetInfo(bankRef.Index)
			offsetInfo.BranchingTo = name

			// reference can be a function address of a jump engine
			if offsetInfo.IsType(program.CodeOffset) {
				offsetInfo.Code = dis.arch.FormatBranchReference(offsetInfo, name)
				offsetInfo.BranchingTo = ""
			}
		}
	}
}

// splitInstructionAt converts an instruction that owns address into data bytes.
// reason explains why it must be split; annotations do not require a diagnostic.
func (dis *Disasm) splitInstructionAt(address uint16, reason string) {
	// look backwards for instruction start
	address--

	for offsetInfo := dis.mapper.OffsetInfo(address); len(offsetInfo.Data) == 0; {
		address--
		offsetInfo = dis.mapper.OffsetInfo(address)
	}

	offsetInfo := dis.mapper.OffsetInfo(address)
	if offsetInfo.Code == "" { // disambiguous instruction
		if reason != "" {
			offsetInfo.Comment = reason + ": " + offsetInfo.Comment
		}
	} else {
		comment := offsetInfo.Code
		if reason != "" {
			comment = reason + ": " + comment
		}
		if offsetInfo.Comment != "" {
			comment += " | " + offsetInfo.Comment
		}
		offsetInfo.Comment = comment
		offsetInfo.Code = ""
	}

	offsetInfo.SetType(program.CodeAsData)
	dis.ChangeAddressRangeToCodeAsData(address, offsetInfo.Data)
}

// changeAddressRangeToCode sets a range of code addresses to code types.
func (dis *Disasm) changeAddressRangeToCode(address uint16, data []byte) {
	lastCodeAddress := dis.arch.LastCodeAddress()
	for i := 0; i < len(data) && int(address)+i < int(lastCodeAddress); i++ {
		offsetInfo := dis.mapper.OffsetInfo(address + uint16(i))
		if i > 0 {
			// The instruction start owns the complete encoding. Operand offsets may
			// still carry preliminary data-table classification from an earlier trace.
			offsetInfo.Data = nil
			offsetInfo.ClearType(program.DataOffset)
		}
		offsetInfo.SetType(program.CodeOffset)
	}
}
