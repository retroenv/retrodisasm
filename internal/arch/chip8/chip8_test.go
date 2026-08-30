package chip8

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/offset"
	chip8cpu "github.com/retroenv/retrogolib/arch/cpu/chip8"
	"github.com/retroenv/retrogolib/assert"
)

func TestNew(t *testing.T) {
	arch := New()

	assert.NotNil(t, arch)
}

func TestChip8_Constants(t *testing.T) {
	arch := New()

	constants, err := arch.Constants()
	assert.NoError(t, err)
	assert.Empty(t, constants)
}

func TestChip8_AddressingParam(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		param    any
		expected uint16
		valid    bool
	}{
		{"valid uint16", uint16(0x200), 0x200, true},
		{"valid int within range", 0x300, 0x300, true},
		{"invalid int too large", 0x1000, 0, false},
		{"invalid negative int", -1, 0, false},
		{"invalid string", "invalid", 0, false},
		{"boundary max valid", MaxAddress, MaxAddress, true},
		{"boundary invalid", MaxAddress + 1, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, valid := arch.AddressingParam(tt.param)
			assert.Equal(t, tt.expected, addr)
			assert.Equal(t, tt.valid, valid)
		})
	}
}

func TestChip8_HandleDisambiguousInstructions(t *testing.T) {
	arch := New()

	// CHIP-8 has no disambiguous instructions
	result := arch.HandleDisambiguousInstructions(0x200, nil)
	assert.False(t, result)
}

func TestChip8_IsAddressingIndexed(t *testing.T) {
	arch := New()

	// CHIP-8 doesn't use indexed addressing
	result := arch.IsAddressingIndexed(nil)
	assert.False(t, result)
}

func TestChip8_LastCodeAddress(t *testing.T) {
	arch := New()

	addr := arch.LastCodeAddress()
	assert.Equal(t, LastCodeAddress, addr)
}

func TestChip8_ProcessVariableUsage(t *testing.T) {
	arch := New()

	// CHIP-8 has simple addressing, no complex variable usage
	err := arch.ProcessVariableUsage(nil, "test")
	assert.NoError(t, err)
}

func TestChip8_ReadOpParam(t *testing.T) {
	arch := New()

	// CHIP-8 opcodes are self-contained, no additional parameters
	param, data, err := arch.ReadOpParam(0, 0x200)
	assert.NoError(t, err)
	assert.Nil(t, param)
	assert.Nil(t, data)
}

func TestChip8_BankWindowSize(t *testing.T) {
	arch := New()

	// CHIP-8 doesn't use banking
	size := arch.BankWindowSize(nil)
	assert.Equal(t, 0, size)
}

