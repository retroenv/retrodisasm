// Package chip8 provides CHIP-8 architecture specific disassembler implementation.
// CHIP-8 is an interpreted programming language from the 1970s designed for simple games.
// This package handles disassembly of CHIP-8 ROM files into assembly code.
package chip8

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/chip8"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// CHIP-8 memory layout constants.
//
// CHIP-8 memory map (4KB total):
//
//	0x000-0x1FF: Interpreter and font data (512 bytes)
//	0x200-0xFFF: User program space (3584 bytes)
//
// The display buffer (64×32 pixels) and stack are typically maintained
// separately from the 4KB main memory address space.
const (
	// ProgramStart is the memory address where CHIP-8 programs begin execution.
	// CHIP-8 programs are loaded at address 0x200 in the virtual machine's memory space,
	// but stored starting at offset 0x0 in ROM files.
	ProgramStart = 0x200

	// MaxAddress is the highest valid address in CHIP-8 memory space (4KB total).
	MaxAddress = 0xFFF

	// LastCodeAddress is the last valid address for user programs in CHIP-8.
	// Programs can use the full range from 0x200 to 0xFFF.
	LastCodeAddress = 0xFFF
)

// Dependencies contains the dependencies needed by Chip8.
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

// Chip8 implements the arch.Architecture interface for CHIP-8 processors.
// CHIP-8 is an interpreted programming language with 4KB of memory,
// 16 general-purpose 8-bit registers, and a simple instruction set.
type Chip8 struct {
	dis    disasm
	mapper offset.Mapper
}

// New returns a new CHIP-8 architecture configuration.
func New() *Chip8 {
	return &Chip8{}
}

// InjectDependencies sets the required dependencies for this architecture.
func (c *Chip8) InjectDependencies(deps Dependencies) {
	c.dis = deps.Disasm
	c.mapper = deps.Mapper
}

// Constants returns architecture-specific constants for CHIP-8.
// CHIP-8 doesn't have hardware constants like the NES, so returns empty map.
func (c *Chip8) Constants() (map[uint16]consts.Constant, error) {
	return map[uint16]consts.Constant{}, nil
}

// AddressingParam extracts addressing parameters from CHIP-8 instructions.
// CHIP-8 uses direct addressing with 12-bit addresses embedded in opcodes.
func (c *Chip8) AddressingParam(param any) (uint16, bool) {
	switch p := param.(type) {
	case uint16:
		return p, true
	case int:
		// Validate address range for CHIP-8 (12-bit addresses)
		if p >= 0 && p <= MaxAddress {
			return uint16(p), true
		}
	}
	return 0, false
}

// HandleDisambiguousInstructions handles instructions that could be interpreted multiple ways.
// CHIP-8 has a simple instruction set with no ambiguous opcodes.
func (c *Chip8) HandleDisambiguousInstructions(_ uint16, _ *offset.DisasmOffset) bool {
	return false
}

// Initialize sets up the disassembler for CHIP-8 ROM analysis.
// CHIP-8 programs are stored starting at ROM offset 0 but execute at memory address 0x200.
func (c *Chip8) Initialize() error {
	// Set code base address to 0x200 so labels reflect CHIP-8 memory addresses
	c.dis.SetCodeBaseAddress(ProgramStart)

	// Set "Start" label for the entry point (memory address 0x200 = ROM offset 0)
	offsetInfo := c.mapper.OffsetInfo(ProgramStart)
	offsetInfo.Label = "Start"

	// Start disassembly at CHIP-8 program start address (0x200)
	c.dis.AddAddressToParse(ProgramStart, ProgramStart, 0, nil, false)
	return nil
}

// IsAddressingIndexed determines if an opcode uses indexed addressing.
// CHIP-8 uses register-based addressing rather than indexed addressing.
func (c *Chip8) IsAddressingIndexed(_ instruction.Opcode) bool {
	return false
}

// LastCodeAddress returns the highest valid code address for CHIP-8.
// Programs can use the full range from 0x200 to 0xFFF (3584 bytes).
func (c *Chip8) LastCodeAddress() uint16 {
	return LastCodeAddress
}

// PostProcessCode performs architecture-specific post-processing after all code is disassembled.
// CHIP-8 doesn't require any post-processing.
func (c *Chip8) PostProcessCode() error {
	return nil
}

// ProcessOffset processes a CHIP-8 instruction at the given address.
// It parses the instruction, formats it for assembly output, and handles
// control flow analysis for jumps, calls, and data references.
func (c *Chip8) ProcessOffset(address uint16, offsetInfo *offset.DisasmOffset) (bool, error) {
	inspectCode, err := c.initializeOffsetInfo(offsetInfo)
	if err != nil {
		return false, err
	}
	if !inspectCode {
		return false, nil
	}

	instruction := offsetInfo.Opcode.Instruction()
	c.formatOffsetCode(offsetInfo, instruction)

	instr, ok := instruction.(Instruction)
	if !ok {
		return false, fmt.Errorf("unexpected instruction type: %T", instruction)
	}

	c.handleControlFlow(address, offsetInfo, instruction, instr)
	return true, nil
}

