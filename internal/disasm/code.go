package disasm

import (
	"fmt"
	"slices"

	"github.com/retroenv/retrodisasm/internal/offset"
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
	branchDestinations := make([]ParseKey, 0, len(dis.branchDestinations))
	for key := range dis.branchDestinations {
		branchDestinations = append(branchDestinations, key)
	}
	slices.SortFunc(branchDestinations, func(a, b ParseKey) int {
		if a.PC < b.PC {
			return -1
		}
		if a.PC > b.PC {
			return 1
		}
		if a.MappingID < b.MappingID {
			return -1
		}
		if a.MappingID > b.MappingID {
			return 1
		}
		return 0
	})

	labelOwners := map[string]*offset.DisasmOffset{}

	for _, key := range branchDestinations {
		address := key.PC
		offsetInfo := dis.branchDestinationInfo[key]
		if offsetInfo == nil {
			continue
		}

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
		}
		name = dis.uniqueLabelName(name, key, offsetInfo, labelOwners)
		offsetInfo.Label = name

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

func (dis *Disasm) uniqueLabelName(base string, key ParseKey, owner *offset.DisasmOffset,
	owners map[string]*offset.DisasmOffset) string {
	if existingOwner, ok := owners[base]; !ok || existingOwner == owner {
		owners[base] = owner
		return base
	}

	qualified := fmt.Sprintf("%s_m%04x", base, uint16(key.MappingID))
	name := qualified
	for i := 1; ; i++ {
		if existingOwner, ok := owners[name]; !ok || existingOwner == owner {
			owners[name] = owner
			return name
		}
		name = fmt.Sprintf("%s_%d", qualified, i)
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
		dis.stats.codeBytesMarked++
	}
}
