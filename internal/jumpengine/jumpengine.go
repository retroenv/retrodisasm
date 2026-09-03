// Package jumpengine provides jump engine detection and processing.
package jumpengine

import (
	"fmt"
	"slices"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/log"
	"github.com/retroenv/retrogolib/set"
)

const jumpEngineLastInstructionsCheck = 16

// mapper defines the minimal interface needed from the mapper
type mapper interface {
	// OffsetInfo returns the offset information for the given address.
	OffsetInfo(address uint16) *offset.DisasmOffset
}

// disasm defines the minimal interface needed from the disassembler
type disasm interface {
	// AddAddressToParse adds an address to the list to be processed.
	AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool)
	// DeleteFunctionReturnToParse deletes a function return address from the list of addresses to parse.
	DeleteFunctionReturnToParse(address uint16)
	// ReadMemory reads a byte from the memory at the given address.
	ReadMemory(address uint16) (byte, error)
	// ReadMemoryWord reads a word from the memory at the given address.
	ReadMemoryWord(address uint16) (uint16, error)
}

// Dependencies contains the dependencies needed by JumpEngine.
type Dependencies struct {
	Disasm disasm
	Mapper mapper
}

// architecture defines the minimal interface needed from arch.Architecture
type architecture interface {
	// AddressingParam returns the address of the param if it references an address.
	AddressingParam(param any) (uint16, bool)
	// LastCodeAddress returns the last possible address of code.
	LastCodeAddress() uint16
	// ReadOpParam reads the parameter of an opcode.
	ReadOpParam(addressing int, address uint16) (any, []byte, error)
}

// jumpEngineCaller stores info about a caller of a jump engine, which is followed by a list of function addresses
type jumpEngineCaller struct {
	entries           int  // count of referenced functions in the table
	terminated        bool // marks whether the end of the table has been found
	tableStartAddress uint16
}

// JumpEngine implements jump engine detection and processing.
type JumpEngine struct {
	arch   architecture
	dis    disasm
	mapper mapper
	logger *log.Logger

	jumpEngines            set.Set[uint16]     // set of all jump engine functions addresses
	jumpEngineCallers      []*jumpEngineCaller // jump engine caller tables to process
	jumpEngineCallersAdded map[uint16]*jumpEngineCaller
}

func New(logger *log.Logger, ar architecture) *JumpEngine {
	return &JumpEngine{
		arch:                   ar,
		logger:                 logger,
		jumpEngines:            set.New[uint16](),
		jumpEngineCallers:      []*jumpEngineCaller{},
		jumpEngineCallersAdded: map[uint16]*jumpEngineCaller{},
	}
}

// InjectDependencies sets the required dependencies for this jump engine.
func (j *JumpEngine) InjectDependencies(deps Dependencies) {
	j.dis = deps.Disasm
	j.mapper = deps.Mapper
}

// AddJumpEngine adds a jump engine function address to the list of jump engines.
func (j *JumpEngine) AddJumpEngine(address uint16) {
	j.jumpEngines.Add(address)
}

// GetFunctionTableReference detects a jump engine function context and its function table.
// TODO use jump address as key to be able to handle large function
// contexts containing multiple jump engines
func (j *JumpEngine) GetFunctionTableReference(context uint16, dataReferences []uint16) {
	// if there are multiple data references just look at the last 2
	if len(dataReferences) > 2 {
		dataReferences = dataReferences[len(dataReferences)-2:]
	}

	// get the lowest data pointer, jmp and rts based jump engines have the address accesses reversed
	var referenceDistance, smallestReference uint16
	if dataReferences[0] < dataReferences[1] {
		referenceDistance = dataReferences[1] - dataReferences[0]
		smallestReference = dataReferences[0]
	} else {
		referenceDistance = dataReferences[0] - dataReferences[1]
		smallestReference = dataReferences[1]
	}

	// the access to the function tables can be done using the same address and an incremented x register
	// or to an incremented address and the same x register
	if referenceDistance != 0 && referenceDistance != 1 {
		return
	}

	jumpEngine := &jumpEngineCaller{}
	j.jumpEngineCallersAdded[context] = jumpEngine
	j.jumpEngineCallers = append(j.jumpEngineCallers, jumpEngine)

	j.jumpEngineCallersAdded[context].tableStartAddress = smallestReference
}