// FormatBranchReference formats an instruction with a branch or data destination label.
func (c *Chip8) FormatBranchReference(offsetInfo *offset.DisasmOffset, label string) string {
	opcode, ok := decodeOpcode(offsetInfo.Data)
	if !ok {
		return offset.FormatBranchReference(offsetInfo, label)
	}

	switch opcode & 0xF000 {
	case 0x1000:
		return fmt.Sprintf("%s %s", chip8.Jp.Name, label)
	case 0x2000:
		return fmt.Sprintf("%s %s", chip8.Call.Name, label)
	case 0xA000:
		return fmt.Sprintf("%s I, %s", chip8.Ld.Name, label)
	case 0xB000:
		return fmt.Sprintf("%s V0, %s", chip8.Jp.Name, label)
	default:
		return offset.FormatBranchReference(offsetInfo, label)
	}
}

// ProcessVariableUsage processes variable usage patterns in CHIP-8 instructions.
// CHIP-8 uses simple direct addressing and register operations without complex variable patterns.
func (c *Chip8) ProcessVariableUsage(_ *offset.DisasmOffset, _ string) error {
	return nil
}

// ReadOpParam reads additional operation parameters for CHIP-8 instructions.
// CHIP-8 opcodes are 2 bytes with all parameters embedded, so no additional reads needed.
func (c *Chip8) ReadOpParam(_ int, _ uint16) (any, []byte, error) {
	return nil, nil, nil
}

// BankWindowSize returns the bank window size.
// CHIP-8 doesn't use banking, return 0 for single bank mapping.
func (c *Chip8) BankWindowSize(_ *cartridge.Cartridge) int {
	return 0
}

// ReadMemory reads a byte from memory using CHIP-8-specific memory mapping.
// CHIP-8 programs use a 4KB address space starting at 0x000.
func (c *Chip8) ReadMemory(address uint16) (byte, error) {
	value := c.mapper.ReadMemory(address)
	return value, nil
}

// formatOffsetCode formats the instruction code string for display
func (c *Chip8) formatOffsetCode(offsetInfo *offset.DisasmOffset, instruction instruction.Instruction) {
	name := instruction.Name()
	opcodeBytes := offsetInfo.Data
	if len(opcodeBytes) >= 2 {
		opcode := uint16(opcodeBytes[0])<<8 | uint16(opcodeBytes[1])
		if params := c.formatInstruction(name, opcode); params != "" {
			offsetInfo.Code = fmt.Sprintf("%s %s", name, params)
			return
		}
	}
	offsetInfo.Code = name
}

// handleControlFlow processes control flow based on instruction type
func (c *Chip8) handleControlFlow(address uint16, offsetInfo *offset.DisasmOffset, instruction instruction.Instruction, instr Instruction) {
	pc := c.dis.ProgramCounter()

	switch {
	case instr.IsJump():
		if target, ok := c.extractTargetAddressInROM(offsetInfo.Data); ok {
			c.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		}

	case instruction.IsCall():
		if target, ok := c.extractTargetAddressInROM(offsetInfo.Data); ok {
			c.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		}
		nextAddr := pc + uint16(len(offsetInfo.Data))
		c.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)

	case instr.IsSkip():
		nextAddr := pc + uint16(len(offsetInfo.Data))
		skipAddr := nextAddr + 2 // CHIP-8 instructions are 2 bytes
		c.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)
		c.dis.AddAddressToParse(skipAddr, offsetInfo.Context, address, instruction, false)

	case instr.IsDataReference(offsetInfo.Data):
		c.handleDataReference(address, offsetInfo, instruction, pc)

	case !instr.IsReturn():
		nextAddr := pc + uint16(len(offsetInfo.Data))
		c.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)
	}
}

// handleDataReference processes data reference instructions
func (c *Chip8) handleDataReference(address uint16, offsetInfo *offset.DisasmOffset, instruction instruction.Instruction, pc uint16) {
	if target, ok := c.extractTargetAddressInROM(offsetInfo.Data); ok {
		c.dis.AddAddressToParse(target, offsetInfo.Context, address, instruction, true)
		targetInfo := c.mapper.OffsetInfo(target)
		targetInfo.SetType(program.DataOffset)
	}

	nextAddr := pc + uint16(len(offsetInfo.Data))
	c.dis.AddAddressToParse(nextAddr, offsetInfo.Context, address, instruction, false)
}

// formatInstruction formats a CHIP-8 instruction with its parameters.
// Returns the formatted parameter string for the given instruction.
func (c *Chip8) formatInstruction(name string, opcode uint16) string {
	switch name {
	case chip8.Cls.Name, chip8.Ret.Name:
		return "" // No parameters
	case chip8.Jp.Name:
		return c.formatJumpInstruction(opcode)
	case chip8.Call.Name:
		return fmt.Sprintf("$%03X", opcode&0x0FFF)
	case chip8.Se.Name, chip8.Sne.Name:
		return c.formatCompareInstruction(opcode)
	case chip8.Ld.Name:
		return c.formatLoadInstruction(opcode)
	case chip8.Add.Name:
		return c.formatAddInstruction(opcode)
	case chip8.Or.Name, chip8.And.Name, chip8.Xor.Name, chip8.Sub.Name, chip8.Subn.Name:
		return c.formatBinaryInstruction(opcode)
	case chip8.Shr.Name, chip8.Shl.Name:
		return c.formatShiftInstruction(opcode)
	case chip8.Rnd.Name:
		return c.formatRandomInstruction(opcode)
	case chip8.Drw.Name:
		return c.formatDrawInstruction(opcode)
	case chip8.Skp.Name, chip8.Sknp.Name:
		return c.formatSkipInstruction(opcode)
	}
	return ""
}

