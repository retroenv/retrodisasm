// Package x86 provides x86 (8086/8088) architecture specific disassembler implementation for DOS .com files.
package x86

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// DOS .com file constants.
const (
	// DefaultCodeBase is the default base address for DOS .com files (0x0100).
	// DOS .com files are loaded at CS:0x0100 with PSP (Program Segment Prefix) at CS:0x0000.
	DefaultCodeBase = 0x0100

	// MaxAddress is the maximum address in a 64KB segment.
	MaxAddress = 0xFFFF
)

// Dependencies contains the dependencies needed by X86.
type Dependencies struct {
	Disasm disasm
	Mapper offset.Mapper
}

// disasm defines the minimal interface needed from the disassembler.
type disasm interface {
	AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool)
	ProgramCounter() uint16
	ReadMemory(address uint16) (byte, error)
	SetCodeBaseAddress(address uint16)
}

// New returns a new x86 architecture configuration.
func New() *X86 {
	return &X86{
		codeBase: DefaultCodeBase,
	}
}

// X86 implements the arch.Architecture interface for x86 (8086/8088) processors.
type X86 struct {
	dis       disasm
	mapper    offset.Mapper
	codeBase  uint16 // Base address for code (default 0x0100)
	prgLength int    // Length of PRG data for bounds checking
}

// InjectDependencies sets the required dependencies for this architecture.
func (a *X86) InjectDependencies(deps Dependencies) {
	a.dis = deps.Disasm
	a.mapper = deps.Mapper
}

// Constants returns architecture-specific constants for x86/DOS.
// This could include BIOS/DOS interrupt vectors and common addresses.
func (a *X86) Constants() (map[uint16]consts.Constant, error) {
	// TODO: Add DOS/BIOS constants like INT vectors
	return map[uint16]consts.Constant{}, nil
}

// AddressingParam extracts addressing parameters from x86 instructions.
// For x86, this handles absolute addresses, relative branches, and far pointers.
func (a *X86) AddressingParam(param any) (uint16, bool) {
	switch p := param.(type) {
	case uint16:
		return p, true
	case int16:
		return uint16(p), true
	case int:
		if p >= 0 && p <= MaxAddress {
			return uint16(p), true
		}
	}
	return 0, false
}

// HandleDisambiguousInstructions handles instructions that could be interpreted multiple ways.
// x86 has some ambiguous opcodes that need special handling.
func (a *X86) HandleDisambiguousInstructions(_ uint16, _ *offset.DisasmOffset) bool {
	// TODO: Handle ambiguous x86 instructions if needed
	return false
}

// Initialize sets up the disassembler for x86/DOS .com file analysis.
func (a *X86) Initialize() error {
	// Set code base address (default 0x0100 or from options)
	a.dis.SetCodeBaseAddress(a.codeBase)

	// Set "Start" label for the entry point
	offsetInfo := a.mapper.OffsetInfo(a.codeBase)
	offsetInfo.Label = "Start"

	// Start disassembly at code base address
	a.dis.AddAddressToParse(a.codeBase, a.codeBase, 0, nil, false)
	return nil
}

// IsAddressingIndexed determines if an opcode uses indexed addressing.
// x86 uses ModR/M byte for various addressing modes.
func (a *X86) IsAddressingIndexed(_ instruction.Opcode) bool {
	return false
}

// LastCodeAddress returns the highest valid code address.
// For DOS .com files, this is base + PRG length.
func (a *X86) LastCodeAddress() uint16 {
	return a.codeBase + uint16(a.prgLength)
}

// PostProcessCode performs architecture-specific post-processing after all code is disassembled.
func (a *X86) PostProcessCode() error {
	return nil
}

// ProcessOffset processes an x86 instruction at the given address.
func (a *X86) ProcessOffset(address uint16, offsetInfo *offset.DisasmOffset) (bool, error) {
	inspectCode, err := a.initializeOffsetInfo(offsetInfo)
	if err != nil {
		return false, err
	}
	if !inspectCode {
		return false, nil
	}

	instruction := offsetInfo.Opcode.Instruction()
	a.formatOffsetCode(offsetInfo, instruction)

	instr, ok := instruction.(Instruction)
	if !ok {
		return false, fmt.Errorf("unexpected instruction type: %T", instruction)
	}

	a.handleControlFlow(address, offsetInfo, instruction, instr)
	return true, nil
}

// formatOffsetCode formats the instruction code string for display.
func (a *X86) formatOffsetCode(offsetInfo *offset.DisasmOffset, instruction instruction.Instruction) {
	name := instruction.Name()

	// Get the opcode wrapper to access x86-specific info
	opcodeWrapper, ok := offsetInfo.Opcode.(Opcode)
	if !ok {
		offsetInfo.Code = name
		return
	}

	operands := a.formatOperands(opcodeWrapper, offsetInfo.Data)
	if operands != "" {
		offsetInfo.Code = fmt.Sprintf("%s %s", name, operands)
	} else {
		offsetInfo.Code = name
	}
}

// ProcessVariableUsage processes variable usage patterns in x86 instructions.
func (a *X86) ProcessVariableUsage(_ *offset.DisasmOffset, _ string) error {
	return nil
}

// ReadOpParam reads additional operation parameters for x86 instructions.
// This handles ModR/M byte, displacement, and immediate values.
func (a *X86) ReadOpParam(_ int, _ uint16) (any, []byte, error) {
	// TODO: Implement ModR/M and parameter reading
	return nil, nil, nil
}

// BankWindowSize returns the bank window size.
// DOS .com files don't use banking, return 0.
func (a *X86) BankWindowSize(_ *cartridge.Cartridge) int {
	return 0
}

// ReadMemory reads a byte from memory using x86-specific memory mapping.
func (a *X86) ReadMemory(address uint16) (byte, error) {
	value := a.mapper.ReadMemory(address)
	return value, nil
}

// SetCodeBase sets the code base address (for -base flag support).
func (a *X86) SetCodeBase(base uint16) {
	a.codeBase = base
}

// SetPRGLength sets the PRG length for bounds checking.
func (a *X86) SetPRGLength(length int) {
	a.prgLength = length
}

// SetOptions sets architecture options from disasm options.
func (a *X86) SetOptions(cart *cartridge.Cartridge, baseAddress uint16) {
	a.codeBase = baseAddress
	if cart != nil && cart.PRG != nil {
		a.prgLength = len(cart.PRG)
	}
}