// GetContextDataReferences parse all instructions of the function context until the jump
// and returns data references that could point to the function table.
func (j *JumpEngine) GetContextDataReferences(offsets []*offset.DisasmOffset,
	addresses []uint16, codeBaseAddress uint16) ([]uint16, error) {

	var dataReferences []uint16

	for i, offsetInfoInstruction := range offsets {
		address := addresses[i]
		opcode := offsetInfoInstruction.Opcode

		// look for an instructions that loads data from an address in the code or data
		// address range. this should be the table containing the function addresses.
		if opcode == nil || opcode.Instruction().IsNil() || !opcode.ReadsMemory() {
			continue
		}

		param, _, err := j.arch.ReadOpParam(opcode.Addressing(), address)
		if err != nil {
			return nil, fmt.Errorf("reading opcode parameters: %w", err)
		}

		reference, ok := j.arch.AddressingParam(param)
		if ok && reference >= codeBaseAddress && reference < j.arch.LastCodeAddress() {
			dataReferences = append(dataReferences, reference)
		}
	}
	return dataReferences, nil
}

// JumpContextInfo builds the list of instructions of the current function context.
// in some ROMs the jump engine can be part of a label inside a larger function,
// the jump engine detection will use the last instructions before the jmp.
func (j *JumpEngine) JumpContextInfo(jumpAddress uint16, offsetInfo *offset.DisasmOffset) ([]*offset.DisasmOffset, []uint16) {
	var offsets []*offset.DisasmOffset
	var addresses []uint16

	for address := offsetInfo.Context; address != 0 && address < jumpAddress; {
		offsetInfoInstruction := j.mapper.OffsetInfo(address)

		// skip offsets that have not been processed yet
		if len(offsetInfoInstruction.Data) == 0 {
			address++
			continue
		}

		offsets = append(offsets, offsetInfoInstruction)
		addresses = append(addresses, address)

		address += uint16(len(offsetInfoInstruction.Data))
	}

	if len(offsets) > jumpEngineLastInstructionsCheck {
		offsets = offsets[len(offsets)-jumpEngineLastInstructionsCheck-1:]
		addresses = addresses[len(addresses)-jumpEngineLastInstructionsCheck-1:]
	}

	return offsets, addresses
}

// HandleJumpEngineDestination processes a newly detected jump engine destination.
func (j *JumpEngine) HandleJumpEngineDestination(caller, destination, codeBaseAddress uint16) error {
	if j.jumpEngines.Contains(destination) {
		return j.handleJumpEngineCaller(caller, codeBaseAddress)
	}

	return nil
}

// HandleJumpEngineCallers processes all callers of a newly detected jump engine function.
func (j *JumpEngine) HandleJumpEngineCallers(context, codeBaseAddress uint16) error {
	offsetInfo := j.mapper.OffsetInfo(context)
	offsetInfo.LabelComment = "jump engine detected"
	offsetInfo.SetType(program.JumpEngine)

	for _, ref := range offsetInfo.BranchFrom {
		if err := j.handleJumpEngineCaller(ref.Address, codeBaseAddress); err != nil {
			return err
		}
	}
	return nil
}