// formatJumpInstruction formats jump instructions (JP addr, JP V0+addr).
func (c *Chip8) formatJumpInstruction(opcode uint16) string {
	if opcode&0xF000 == 0x1000 {
		return fmt.Sprintf("$%03X", opcode&0x0FFF)
	}
	if opcode&0xF000 == 0xB000 {
		return fmt.Sprintf("V0, $%03X", opcode&0x0FFF)
	}
	return ""
}

// formatCompareInstruction formats comparison instructions (SE, SNE).
func (c *Chip8) formatCompareInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	// SE/SNE instructions:
	// 3XNN: SE Vx, byte
	// 4XNN: SNE Vx, byte
	// 5XY0: SE Vx, Vy
	// 9XY0: SNE Vx, Vy
	switch opcode & 0xF000 {
	case 0x3000, 0x4000:
		// SE/SNE Vx, byte
		return fmt.Sprintf("V%X, $%02X", x, opcode&0x00FF)
	case 0x5000, 0x9000:
		// SE/SNE Vx, Vy
		y := extractRegisterY(opcode)
		return fmt.Sprintf("V%X, V%X", x, y)
	}
	return ""
}

// formatLoadInstruction formats load instructions (LD Vx, byte/Vy/I).
func (c *Chip8) formatLoadInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	switch opcode & 0xF000 {
	case 0x6000:
		return fmt.Sprintf("V%X, $%02X", x, opcode&0x00FF)
	case 0x8000:
		y := extractRegisterY(opcode)
		return fmt.Sprintf("V%X, V%X", x, y)
	case 0xA000:
		return fmt.Sprintf("I, $%03X", opcode&0x0FFF)
	}
	return ""
}

// formatAddInstruction formats add instructions (ADD Vx, byte/Vy).
func (c *Chip8) formatAddInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	if opcode&0xF000 == 0x7000 {
		return fmt.Sprintf("V%X, $%02X", x, opcode&0x00FF)
	}
	if opcode&0xF000 == 0x8000 {
		y := extractRegisterY(opcode)
		return fmt.Sprintf("V%X, V%X", x, y)
	}
	return ""
}

// formatBinaryInstruction formats binary operation instructions (OR, AND, XOR, SUB, SUBN).
func (c *Chip8) formatBinaryInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	y := extractRegisterY(opcode)
	return fmt.Sprintf("V%X, V%X", x, y)
}

// formatShiftInstruction formats shift instructions (SHR, SHL).
func (c *Chip8) formatShiftInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	return fmt.Sprintf("V%X", x)
}

// formatRandomInstruction formats random number instructions (RND).
func (c *Chip8) formatRandomInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	return fmt.Sprintf("V%X, $%02X", x, opcode&0x00FF)
}

// formatDrawInstruction formats draw instructions (DRW).
func (c *Chip8) formatDrawInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	y := extractRegisterY(opcode)
	n := opcode & 0x000F
	return fmt.Sprintf("V%X, V%X, $%X", x, y, n)
}

// formatSkipInstruction formats skip instructions (SKP, SKNP).
func (c *Chip8) formatSkipInstruction(opcode uint16) string {
	x := extractRegisterX(opcode)
	return fmt.Sprintf("V%X", x)
}

// decodeOpcode extracts the 16-bit opcode from instruction bytes.
func decodeOpcode(data []byte) (uint16, bool) {
	if len(data) < 2 {
		return 0, false
	}
	return uint16(data[0])<<8 | uint16(data[1]), true
}

// extractRegisterX extracts the X register nibble from a CHIP-8 opcode.
func extractRegisterX(opcode uint16) uint16 {
	return (opcode & 0x0F00) >> 8
}

// extractRegisterY extracts the Y register nibble from a CHIP-8 opcode.
func extractRegisterY(opcode uint16) uint16 {
	return (opcode & 0x00F0) >> 4
}

// extractTargetAddressInROM extracts the target address from a CHIP-8 instruction
// and returns (memory address, true) if it's in ROM, or (0, false) if it targets
// interpreter memory (< $200) or is invalid.
func (c *Chip8) extractTargetAddressInROM(data []byte) (uint16, bool) {
	opcode, ok := decodeOpcode(data)
	if !ok {
		return 0, false
	}

	// Target address is in lower 12 bits for JP, CALL, and LD I instructions
	target := opcode & 0x0FFF

	// Validate target address is within CHIP-8 address space
	if target > MaxAddress || target < ProgramStart {
		return 0, false
	}

	return target, true
}
