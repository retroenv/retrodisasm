package chip8

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/cpu/chip8"
	"github.com/retroenv/retrogolib/assert"
)

func TestInstruction_IsCall(t *testing.T) {
	tests := []struct {
		name     string
		ins      *chip8.Instruction
		expected bool
	}{
		{"call instruction", chip8.CallInst, true},
		{"jump instruction", chip8.JpInst, false},
		{"load instruction", chip8.LdInst, false},
		{"nil instruction", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instr := Instruction{ins: tt.ins}
			result := instr.IsCall()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_IsNil(t *testing.T) {
	tests := []struct {
		name     string
		ins      *chip8.Instruction
		expected bool
	}{
		{"nil instruction", nil, true},
		{"valid instruction", chip8.JpInst, false},
		{"call instruction", chip8.CallInst, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instr := Instruction{ins: tt.ins}
			result := instr.IsNil()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_Name(t *testing.T) {
	tests := []struct {
		name     string
		ins      *chip8.Instruction
		expected string
	}{
		{"nil instruction", nil, ""},
		{"jump instruction", chip8.JpInst, chip8.JpName},
		{"call instruction", chip8.CallInst, chip8.CallName},
		{"load instruction", chip8.LdInst, chip8.LdName},
		{"clear instruction", chip8.ClsInst, chip8.ClsName},
		{"return instruction", chip8.RetInst, chip8.RetName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instr := Instruction{ins: tt.ins}
			result := instr.Name()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_Unofficial(t *testing.T) {
	tests := []struct {
		name string
		ins  *chip8.Instruction
	}{
		{"nil instruction", nil},
		{"jump instruction", chip8.JpInst},
		{"call instruction", chip8.CallInst},
		{"load instruction", chip8.LdInst},
		{"clear instruction", chip8.ClsInst},
		{"return instruction", chip8.RetInst},
		{"draw instruction", chip8.DrwInst},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instr := Instruction{ins: tt.ins}
			// CHIP-8 has no unofficial instructions
			result := instr.Unofficial()
			assert.False(t, result)
		})
	}
}

// Test instruction wrapper creation
func TestInstructionWrapper(t *testing.T) {
	// Test with various CHIP-8 instructions
	instructions := []*chip8.Instruction{
		chip8.ClsInst,
		chip8.RetInst,
		chip8.JpInst,
		chip8.CallInst,
		chip8.SeInst,
		chip8.SneInst,
		chip8.LdInst,
		chip8.AddInst,
		chip8.OrInst,
		chip8.AndInst,
		chip8.XorInst,
		chip8.SubInst,
		chip8.SubnInst,
		chip8.ShrInst,
		chip8.ShlInst,
		chip8.RndInst,
		chip8.DrwInst,
		chip8.SkpInst,
		chip8.SknpInst,
	}

	for _, ins := range instructions {
		t.Run(ins.Name, func(t *testing.T) {
			instr := Instruction{ins: ins}

			// Test basic properties
			assert.False(t, instr.IsNil())
			assert.Equal(t, ins.Name, instr.Name())
			assert.False(t, instr.Unofficial())

			// Test call detection (only CALL should return true)
			expectedIsCall := ins == chip8.CallInst
			assert.Equal(t, expectedIsCall, instr.IsCall())
		})
	}
}
