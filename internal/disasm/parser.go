package disasm

import (
	"context"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
)

// AddAddressToParse adds an address to the list to be processed if the address has not been processed yet.
func (dis *Disasm) AddAddressToParse(address, context, from uint16,
	currentInstruction instruction.Instruction, isABranchDestination bool) {
	dis.stats.queueRequests++

	if !dis.isValidCodeAddress(address) {
		dis.stats.queueRejectedInvalid++
		return
	}

	offsetInfo := dis.mapper.OffsetInfo(address)
	key := dis.currentParseKey(address)
	if isABranchDestination && currentInstruction != nil && currentInstruction.IsCall() {
		offsetInfo.SetType(program.CallDestination)
		if offsetInfo.Context == 0 {
			offsetInfo.Context = address // begin a new context
		}
	} else if offsetInfo.Context == 0 {
		offsetInfo.Context = context // continue current context
	}

	if isABranchDestination {
		// Always add BranchFrom references when isABranchDestination is true.
		// Initialization calls pass isABranchDestination = false, so they're already filtered out.
		bankRef := offset.BankReference{
			Mapped:  dis.mapper.MappedBank(from),
			Address: from,
			Index:   dis.mapper.MappedBankIndex(from),
		}
		bankRef.ID = bankRef.Mapped.ID()
		offsetInfo.BranchFrom = append(offsetInfo.BranchFrom, bankRef)
		dis.branchDestinations.Add(key)
		dis.branchDestinationInfo[key] = offsetInfo
		dis.stats.branchDestinationsAdded++
	}

	if dis.offsetsToParseAdded.Contains(key) {
		dis.stats.queueRejectedDuplicate++
		return
	}
	dis.offsetsToParseAdded.Add(key)

	// add instructions that follow a function call to a special queue with lower priority, to allow the
	// jump engine be detected before trying to parse the data following the call, which in case of a jump
	// engine is not code but pointers to functions.
	if currentInstruction != nil && currentInstruction.IsCall() {
		dis.functionReturnsToParse = append(dis.functionReturnsToParse, key)
		dis.functionReturnsToParseAdded.Add(key)
		dis.stats.queueAddedFunctionRet++
	} else {
		dis.offsetsToParse = append(dis.offsetsToParse, key)
		dis.stats.queueAddedPrimary++
	}
}

// DeleteFunctionReturnToParse deletes a function return address from the list of addresses to parse.
func (dis *Disasm) DeleteFunctionReturnToParse(address uint16) {
	for key := range dis.functionReturnsToParseAdded {
		if key.PC == address {
			dis.functionReturnsToParseAdded.Remove(key)
		}
	}
}

// isValidCodeAddress checks if an address is within valid code bounds.
func (dis *Disasm) isValidCodeAddress(address uint16) bool {
	// Ignore branching into addresses before the code base address, for example when generating code in
	// zeropage and branching into it to execute it.
	if address < dis.codeBaseAddress {
		return false
	}

	// For binary mode, also check upper bound (code base + code size)
	if dis.options.Binary && dis.cart != nil {
		codeEnd := dis.codeBaseAddress + uint16(len(dis.cart.PRG))
		if address >= codeEnd {
			return false
		}
	}

	// Don't follow branches to addresses marked as unreachable (dead code)
	if dis.unreachableAddresses.Contains(address) {
		return false
	}

	return true
}

// followExecutionFlow parses opcodes and follows the execution flow to parse all code.
func (dis *Disasm) followExecutionFlow(ctx context.Context) error {
	for {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("disassembly cancelled: %w", ctx.Err())
		default:
			// Continue processing
		}

		key, ok, err := dis.addressToDisassemble()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		address := key.PC
		dis.mapper.RestoreMappingSignature(key.MappingID)

		if dis.offsetsParsed.Contains(key) {
			dis.stats.alreadyParsedSkips++
			continue
		}
		dis.offsetsParsed.Add(key)
		dis.stats.parsedOffsets++

		dis.pc = address
		offsetInfo := dis.mapper.OffsetInfo(dis.pc)

		inspectCode, err := dis.arch.ProcessOffset(address, offsetInfo)
		if err != nil {
			return fmt.Errorf("error processing offset at address %04x: %w", address, err)
		}
		if !inspectCode {
			dis.stats.inspectSkipped++
			continue
		}

		dis.checkInstructionOverlap(address, offsetInfo)

		if dis.arch.HandleDisambiguousInstructions(address, offsetInfo) {
			dis.stats.disambiguousHandled++
			continue
		}

		dis.changeAddressRangeToCode(address, offsetInfo.Data)
	}
	return nil
}

// in case the current instruction overlaps with an already existing instruction,
// cut the current one short.
func (dis *Disasm) checkInstructionOverlap(address uint16, offsetInfo *offset.DisasmOffset) {
	for i := 1; i < len(offsetInfo.Data) && int(address)+i < int(dis.arch.LastCodeAddress()); i++ {
		followingAddress := address + uint16(i)
		offsetInfoFollowing := dis.mapper.OffsetInfo(followingAddress)

		// Check for regular code overlap or CodeAsData that's a branch destination
		// (CodeAsData that's NOT a branch destination can be consumed by this instruction)
		isOverlap := offsetInfoFollowing.IsType(program.CodeOffset) ||
			(offsetInfoFollowing.IsType(program.CodeAsData) && dis.isBranchDestination(followingAddress))

		if !isOverlap {
			continue
		}

		offsetInfoFollowing.Comment = "branch into instruction detected"
		offsetInfo.Comment = offsetInfo.Code
		offsetInfo.Data = offsetInfo.Data[:i]
		offsetInfo.Code = ""
		offsetInfo.ClearType(program.CodeOffset)
		offsetInfo.SetType(program.CodeAsData | program.DataOffset)
		dis.stats.overlapDetected++
		return
	}
}

// isBranchDestination checks if an address is a branch destination.
func (dis *Disasm) isBranchDestination(address uint16) bool {
	return dis.branchDestinations.Contains(dis.currentParseKey(address))
}

// addressToDisassemble returns the next parse key to disassemble.
// Return addresses from function calls have the lowest priority, to be able to
// handle jump table functions correctly.
func (dis *Disasm) addressToDisassemble() (ParseKey, bool, error) {
	for {
		if len(dis.offsetsToParse) > 0 {
			key := dis.offsetsToParse[0]
			dis.offsetsToParse = dis.offsetsToParse[1:]
			dis.stats.dequeuedPrimary++
			return key, true, nil
		}

		for len(dis.functionReturnsToParse) > 0 {
			key := dis.functionReturnsToParse[0]
			dis.functionReturnsToParse = dis.functionReturnsToParse[1:]

			ok := dis.functionReturnsToParseAdded.Contains(key)
			// if the address was removed from the set it marks the address as not being parsed anymore,
			// this way is more efficient than iterating the slice to delete the element
			if !ok {
				continue
			}
			dis.functionReturnsToParseAdded.Remove(key)
			dis.stats.dequeuedFunctionRet++
			return key, true, nil
		}

		dis.stats.jumpEngineScanCalls++
		isEntry, err := dis.jumpEngine.ScanForNewJumpEngineEntry(dis.codeBaseAddress)
		if err != nil {
			return ParseKey{}, false, fmt.Errorf("scanning for new jump engine entry: %w", err)
		}
		if isEntry {
			dis.stats.jumpEngineEntryFound++
		}
		if !isEntry {
			return ParseKey{}, false, nil
		}
	}
}
