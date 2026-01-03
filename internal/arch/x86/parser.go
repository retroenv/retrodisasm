package x86

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/x86"
)

// initializeOffsetInfo initializes the offset info by reading and parsing the instruction.
func (a *X86) initializeOffsetInfo(offsetInfo *offset.DisasmOffset) (bool, error) {
	pc := a.dis.ProgramCounter()

	// Read opcode byte
	opcodeByte, err := a.dis.ReadMemory(pc)
	if err != nil {
		return false, fmt.Errorf("reading opcode at %04X: %w", pc, err)
	}

	// Get opcode info from table
	opcodeInfo := x86.Opcodes[opcodeByte]

	// Handle undefined/unknown opcodes - treat as data and stop tracing
	if opcodeInfo.Instruction == nil || opcodeInfo.Instruction == x86.Undefined {
		offsetInfo.Data = []byte{opcodeByte}
		offsetInfo.SetType(program.DataOffset)
		return false, nil
	}

	// Start with base opcode byte
	data := []byte{opcodeByte}
	instructionSize := 1

	// Handle two-byte opcodes (0x0F prefix)
	if opcodeByte == 0x0F {
		// Read second opcode byte
		if pc+1 >= a.LastCodeAddress() {
			offsetInfo.Data = data
			offsetInfo.Opcode = Opcode{op: &opcodeInfo}
			return false, nil
		}

		secondByte, err := a.dis.ReadMemory(pc + 1)
		if err != nil {
			offsetInfo.Data = data
			offsetInfo.Opcode = Opcode{op: &opcodeInfo}
			return false, nil //nolint:nilerr // Intentionally stopping disassembly on read error
		}

		data = append(data, secondByte)
		instructionSize = 2
		// TODO: Look up two-byte opcode
	}

	// Handle ModR/M byte if needed
	if opcodeInfo.HasModRM {
		if pc+uint16(instructionSize) >= a.LastCodeAddress() {
			offsetInfo.Data = data
			offsetInfo.Opcode = Opcode{op: &opcodeInfo}
			return false, nil
		}

		modrmByte, err := a.dis.ReadMemory(pc + uint16(instructionSize))
		if err != nil {
			offsetInfo.Data = data
			offsetInfo.Opcode = Opcode{op: &opcodeInfo}
			return false, nil //nolint:nilerr // Intentionally stopping disassembly on read error
		}

		data = append(data, modrmByte)
		instructionSize++

		// Decode ModR/M for displacement
		mod := (modrmByte >> 6) & 0x03
		rm := modrmByte & 0x07

		// Add displacement bytes based on mod and r/m
		dispSize := a.getDisplacementSize(mod, rm)
		for range dispSize {
			if pc+uint16(instructionSize) >= a.LastCodeAddress() {
				break
			}
			dispByte, err := a.dis.ReadMemory(pc + uint16(instructionSize))
			if err != nil {
				break
			}
			data = append(data, dispByte)
			instructionSize++
		}
	}

	// Add immediate bytes based on opcode size
	// The size in the opcode table includes everything
	remainingBytes := int(opcodeInfo.Size) - instructionSize
	for range remainingBytes {
		if pc+uint16(instructionSize) >= a.LastCodeAddress() {
			break
		}
		immByte, err := a.dis.ReadMemory(pc + uint16(instructionSize))
		if err != nil {
			break
		}
		data = append(data, immByte)
		instructionSize++
	}

	offsetInfo.Data = data
	offsetInfo.Opcode = Opcode{op: &opcodeInfo}
	return true, nil //nolint:nilerr // Error intentionally ignored in loops above
}

// getDisplacementSize returns the displacement size based on mod and r/m fields.
func (a *X86) getDisplacementSize(mod, rm byte) int {
	switch mod {
	case 0: // No displacement (except for special case [disp16])
		if rm == 6 {
			return 2 // [disp16]
		}
		return 0
	case 1: // 8-bit displacement
		return 1
	case 2: // 16-bit displacement
		return 2
	case 3: // Register to register (no displacement)
		return 0
	}
	return 0
}
