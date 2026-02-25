package disasm

import (
	"fmt"
	"slices"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	cpum6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
)

const (
	funcNaming       = "_func_%04x"
	jumpEngineNaming = "_jump_engine_%04x"
	labelNaming      = "_label_%04x"
)

// processJumpDestinations processes all jump destinations and updates the callers with
// the generated jump destination label name.
func (dis *Disasm) processJumpDestinations() {
	branchDestinations := dis.sortedBranchDestinations()
	labelOwners := map[string]*offset.DisasmOffset{}
	defaultMappingSignature := dis.defaultMappingSignature()

	for _, key := range branchDestinations {
		destinationInfo := dis.branchDestinationInfo[key]
		if destinationInfo == nil {
			continue
		}
		canRewriteCallersToLabel := key.MappingID == defaultMappingSignature &&
			dis.canRewriteCallersToBranchLabel(key, destinationInfo)

		name := dis.resolveDestinationLabel(key.PC, destinationInfo, key, labelOwners)
		dis.applyDestinationLabel(key.PC, name, canRewriteCallersToLabel, destinationInfo)
	}
}

// sortedBranchDestinations returns branch destination keys sorted by PC then MappingID.
func (dis *Disasm) sortedBranchDestinations() []ParseKey {
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
	return branchDestinations
}

// resolveDestinationLabel determines the unique label name for a branch destination.
func (dis *Disasm) resolveDestinationLabel(
	address uint16, destinationInfo *offset.DisasmOffset, key ParseKey,
	labelOwners map[string]*offset.DisasmOffset,
) string {

	name := destinationInfo.Label
	if name == "" {
		switch {
		case destinationInfo.IsType(program.JumpEngine):
			name = fmt.Sprintf(jumpEngineNaming, address)
		case destinationInfo.IsType(program.CallDestination) && !isSuspectCallDestination(destinationInfo):
			name = fmt.Sprintf(funcNaming, address)
		default:
			name = fmt.Sprintf(labelNaming, address)
		}
	}
	name = dis.uniqueLabelName(name, key, destinationInfo, labelOwners)
	destinationInfo.Label = name
	return name
}

// isSuspectCallDestination returns true if a CallDestination offset's first byte
// is BRK or an unofficial opcode, indicating the label is likely a false positive
// created when a JSR/JMP target in the fixed bank pointed to data in another
// bank mapping. These are downgraded from _func_ to _label_ naming.
func isSuspectCallDestination(info *offset.DisasmOffset) bool {
	if len(info.Data) == 0 {
		return false
	}
	op := info.Data[0]
	opcode := cpum6502.Opcodes[op]
	if opcode.Instruction == nil || opcode.Instruction.Unofficial {
		return true
	}
	if opcode.Instruction.Name == cpum6502.BrkName {
		return true
	}
	return false
}

// applyDestinationLabel applies the resolved label to the destination info and its callers.
func (dis *Disasm) applyDestinationLabel(
	address uint16, name string, canRewriteCallersToLabel bool, destinationInfo *offset.DisasmOffset,
) {
	// if the offset is marked as code but does not have opcode bytes, the jump destination
	// is inside the second or third byte of an instruction.
	if (destinationInfo.IsType(program.CodeOffset) || destinationInfo.IsType(program.CodeAsData)) &&
		len(destinationInfo.Data) == 0 {

		dis.handleJumpIntoInstruction(address)
	}

	for _, bankRef := range destinationInfo.BranchFrom {
		callerInfo := bankRef.Mapped.OffsetInfo(bankRef.Index)
		rewriteAbsoluteToStableLabel := len(callerInfo.Data) >= 3 &&
			dis.isStableEmittedLabelAddress(address, destinationInfo)
		if !canRewriteCallersToLabel && len(callerInfo.Data) >= 3 && !rewriteAbsoluteToStableLabel {
			continue
		}
		callerInfo.BranchingTo = name

		// reference can be a function address of a jump engine
		if callerInfo.IsType(program.CodeOffset) && callerInfo.Opcode != nil {
			callerInfo.Code = callerInfo.Opcode.Instruction().Name()
		}
	}
}

func (dis *Disasm) isStableEmittedLabelAddress(targetAddress uint16, targetInfo *offset.DisasmOffset) bool {
	emittedAddress, ok := dis.mapper.EmittedAddressOfOffset(targetInfo)
	return ok && emittedAddress == targetAddress
}

func (dis *Disasm) defaultMappingSignature() uint64 {
	current := dis.mapper.MappingSignature()
	defer func() {
		if !dis.mapper.RestoreMappingSignature(current) {
			dis.mapper.RestoreDefaultMapping()
		}
	}()

	dis.mapper.RestoreDefaultMapping()
	return dis.mapper.MappingSignature()
}

func (dis *Disasm) canRewriteCallersToBranchLabel(key ParseKey, owner *offset.DisasmOffset) bool {
	current := dis.mapper.MappingSignature()
	restoreCurrent := func() {
		if !dis.mapper.RestoreMappingSignature(current) {
			dis.mapper.RestoreDefaultMapping()
		}
	}
	defer restoreCurrent()

	if !dis.mapper.RestoreMappingSignature(key.MappingID) {
		return false
	}

	resolved := dis.mapper.OffsetInfo(key.PC)
	return resolved != nil && resolved == owner
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
