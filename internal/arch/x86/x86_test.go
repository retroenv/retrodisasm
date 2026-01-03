package x86

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestNew(t *testing.T) {
	logger := log.NewTestLogger(t)
	converter := parameter.New(parameter.Config{})
	arch := New(logger, converter)

	assert.NotNil(t, arch)
	assert.Equal(t, converter, arch.converter)
	assert.Equal(t, logger, arch.logger)
	assert.Equal(t, DefaultCodeBase, arch.codeBase)
}

func TestX86_Constants(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	constants, err := arch.Constants()
	assert.NoError(t, err)
	assert.Empty(t, constants) // Currently returns empty, but could include DOS/BIOS constants
}

func TestX86_AddressingParam(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	tests := []struct {
		name     string
		param    any
		expected uint16
		valid    bool
	}{
		{"valid uint16", uint16(0x0100), 0x0100, true},
		{"valid int16", int16(0x0200), 0x0200, true},
		{"valid int within range", 0x0300, 0x0300, true},
		{"invalid int too large", 0x10000, 0, false},
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

func TestX86_HandleDisambiguousInstructions(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	result := arch.HandleDisambiguousInstructions(0x0100, nil)
	assert.False(t, result) // Currently no disambiguation implemented
}

func TestX86_IsAddressingIndexed(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	result := arch.IsAddressingIndexed(nil)
	assert.False(t, result)
}

func TestX86_LastCodeAddress(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	// Set PRG length for testing
	arch.SetPRGLength(1000)

	addr := arch.LastCodeAddress()
	assert.Equal(t, DefaultCodeBase+1000, addr)
}

func TestX86_SetCodeBase(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	customBase := uint16(0x0200)
	arch.SetCodeBase(customBase)

	assert.Equal(t, customBase, arch.codeBase)
}

func TestX86_BankWindowSize(t *testing.T) {
	logger := log.NewTestLogger(t)
	arch := New(logger, parameter.New(parameter.Config{}))

	size := arch.BankWindowSize(nil)
	assert.Equal(t, 0, size) // DOS doesn't use banking
}