func TestInstruction_IsJump(t *testing.T) {
	tests := []struct {
		name     string
		instr    *chip8cpu.Instruction
		expected bool
	}{
		{"jump instruction", chip8cpu.JpInst, true},
		{"call instruction", chip8cpu.CallInst, false},
		{"load instruction", chip8cpu.LdInst, false},
		{"return instruction", chip8cpu.RetInst, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction := Instruction{ins: tt.instr}
			result := instruction.IsJump()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_IsReturn(t *testing.T) {
	tests := []struct {
		name     string
		instr    *chip8cpu.Instruction
		expected bool
	}{
		{"return instruction", chip8cpu.RetInst, true},
		{"call instruction", chip8cpu.CallInst, false},
		{"jump instruction", chip8cpu.JpInst, false},
		{"load instruction", chip8cpu.LdInst, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction := Instruction{ins: tt.instr}
			result := instruction.IsReturn()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_IsSkip(t *testing.T) {
	tests := []struct {
		name     string
		instr    *chip8cpu.Instruction
		expected bool
	}{
		{"SE instruction", chip8cpu.SeInst, true},
		{"SNE instruction", chip8cpu.SneInst, true},
		{"SKP instruction", chip8cpu.SkpInst, true},
		{"SKNP instruction", chip8cpu.SknpInst, true},
		{"jump instruction", chip8cpu.JpInst, false},
		{"call instruction", chip8cpu.CallInst, false},
		{"load instruction", chip8cpu.LdInst, false},
		{"return instruction", chip8cpu.RetInst, false},
		{"nil instruction", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction := Instruction{ins: tt.instr}
			result := instruction.IsSkip()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstruction_IsDataReference(t *testing.T) {
	tests := []struct {
		name     string
		instr    *chip8cpu.Instruction
		data     []byte
		expected bool
	}{
		{"LD I instruction", chip8cpu.LdInst, []byte{0xA2, 0x00}, true},
		{"LD V instruction", chip8cpu.LdInst, []byte{0x62, 0x00}, false},
		{"non-LD instruction", chip8cpu.JpInst, []byte{0x12, 0x00}, false},
		{"insufficient data", chip8cpu.LdInst, []byte{0xA2}, false},
		{"empty data", chip8cpu.LdInst, []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction := Instruction{ins: tt.instr}
			result := instruction.IsDataReference(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_Initialize(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	err := arch.Initialize()
	assert.NoError(t, err)

	// Verify the "Start" label was set at ProgramStart
	offsetInfo := mapper.OffsetInfo(ProgramStart)
	assert.Equal(t, "Start", offsetInfo.Label)
}

func TestChip8_ReadMemory(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// ReadMemory is a simple wrapper around mapper.ReadMemory()
	// Just verify it doesn't error
	_, err := arch.ReadMemory(0x200)
	assert.NoError(t, err)

	_, err = arch.ReadMemory(0x300)
	assert.NoError(t, err)
}

func TestChip8_ProcessOffset(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up a simple CLS instruction (0x00E0)
	dis.Memory[0x000] = 0x00
	dis.Memory[0x001] = 0xE0

	// Create a fresh offset info without preset type
	offsetInfo := &offset.DisasmOffset{}

	// Process the offset
	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.True(t, result)

	// Verify instruction was parsed
	assert.NotNil(t, offsetInfo.Opcode)
	assert.Equal(t, "cls", offsetInfo.Code)
}

func TestChip8_ProcessOffset_JumpInstruction(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up a JP instruction (0x1234 = JP $234)
	dis.Memory[0x000] = 0x12
	dis.Memory[0x001] = 0x34

	offsetInfo := &offset.DisasmOffset{}

	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.True(t, result)
	assert.Equal(t, "jp $234", offsetInfo.Code)
}

func TestChip8_ProcessOffset_CallInstruction(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up a CALL instruction (0x2300 = CALL $300)
	dis.Memory[0x000] = 0x23
	dis.Memory[0x001] = 0x00

	offsetInfo := &offset.DisasmOffset{}

	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.True(t, result)
	assert.Equal(t, "call $300", offsetInfo.Code)
}

func TestChip8_ProcessOffset_SkipInstruction(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up a SE instruction (0x3234 = SE V2, $34)
	dis.Memory[0x000] = 0x32
	dis.Memory[0x001] = 0x34

	offsetInfo := &offset.DisasmOffset{}

	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.True(t, result)
	assert.Equal(t, "se V2, $34", offsetInfo.Code)
}

func TestChip8_ProcessOffset_LoadIInstruction(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up a LD I instruction (0xA234 = LD I, $234)
	dis.Memory[0x000] = 0xA2
	dis.Memory[0x001] = 0x34

	offsetInfo := &offset.DisasmOffset{}

	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.True(t, result)
	assert.Equal(t, "ld I, $234", offsetInfo.Code)
}

func TestChip8_ProcessOffset_DataOffset(t *testing.T) {
	arch := New()
	mapper := newMockMapper()
	dis := newMockDisasm()

	// Inject dependencies
	arch.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Set up memory with invalid opcode
	dis.Memory[0x000] = 0xFF
	dis.Memory[0x001] = 0xFF

	// Create a fresh offset info without preset type
	offsetInfo := &offset.DisasmOffset{}

	// Process the offset - should return false for data
	result, err := arch.ProcessOffset(ProgramStart, offsetInfo)
	assert.NoError(t, err)
	assert.False(t, result)
}

func TestChip8_Constants_EdgeCases(t *testing.T) {
	// Test constant values are correct
	assert.Equal(t, uint16(0x200), ProgramStart)
	assert.Equal(t, uint16(0xFFF), MaxAddress)
	assert.Equal(t, uint16(0xFFF), LastCodeAddress)
}

func TestChip8_AddressingParam_EdgeCases(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		param    any
		expected uint16
		valid    bool
	}{
		{"zero address", uint16(0), 0, true},
		{"zero int", 0, 0, true},
		{"max address as uint16", uint16(MaxAddress), MaxAddress, true},
		{"program start", uint16(ProgramStart), ProgramStart, true},
		{"last code address", LastCodeAddress, LastCodeAddress, true},
		{"float type", float64(0x200), 0, false},
		{"bool type", true, 0, false},
		{"slice type", []byte{0x12}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, valid := arch.AddressingParam(tt.param)
			assert.Equal(t, tt.expected, addr)
			assert.Equal(t, tt.valid, valid)
		})
	}
}

func TestChip8_formatInstruction(t *testing.T) {
	arch := New()

	tests := []struct {
		name      string
		instrName string
		opcode    uint16
		expected  string
	}{
		{"CLS instruction", "cls", 0x00E0, ""},
		{"RET instruction", "ret", 0x00EE, ""},
		{"JP instruction", "jp", 0x1234, "$234"},
		{"JP V0 instruction", "jp", 0xB234, "V0, $234"},
		{"CALL instruction", "call", 0x2234, "$234"},
		{"SE Vx, byte", "se", 0x3234, "V2, $34"},
		{"SE Vx, Vy", "se", 0x5230, "V2, V3"},
		{"SNE Vx, byte", "sne", 0x4234, "V2, $34"},
		{"SNE Vx, Vy", "sne", 0x9230, "V2, V3"},
		{"LD Vx, byte", "ld", 0x6234, "V2, $34"},
		{"LD Vx, Vy", "ld", 0x8230, "V2, V3"},
		{"LD I, addr", "ld", 0xA234, "I, $234"},
		{"ADD Vx, byte", "add", 0x7234, "V2, $34"},
		{"ADD Vx, Vy", "add", 0x8234, "V2, V3"},
		{"OR Vx, Vy", "or", 0x8231, "V2, V3"},
		{"AND Vx, Vy", "and", 0x8232, "V2, V3"},
		{"XOR Vx, Vy", "xor", 0x8233, "V2, V3"},
		{"SUB Vx, Vy", "sub", 0x8235, "V2, V3"},
		{"SUBN Vx, Vy", "subn", 0x8237, "V2, V3"},
		{"SHR Vx", "shr", 0x8236, "V2"},
		{"SHL Vx", "shl", 0x823E, "V2"},
		{"RND Vx, byte", "rnd", 0xC234, "V2, $34"},
		{"DRW Vx, Vy, n", "drw", 0xD235, "V2, V3, $5"},
		{"SKP Vx", "skp", 0xE29E, "V2"},
		{"SKNP Vx", "sknp", 0xE2A1, "V2"},
		{"unknown instruction", "unknown", 0x0000, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := arch.formatInstruction(tt.instrName, tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_extractTargetAddressInROM(t *testing.T) {
	arch := New()

	tests := []struct {
		name          string
		data          []byte
		expectedAddr  uint16
		expectedValid bool
	}{
		// Valid ROM targets
		{"JP to program start", []byte{0x12, 0x00}, ProgramStart, true},
		{"CALL to program start", []byte{0x22, 0x00}, ProgramStart, true},
		{"LD I to program start", []byte{0xA2, 0x00}, ProgramStart, true},
		{"JP to mid-ROM", []byte{0x12, 0x34}, 0x234, true},
		{"CALL to mid-ROM", []byte{0x22, 0x34}, 0x234, true},
		{"LD I to mid-ROM", []byte{0xA2, 0x34}, 0x234, true},
		{"JP to max address", []byte{0x1F, 0xFF}, MaxAddress, true},

		// Invalid targets - interpreter memory (< ProgramStart)
		{"JP to interpreter area", []byte{0x11, 0x50}, 0, false},
		{"CALL to interpreter area", []byte{0x21, 0x00}, 0, false},
		{"LD I to font area", []byte{0xA0, 0x50}, 0, false},

		// Edge cases
		{"insufficient data", []byte{0x12}, 0, false},
		{"empty data", []byte{}, 0, false},
		{"address at boundary", []byte{0x11, 0xFF}, 0, false}, // 0x1FF < ProgramStart
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, valid := arch.extractTargetAddressInROM(tt.data)
			assert.Equal(t, tt.expectedValid, valid)
			if valid {
				assert.Equal(t, tt.expectedAddr, addr)
			}
		})
	}
}

func TestChip8_formatJumpInstruction(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		opcode   uint16
		expected string
	}{
		{"JP direct", 0x1234, "$234"},
		{"JP V0 indexed", 0xB234, "V0, $234"},
		{"non-jump opcode", 0x6234, ""}, // LD instruction
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := arch.formatJumpInstruction(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_formatCompareInstruction(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		opcode   uint16
		expected string
	}{
		{"SE Vx, byte", 0x3234, "V2, $34"},
		{"SNE Vx, byte", 0x4234, "V2, $34"},
		{"SE Vx, Vy", 0x5230, "V2, V3"},
		{"SNE Vx, Vy", 0x9230, "V2, V3"},
		{"non-compare opcode", 0x6234, ""}, // LD instruction
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := arch.formatCompareInstruction(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_formatLoadInstruction(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		opcode   uint16
		expected string
	}{
		{"LD Vx, byte", 0x6234, "V2, $34"},
		{"LD Vx, Vy", 0x8230, "V2, V3"},
		{"LD I, addr", 0xA234, "I, $234"},
		{"non-LD opcode", 0x1234, ""}, // JP instruction
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := arch.formatLoadInstruction(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_formatAddInstruction(t *testing.T) {
	arch := New()

	tests := []struct {
		name     string
		opcode   uint16
		expected string
	}{
		{"ADD Vx, byte", 0x7234, "V2, $34"},
		{"ADD Vx, Vy", 0x8234, "V2, V3"},
		{"non-ADD opcode", 0x1234, ""}, // JP instruction
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := arch.formatAddInstruction(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChip8_formatBinaryInstruction(t *testing.T) {
	arch := New()

	opcode := uint16(0x8231) // OR V2, V3
	result := arch.formatBinaryInstruction(opcode)
	assert.Equal(t, "V2, V3", result)
}

func TestChip8_formatShiftInstruction(t *testing.T) {
	arch := New()

	opcode := uint16(0x8236) // SHR V2
	result := arch.formatShiftInstruction(opcode)
	assert.Equal(t, "V2", result)
}

func TestChip8_formatRandomInstruction(t *testing.T) {
	arch := New()

	opcode := uint16(0xC234) // RND V2, $34
	result := arch.formatRandomInstruction(opcode)
	assert.Equal(t, "V2, $34", result)
}

func TestChip8_formatDrawInstruction(t *testing.T) {
	arch := New()

	opcode := uint16(0xD235) // DRW V2, V3, $5
	result := arch.formatDrawInstruction(opcode)
	assert.Equal(t, "V2, V3, $5", result)
}

func TestChip8_formatSkipInstruction(t *testing.T) {
	arch := New()

	opcode := uint16(0xE29E) // SKP V2
	result := arch.formatSkipInstruction(opcode)
	assert.Equal(t, "V2", result)
}
