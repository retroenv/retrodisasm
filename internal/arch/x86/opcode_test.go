package x86

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/cpu/x86"
	"github.com/retroenv/retrogolib/assert"
)

func TestGetOpcodeInfo(t *testing.T) {
	tests := []struct {
		opcode   uint8
		wantName string
		wantOk   bool
	}{
		{0xB4, "mov", true},  // MOV AH, imm8
		{0xBA, "mov", true},  // MOV DX, imm16
		{0xCD, "int", true},  // INT imm8
		{0xC3, "ret", true},  // RET
		{0x90, "nop", true},  // NOP
		{0xE8, "call", true}, // CALL rel16
		{0xEB, "jmp", true},  // JMP rel8
		{0x74, "jz", true},   // JZ rel8
		{0x64, "", false},    // Segment override prefix - not a standalone instruction
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			opcodeInfo, ok := x86.GetOpcodeInfo(tt.opcode)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.NotNil(t, opcodeInfo.Instruction)
				assert.Equal(t, tt.wantName, opcodeInfo.Instruction.Name)
			}
		})
	}
}

func TestOpcodeWrapper(t *testing.T) {
	// Get opcode for MOV AH, imm8
	opcodeInfo, ok := x86.GetOpcodeInfo(0xB4)
	assert.True(t, ok)

	wrapper := Opcode{op: opcodeInfo}

	// Test addressing mode
	assert.Equal(t, int(x86.ImmediateAddressing), wrapper.Addressing())

	// Test instruction
	instr := wrapper.Instruction()
	assert.Equal(t, "mov", instr.Name())
	assert.False(t, instr.IsNil())
	assert.False(t, instr.IsCall())

	// Cast to Instruction type to access x86-specific methods
	x86Instr := instr.(Instruction)
	assert.False(t, x86Instr.IsJump())
	assert.False(t, x86Instr.IsReturn())
}
