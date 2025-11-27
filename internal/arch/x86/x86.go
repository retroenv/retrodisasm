// Package x86 provides x86-16 architecture specific disassembler implementation.
// This package handles disassembly of DOS .com files into NASM-compatible assembly code.
package x86

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/log"
)

// DOS .com file memory layout constants.
//
// DOS .com files are loaded at CS:0100h (after the 256-byte PSP).
// Maximum file size is 65,280 bytes (64KB - 256 bytes for PSP).
const (
	// ProgramStart is the memory address where DOS .com programs begin execution.
	// Programs are loaded at offset 0x100 in the code segment, after the PSP.
	ProgramStart = 0x0100

	// MaxAddress is the highest valid address in a .com file (64KB segment limit).
	MaxAddress = 0xFFFF

	// LastCodeAddress is the last valid address for code in a .com file.
	LastCodeAddress = 0xFFFF
)

// Dependencies contains the dependencies needed by ArchX86.
type Dependencies struct {
	Disasm disasm
	Mapper offset.Mapper
}

// disasm defines the minimal interface needed from the disassembler.
type disasm interface {
	// AddAddressToParse adds an address to the list to be processed.
	AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool)
	// ProgramCounter returns the current program counter of the execution tracer.
	ProgramCounter() uint16
	// ReadMemory reads a byte from the memory at the given address.
	ReadMemory(address uint16) (byte, error)
	// SetCodeBaseAddress sets the code base address.
	SetCodeBaseAddress(address uint16)
}

// New returns a new x86-16 architecture configuration.
func New(logger *log.Logger) *ArchX86 {
	return &ArchX86{
		logger: logger,
	}
}

// ArchX86 implements the arch.Architecture interface for x86-16 processors.
// This is used for disassembling DOS .com files.
type ArchX86 struct {
	logger *log.Logger
	dis    disasm
	mapper offset.Mapper
}

// InjectDependencies sets the required dependencies for this architecture.
func (ar *ArchX86) InjectDependencies(deps Dependencies) {
	ar.dis = deps.Disasm
	ar.mapper = deps.Mapper
}

// Constants returns architecture-specific constants for x86.
// For DOS, this includes common interrupt addresses and BIOS data areas.
func (ar *ArchX86) Constants() (map[uint16]consts.Constant, error) {
	// DOS .com files don't have fixed hardware addresses like NES.
	// We could add BIOS data area constants here if needed.
	return map[uint16]consts.Constant{}, nil
}

// AddressingParam extracts addressing parameters from x86 instructions.
// Returns the address and true if the parameter represents an addressable location.
func (ar *ArchX86) AddressingParam(param any) (uint16, bool) {
	switch p := param.(type) {
	case uint16:
		return p, true
	case int:
		if p >= 0 && p <= MaxAddress {
			return uint16(p), true
		}
	case int16:
		// Relative addresses
		return uint16(p), true
	}
	return 0, false
}

// HandleDisambiguousInstructions handles instructions that could be interpreted multiple ways.
// x86 has some ambiguous opcodes that need special handling.
func (ar *ArchX86) HandleDisambiguousInstructions(_ uint16, _ *offset.DisasmOffset) bool {
	return false
}

// Initialize sets up the disassembler for DOS .com file analysis.
// Programs are stored starting at file offset 0 but execute at memory address 0x100.
func (ar *ArchX86) Initialize() error {
	// Set code base address to 0x100 so labels reflect DOS memory addresses
	ar.dis.SetCodeBaseAddress(ProgramStart)

	// Set "Start" label for the entry point (memory address 0x100 = file offset 0)
	offsetInfo := ar.mapper.OffsetInfo(ProgramStart)
	offsetInfo.Label = "Start"

	// Start disassembly at DOS program start address (0x100)
	ar.dis.AddAddressToParse(ProgramStart, ProgramStart, 0, nil, false)
	return nil
}

// IsAddressingIndexed determines if an opcode uses indexed addressing.
// x86 uses various indexed addressing modes through ModR/M byte.
func (ar *ArchX86) IsAddressingIndexed(opcode instruction.Opcode) bool {
	addressing := x86.AddressingMode(opcode.Addressing())
	switch addressing {
	case x86.IndexedAddressing, x86.BasedIndexedAddressing:
		return true
	}
	return false
}

// LastCodeAddress returns the highest valid code address for DOS .com files.
// Programs can use the full 64KB segment.
func (ar *ArchX86) LastCodeAddress() uint16 {
	return LastCodeAddress
}

// ProcessOffset processes an x86 instruction at the given address.
// It parses the instruction, formats it for assembly output, and handles
// control flow analysis for jumps, calls, and data references.
func (ar *ArchX86) ProcessOffset(address uint16, offsetInfo *offset.DisasmOffset) (bool, error) {
	inspectCode, err := ar.initializeOffsetInfo(offsetInfo)
	if err != nil {
		return false, err
	}
	if !inspectCode {
		return false, nil
	}

	instruction := offsetInfo.Opcode.Instruction()
	ar.formatOffsetCode(offsetInfo, instruction)

	instr, ok := instruction.(Instruction)
	if !ok {
		return false, fmt.Errorf("unexpected instruction type: %T", instruction)
	}

	ar.handleControlFlow(address, offsetInfo, instruction, instr)
	return true, nil
}

// ProcessVariableUsage processes variable usage patterns in x86 instructions.
// x86 uses complex addressing modes that may reference memory variables.
func (ar *ArchX86) ProcessVariableUsage(_ *offset.DisasmOffset, _ string) error {
	return nil
}

// ReadOpParam reads additional operation parameters for x86 instructions.
// x86 has variable-length instructions with ModR/M bytes and immediates.
func (ar *ArchX86) ReadOpParam(addressing int, address uint16) (any, []byte, error) {
	mode := x86.AddressingMode(addressing)

	switch mode {
	case x86.ImpliedAddressing, x86.StringAddressing:
		return nil, nil, nil

	case x86.ImmediateAddressing:
		// Read immediate byte
		b, err := ar.dis.ReadMemory(address)
		if err != nil {
			return nil, nil, fmt.Errorf("reading immediate byte at %04X: %w", address, err)
		}
		return int(b), []byte{b}, nil

	case x86.RelativeAddressing:
		// Read relative offset (signed byte)
		b, err := ar.dis.ReadMemory(address)
		if err != nil {
			return nil, nil, fmt.Errorf("reading relative offset at %04X: %w", address, err)
		}
		return int8(b), []byte{b}, nil

	case x86.DirectAddressing:
		// Read 16-bit direct address
		lo, err := ar.dis.ReadMemory(address)
		if err != nil {
			return nil, nil, fmt.Errorf("reading direct address low byte at %04X: %w", address, err)
		}
		hi, err := ar.dis.ReadMemory(address + 1)
		if err != nil {
			return nil, nil, fmt.Errorf("reading direct address high byte at %04X: %w", address+1, err)
		}
		addr := uint16(hi)<<8 | uint16(lo)
		return addr, []byte{lo, hi}, nil
	}

	return nil, nil, nil
}

// BankWindowSize returns the bank window size.
// DOS .com files don't use banking, return 0 for single bank mapping.
func (ar *ArchX86) BankWindowSize(_ *cartridge.Cartridge) int {
	return 0
}

// ReadMemory reads a byte from memory using x86-specific memory mapping.
// DOS .com files use a flat 64KB address space.
func (ar *ArchX86) ReadMemory(address uint16) (byte, error) {
	value := ar.mapper.ReadMemory(address)
	return value, nil
}
