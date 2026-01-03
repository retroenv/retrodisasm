// Package chip8 provides CHIP-8 architecture support for the NES disassembler.
//
// # CHIP-8 Architecture Overview
//
// CHIP-8 is an interpreted programming language developed in the 1970s for simple games
// and applications on early microcomputers. Despite being designed for the NES disassembler,
// this package provides experimental support for CHIP-8 ROM analysis.
//
// # Memory Layout
//
// CHIP-8 systems have 4KB of memory (0x000-MaxAddress):
//   - 0x000-0x1FF: Interpreter area (not used for user programs)
//   - ProgramStart-MaxAddress: User program and data area
//
// # Instruction Set
//
// CHIP-8 has a simple instruction set with 35 opcodes:
//   - All instructions are 2 bytes (16 bits)
//   - Instructions use direct addressing with 12-bit addresses
//   - 16 general-purpose 8-bit registers (V0-VF)
//   - Special-purpose registers: I (16-bit), PC, SP
//
// # Disassembly Process
//
// The disassembler analyzes CHIP-8 ROMs through:
//  1. Instruction parsing and validation
//  2. Control flow analysis for jumps and calls
//  3. Data reference detection for sprite data
//  4. Assembly code generation
//
// # Memory Constants
//
// The package defines key memory layout constants:
//   - ProgramStart (0x200): Where CHIP-8 programs begin execution
//   - MaxAddress (0xFFF): Highest valid memory address
//   - LastCodeAddress (0xFFF): End of user program space
//
// # Usage Example
//
//	// Create CHIP-8 architecture instance
//	arch := chip8.New()
//
//	// Initialize disassembler
//	err := arch.Initialize(disassembler)
//	if err != nil {
//		return fmt.Errorf("failed to initialize CHIP-8 architecture: %w", err)
//	}
//
// # Supported Operations
//
// The package supports all standard CHIP-8 operations:
//   - Flow control: JP, CALL, RET
//   - Arithmetic: ADD, SUB, OR, AND, XOR
//   - Memory: LD (load/store operations)
//   - Graphics: DRW (draw sprites)
//   - Input: SKP, SKNP (skip on key press/release)
//   - Timers and sound operations
//
// # Limitations
//
// This is experimental support with the following limitations:
//   - No banking support (CHIP-8 uses linear addressing)
//   - Simple instruction set compared to 6502
//   - Limited to 4KB address space (0x000-MaxAddress)
//   - No complex addressing modes
package chip8
