package disasm

import (
	"fmt"
	"slices"

	"github.com/retroenv/retrodisasm/internal/program"
)

const (
	funcNaming       = "_func_%04x"
	jumpEngineNaming = "_jump_engine_%04x"
	labelNaming      = "_label_%04x"
)

// processJumpDestinations processes all jump destinations and updates the callers with
// the generated jump destination label name.
func (dis *Disasm) processJumpDestinations() {
	branchDestinations := make([]uint16, 0, len(dis.branchDestinations))
	for dest := range dis.branchDestinations {
		branchDestinations = append(branchDestinations, dest)
	}
	slices.Sort(branchDestinations)

	for _, address := range branchDestinations {
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

		// if the offset is marked as code but does not have opcode bytes, the jump destination
		// is inside the second or third byte of an instruction.
		if (offsetInfo.IsType(program.CodeOffset) || offsetInfo.IsType(program.CodeAsData)) &&
			len(offsetInfo.Data) == 0 {

			dis.handleJumpIntoInstruction(address)
		}

		for _, bankRef := range offsetInfo.BranchFrom {
			offsetInfo = bankRef.Mapped.OffsetInfo(bankRef.Index)
			offsetInfo.BranchingTo = name

			// reference can be a function address of a jump engine
			if offsetInfo.IsType(program.CodeOffset) {
				offsetInfo.Code = offsetInfo.Opcode.Instruction().Name()
			}
		}
	}
}

// processBankBranchDestinations processes branch destinations for the currently mapped bank.
// This must be called while the bank is still mapped so labels are set on the correct offsets.
// Only processes addresses in switchable regions; fixed regions are handled by processJumpDestinations.
func (dis *Disasm) processBankBranchDestinations() {
	bankIndex := dis.currentBankIndex

	for address := range dis.branchDestinations {
		// Skip fixed addresses - they'll be processed once with the default mapping
		if dis.mapper.IsAddressFixed(address) {
			continue
		}

		offsetInfo := dis.mapper.OffsetInfo(address)
		if offsetInfo == nil {
			continue
		}

		// Use existing label or generate bank-specific label
		name := offsetInfo.Label
		if name == "" {
			switch {
			case offsetInfo.IsType(program.JumpEngine):
				name = fmt.Sprintf("_jump_engine_%04x_b%d", address, bankIndex)
			case offsetInfo.IsType(program.CallDestination):
				name = fmt.Sprintf("_func_%04x_b%d", address, bankIndex)
			default:
				name = fmt.Sprintf("_label_%04x_b%d", address, bankIndex)
			}
			offsetInfo.Label = name

			// Handle jump into instruction: if offset is code but has no opcode bytes,
			// it's inside another instruction
			if (offsetInfo.IsType(program.CodeOffset) || offsetInfo.IsType(program.CodeAsData)) &&
				len(offsetInfo.Data) == 0 {

				dis.handleJumpIntoInstruction(address)
			}
		}

		// Update callers to reference this label
		for _, bankRef := range offsetInfo.BranchFrom {
			callerInfo := bankRef.Mapped.OffsetInfo(bankRef.Index)
			callerInfo.BranchingTo = name

			if callerInfo.IsType(program.CodeOffset) {
				callerInfo.Code = callerInfo.Opcode.Instruction().Name()
			}
		}
	}
}

// handleJumpIntoInstruction converts an instruction that has a jump destination label inside
// its second or third opcode bytes into data.
func (dis *Disasm) handleJumpIntoInstruction(address uint16) {
	// look backwards for instruction start
	address--

	for offsetInfo := dis.mapper.OffsetInfo(address); len(offsetInfo.Data) == 0; {
		address--
		offsetInfo = dis.mapper.OffsetInfo(address)
	}

	offsetInfo := dis.mapper.OffsetInfo(address)
	if offsetInfo.Code == "" { // disambiguous instruction
		offsetInfo.Comment = "branch into instruction detected: " + offsetInfo.Comment
	} else {
		offsetInfo.Comment = "branch into instruction detected: " + offsetInfo.Code
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
		offsetInfo.SetType(program.CodeOffset)
	}
}
