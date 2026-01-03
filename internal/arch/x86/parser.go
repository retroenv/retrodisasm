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

	// Parse instruction bytes
	data, ok := a.parseInstructionBytes(pc, opcodeByte, &opcodeInfo)
	if !ok {
		offsetInfo.Data = data
		offsetInfo.Opcode = Opcode{op: &opcodeInfo}
		return false, nil
	}

	offsetInfo.Data = data
	offsetInfo.Opcode = Opcode{op: &opcodeInfo}
	return true, nil
}

// parseInstructionBytes reads all bytes for an instruction.
func (a *X86) parseInstructionBytes(pc uint16, opcodeByte byte, opcodeInfo *x86.Opcode) ([]byte, bool) {
	data := []byte{opcodeByte}
	instructionSize := 1

	// Handle two-byte opcodes (0x0F prefix)
	if opcodeByte == 0x0F {
		secondByte, ok := a.readByte(pc + 1)
		if !ok {
			return data, false
		}
		data = append(data, secondByte)
		instructionSize = 2
		// TODO: Look up two-byte opcode
	}

	// Handle ModR/M byte if needed
	if opcodeInfo.HasModRM {
		data, instructionSize = a.readModRMBytes(pc, data, instructionSize)
	}

	// Add immediate bytes based on opcode size
	data = a.readImmediateBytes(pc, data, instructionSize, int(opcodeInfo.Size))

	return data, true
}

// readByte reads a single byte at the given address.
func (a *X86) readByte(addr uint16) (byte, bool) {
	if addr >= a.LastCodeAddress() {
		return 0, false
	}
	b, err := a.dis.ReadMemory(addr)
	if err != nil {
		return 0, false
	}
	return b, true
}

// readModRMBytes reads the ModR/M byte and any displacement bytes.
func (a *X86) readModRMBytes(pc uint16, data []byte, instructionSize int) ([]byte, int) {
	modrmByte, ok := a.readByte(pc + uint16(instructionSize))
	if !ok {
		return data, instructionSize
	}

	data = append(data, modrmByte)
	instructionSize++

	// Decode ModR/M for displacement
	mod := (modrmByte >> 6) & 0x03
	rm := modrmByte & 0x07

	// Add displacement bytes based on mod and r/m
	dispSize := a.getDisplacementSize(mod, rm)
	for range dispSize {
		dispByte, ok := a.readByte(pc + uint16(instructionSize))
		if !ok {
			break
		}
		data = append(data, dispByte)
		instructionSize++
	}

	return data, instructionSize
}

// readImmediateBytes reads immediate value bytes.
func (a *X86) readImmediateBytes(pc uint16, data []byte, instructionSize, totalSize int) []byte {
	remainingBytes := totalSize - instructionSize
	for range remainingBytes {
		immByte, ok := a.readByte(pc + uint16(instructionSize))
		if !ok {
			break
		}
		data = append(data, immByte)
		instructionSize++
	}
	return data
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
