package m6502

import (
	"errors"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
)

var errInstructionOverlapsIRQHandlers = errors.New("instruction overlaps IRQ handler start")

// initializeOffsetInfo initializes the offset info and returns
// whether the offset should process inspection for code parameters.
func (ar *Arch6502) initializeOffsetInfo(offsetInfo *offset.DisasmOffset) (bool, error) {
	if offsetInfo.IsType(program.CodeOffset) {
		return false, nil // was set by CDL
	}
	// Jump-engine table entries own both pointer bytes. A branch into the
	// switchable window may target a different physical bank at runtime.
	if offsetInfo.IsType(program.FunctionReference) {
		return false, nil
	}

	pc := ar.dis.ProgramCounter()
	b, err := ar.dis.ReadMemory(pc)
	if err != nil {
		return false, fmt.Errorf("reading memory at address %04x: %w", pc, err)
	}
	offsetInfo.Data = make([]byte, 1, cpu6502.MaxOpcodeSize)
	offsetInfo.Data[0] = b

	if offsetInfo.IsType(program.DataOffset) {
		return false, nil // was set by CDL
	}

	opcode := cpu6502.Opcodes[b]
	if opcode.Instruction == nil {
		// consider an unknown instruction as start of data
		offsetInfo.SetType(program.DataOffset)
		return false, nil
	}

	// Stop at unofficial opcodes: if this option is enabled and this is an
	// unofficial instruction that we didn't explicitly branch to, treat it
	// as data instead of code and stop tracing.
	opts := ar.dis.Options()
	if opts.StopAtUnofficial && isUnofficialInstruction(opcode.Instruction) && !ar.dis.IsBranchDestination(pc) {
		offsetInfo.SetType(program.DataOffset)
		return false, nil
	}

	op := &Opcode{
		op: opcode,
	}
	offsetInfo.Opcode = op
	return true, nil
}

// processParamInstruction processes an instruction with parameters.
// Special handling is required as this instruction could branch to a different location.
func (ar *Arch6502) processParamInstruction(address uint16, offsetInfo *offset.DisasmOffset) (string, error) {
	opcode := offsetInfo.Opcode
	pc := ar.dis.ProgramCounter()
	param, opcodes, err := ar.ReadOpParam(opcode.Addressing(), pc)
	if err != nil {
		return "", fmt.Errorf("reading opcode parameters: %w", err)
	}
	offsetInfo.Data = append(offsetInfo.Data, opcodes...)

	if address+uint16(len(offsetInfo.Data)) > cpu6502.InterruptVectorStartAddress {
		return "", errInstructionOverlapsIRQHandlers
	}

	paramAsString, err := parameter.String(ar.converter, cpu6502.AddressingMode(opcode.Addressing()), param)
	if err != nil {
		return "", fmt.Errorf("getting parameter as string: %w", err)
	}

	paramAsString = ar.replaceParamByAlias(address, opcode, param, paramAsString)

	if _, ok := cpu6502.BranchingInstructions[opcode.Instruction().Name()]; ok {
		addr, ok := param.(cpu6502.Absolute)
		if ok {
			ar.dis.AddAddressToParse(uint16(addr), offsetInfo.Context, pc, opcode.Instruction(), true)
		}
	}

	return paramAsString, nil
}

// handleInstructionIRQOverlap handles an instruction overlapping with the start of the IRQ handlers.
// The opcodes are cut until the start of the IRQ handlers and the offset is converted to type data.
func (ar *Arch6502) handleInstructionIRQOverlap(address uint16, offsetInfo *offset.DisasmOffset) {
	if address > cpu6502.InterruptVectorStartAddress {
		return
	}

	keepLength := int(cpu6502.InterruptVectorStartAddress - address)
	offsetInfo.Data = offsetInfo.Data[:keepLength]

	for i := range keepLength {
		offsetInfo = ar.mapper.OffsetInfo(address + uint16(i))
		offsetInfo.ClearType(program.CodeOffset)
		offsetInfo.SetType(program.CodeAsData | program.DataOffset)
	}
}

// replaceParamByAlias replaces the absolute address with an alias name if it can match it to
// a constant, zero page variable or a code reference.
func (ar *Arch6502) replaceParamByAlias(address uint16, opcode instruction.Opcode, param any, paramAsString string) string {
	forceVariableUsage := false
	addressReference, addressValid := ar.AddressingParam(param)
	if !addressValid || addressReference >= cpu6502.InterruptVectorStartAddress {
		return paramAsString
	}

	if _, ok := cpu6502.BranchingInstructions[opcode.Instruction().Name()]; ok {
		var handleParam bool
		handleParam, forceVariableUsage = checkBranchingParam(addressReference, opcode)
		if !handleParam {
			return paramAsString
		}
	}

	changedParamAsString, ok := ar.consts.ReplaceParameter(addressReference, opcode, paramAsString)
	if ok {
		return changedParamAsString
	}

	ar.vars.AddReference(addressReference, address, opcode, forceVariableUsage)

	// Mark PRG ROM addresses referenced by indexed loads as data tables.
	// This prevents the execution flow tracer from misinterpreting table
	// contents as code when it falls through from preceding code.
	if ar.IsAddressingIndexed(opcode) && addressReference >= ar.codeBaseAddress {
		ar.markIndexedReferenceAsData(addressReference)
	}

	return paramAsString
}

// markIndexedReferenceAsData marks the byte at the given PRG ROM address as
// a data offset, preventing the execution flow tracer from decoding it as code.
// Only marks bytes that have not already been classified as code.
func (ar *Arch6502) markIndexedReferenceAsData(address uint16) {
	offsetInfo := ar.mapper.OffsetInfo(address)
	if offsetInfo == nil || offsetInfo.IsType(program.CodeOffset) {
		return
	}
	offsetInfo.SetType(program.DataOffset)
	if len(offsetInfo.Data) == 0 && !ar.previousOffsetOwnsByte(address) {
		b := ar.mapper.ReadMemory(address)
		offsetInfo.Data = []byte{b}
	}
}

func (ar *Arch6502) previousOffsetOwnsByte(address uint16) bool {
	for distance := uint16(1); distance < cpu6502.MaxOpcodeSize; distance++ {
		previous := ar.mapper.OffsetInfo(address - distance)
		if previous != nil && len(previous.Data) > int(distance) {
			return true
		}
	}
	return false
}

// checkBranchingParam checks whether the branching instruction should do a variable check for the parameter
// and forces variable usage.
func checkBranchingParam(address uint16, opcode instruction.Opcode) (bool, bool) {
	name := opcode.Instruction().Name()
	addressing := cpu6502.AddressingMode(opcode.Addressing())

	switch {
	case name == cpu6502.JmpName && addressing == cpu6502.IndirectAddressing:
		return true, false
	case name == cpu6502.JmpName || name == cpu6502.JsrName:
		if addressing == cpu6502.AbsoluteAddressing && address < nes.CodeBaseAddress {
			return true, true
		}
	}
	return false, false
}
