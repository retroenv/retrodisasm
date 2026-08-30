package chip8

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/cpu/chip8"
	"github.com/retroenv/retrogolib/assert"
)

func TestOpcode_Addressing(t *testing.T) {
	// Create a test opcode with known mask
	testOpcode := chip8.Opcode{
		Info: chip8.OpcodeInfo{
			Mask:  0xF000,
			Value: 0x1000,
		},
		Instruction: chip8.JpInst,
	}

	opcode := Opcode{op: testOpcode}
	addressing := opcode.Addressing()

	assert.Equal(t, 0xF000, addressing)
}

func TestOpcode_Instruction(t *testing.T) {
	testOpcode := chip8.Opcode{
		Instruction: chip8.JpInst,
	}

	opcode := Opcode{op: testOpcode}
	instr := opcode.Instruction()

	// Verify it returns the correct wrapped instruction
	assert.False(t, instr.IsNil())
	assert.Equal(t, chip8.JpName, instr.Name())
}

func TestOpcode_ReadsMemory(t *testing.T) {
	tests := []struct {
		name        string
		instruction *chip8.Instruction
		expected    bool
	}{
		{"nil instruction", nil, false},
		{"LD instruction", chip8.LdInst, true},
		{"DRW instruction", chip8.DrwInst, true},
		{"JP instruction", chip8.JpInst, false},
		{"CALL instruction", chip8.CallInst, false},
		{"CLS instruction", chip8.ClsInst, false},
		{"RET instruction", chip8.RetInst, false},
		{"ADD instruction", chip8.AddInst, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testOpcode := chip8.Opcode{
				Instruction: tt.instruction,
			}
			opcode := Opcode{op: testOpcode}

			result := opcode.ReadsMemory()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpcode_WritesMemory(t *testing.T) {
	tests := []struct {
		name        string
		instruction *chip8.Instruction
		expected    bool
	}{
		{"nil instruction", nil, false},
		{"LD instruction", chip8.LdInst, true},
		{"DRW instruction", chip8.DrwInst, false}, // DRW writes to display memory, not main memory
		{"JP instruction", chip8.JpInst, false},
		{"CALL instruction", chip8.CallInst, false},
		{"CLS instruction", chip8.ClsInst, false},
		{"RET instruction", chip8.RetInst, false},
		{"ADD instruction", chip8.AddInst, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testOpcode := chip8.Opcode{
				Instruction: tt.instruction,
			}
			opcode := Opcode{op: testOpcode}

			result := opcode.WritesMemory()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpcode_ReadWritesMemory(t *testing.T) {
	tests := []struct {
		name        string
		instruction *chip8.Instruction
	}{
		{"nil instruction", nil},
		{"LD instruction", chip8.LdInst},
		{"DRW instruction", chip8.DrwInst},
		{"JP instruction", chip8.JpInst},
		{"CALL instruction", chip8.CallInst},
		{"CLS instruction", chip8.ClsInst},
		{"RET instruction", chip8.RetInst},
		{"ADD instruction", chip8.AddInst},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testOpcode := chip8.Opcode{
				Instruction: tt.instruction,
			}
			opcode := Opcode{op: testOpcode}

			// CHIP-8 instructions don't both read and write memory simultaneously
			result := opcode.ReadWritesMemory()
			assert.False(t, result)
		})
	}
}

// Test with actual CHIP-8 opcode patterns
func TestOpcode_WithRealOpcodes(t *testing.T) {
	// Test with real CHIP-8 opcodes from the retrogolib
	tests := []struct {
		name     string
		nibble   int
		expected bool // whether we expect to find valid opcodes
	}{
		{"nibble 0", 0, true},  // CLS, RET, etc.
		{"nibble 1", 1, true},  // JP
		{"nibble 2", 2, true},  // CALL
		{"nibble 3", 3, true},  // SE Vx, byte
		{"nibble 4", 4, true},  // SNE Vx, byte
		{"nibble 5", 5, true},  // SE Vx, Vy
		{"nibble 6", 6, true},  // LD Vx, byte
		{"nibble 7", 7, true},  // ADD Vx, byte
		{"nibble 8", 8, true},  // Various ALU ops
		{"nibble 9", 9, true},  // SNE Vx, Vy
		{"nibble A", 10, true}, // LD I, addr
		{"nibble B", 11, true}, // JP V0, addr
		{"nibble C", 12, true}, // RND Vx, byte
		{"nibble D", 13, true}, // DRW Vx, Vy, nibble
		{"nibble E", 14, true}, // SKP Vx, SKNP Vx
		{"nibble F", 15, true}, // Timer and I/O ops
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opcodes := chip8.Opcodes[tt.nibble]
			if tt.expected {
				assert.NotEmpty(t, opcodes, "Expected opcodes for nibble %X", tt.nibble)
			}

			// Test each opcode in the nibble
			for _, op := range opcodes {
				opcode := Opcode{op: op}

				// Basic validation
				assert.NotNil(t, opcode.op.Instruction)

				// Test instruction wrapper
				instr := opcode.Instruction()
				assert.False(t, instr.IsNil())
				assert.NotEmpty(t, instr.Name())
			}
		})
	}
}