// ScanForNewJumpEngineEntry scans all jump engine calls for an unprocessed entry in the function address table that
// follows the call. It returns whether a new address to parse was added.
func (j *JumpEngine) ScanForNewJumpEngineEntry(codeBaseAddress uint16) (bool, error) {
	for len(j.jumpEngineCallers) != 0 {
		// Remove all terminated entries
		j.jumpEngineCallers = slices.DeleteFunc(j.jumpEngineCallers, func(ec *jumpEngineCaller) bool {
			return ec.terminated
		})

		// Find the jump engine table with the smallest number of processed entries.
		// This conservative approach avoids interpreting code in the table area as function references.
		minEntries := -1
		for _, engineCaller := range j.jumpEngineCallers {
			if i := engineCaller.entries; i < minEntries || minEntries == -1 {
				minEntries = i
			}
		}
		if minEntries == -1 {
			return false, nil
		}

		for i := 0; i < len(j.jumpEngineCallers); i++ {
			engineCaller := j.jumpEngineCallers[i]
			if engineCaller.entries != minEntries {
				continue
			}

			// calculate next address in table to process
			address := engineCaller.tableStartAddress + uint16(2*engineCaller.entries)
			isEntry, err := j.processJumpEngineEntry(address, engineCaller, codeBaseAddress)
			if err != nil {
				return false, err
			}
			if isEntry {
				return true, nil
			}
			j.logger.Debug("Jump engine table",
				log.Hex("address", engineCaller.tableStartAddress),
				log.Int("entries", engineCaller.entries),
			)

			// jump engine table is processed, remove it from list to process
			j.jumpEngineCallers = slices.Delete(j.jumpEngineCallers, i, i+1)
			i--
		}
	}
	return false, nil
}

// handleJumpEngineCaller processes a newly detected jump engine caller, the return address of the call is
// marked as function reference instead of code. The first entry of the function table is processed.
func (j *JumpEngine) handleJumpEngineCaller(caller, codeBaseAddress uint16) error {
	jumpEngine, ok := j.jumpEngineCallersAdded[caller]
	if !ok {
		jumpEngine = &jumpEngineCaller{}
		j.jumpEngineCallersAdded[caller] = jumpEngine
		j.jumpEngineCallers = append(j.jumpEngineCallers, jumpEngine)
	}

	// get the address of the function pointers after the jump engine call
	offsetInfo := j.mapper.OffsetInfo(caller)
	address := caller + uint16(len(offsetInfo.Data))

	// remove from code that should be parsed
	j.dis.DeleteFunctionReturnToParse(address)
	jumpEngine.tableStartAddress = address

	_, err := j.processJumpEngineEntry(address, jumpEngine, codeBaseAddress)
	return err
}

// processJumpEngineEntry processes a potential function reference in a jump engine table.
// It returns whether the entry was a valid function reference address and has been added for processing.
func (j *JumpEngine) processJumpEngineEntry(address uint16, jumpEngine *jumpEngineCaller, codeBaseAddress uint16) (bool, error) {
	if jumpEngine.terminated {
		return false, nil
	}

	// verify that the destination is in valid code address range
	destination, err := j.dis.ReadMemoryWord(address)
	if err != nil {
		return false, fmt.Errorf("reading memory word: %w", err)
	}
	if destination < codeBaseAddress || destination >= j.arch.LastCodeAddress() {
		jumpEngine.terminated = true
		return false, nil
	}

	offsetInfo1 := j.mapper.OffsetInfo(address)
	offsetInfo2 := j.mapper.OffsetInfo(address + 1)

	// if the potential jump table entry is already marked as code, the table end is reached
	if offsetInfo1.IsType(program.CodeOffset|program.CodeAsData) ||
		offsetInfo2.IsType(program.CodeOffset|program.CodeAsData) {

		jumpEngine.terminated = true
		return false, nil
	}

	if jumpEngine.entries == 0 {
		offsetInfo1.SetType(program.JumpTable)
	}

	offsetInfo1.SetType(program.FunctionReference)
	offsetInfo2.SetType(program.FunctionReference)

	b1, err := j.dis.ReadMemory(address)
	if err != nil {
		return false, fmt.Errorf("reading memory: %w", err)
	}
	b2, err := j.dis.ReadMemory(address + 1)
	if err != nil {
		return false, fmt.Errorf("reading memory: %w", err)
	}

	offsetInfo1.Data = []byte{b1, b2}
	offsetInfo2.Data = nil

	jumpEngine.entries++

	j.dis.AddAddressToParse(destination, destination, address, nil, true)
	return true, nil
}
