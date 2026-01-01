package mapper

import (
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestGetMapperConfig_KnownMappers(t *testing.T) {
	// Test that known mappers return correct config types
	tests := []struct {
		name      string
		mapperNum byte
		wantType  string
	}{
		{"NROM", 0, "*mapper.nromConfig"},
		{"UxROM", 2, "*mapper.uxromConfig"},
		{"CNROM", 3, "*mapper.nromConfig"},
		{"MMC3", 4, "*mapper.mmc3Config"},
		{"AxROM", 7, "*mapper.axromConfig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetMapperConfig(tt.mapperNum)
			assert.NotNil(t, cfg)
		})
	}
}

func TestGetMapperConfig_UnknownMapper(t *testing.T) {
	// Unknown mapper should return NROM config (default)
	cfg := GetMapperConfig(255)
	assert.NotNil(t, cfg)
	assert.Equal(t, 0x8000, cfg.BankWindowSize())
	assert.True(t, cfg.IsAddressFixed(0x8000))
	assert.Nil(t, cfg.SwitchableWindows())
}

func TestNromConfig(t *testing.T) {
	cfg := &nromConfig{}

	assert.Equal(t, 0x8000, cfg.BankWindowSize())
	assert.True(t, cfg.IsAddressFixed(0x8000))
	assert.True(t, cfg.IsAddressFixed(0xFFFF))
	assert.Nil(t, cfg.SwitchableWindows())
}

func TestUxromConfig(t *testing.T) {
	cfg := &uxromConfig{}

	assert.Equal(t, 0x4000, cfg.BankWindowSize())
	assert.False(t, cfg.IsAddressFixed(0x8000))
	assert.False(t, cfg.IsAddressFixed(0xBFFF))
	assert.True(t, cfg.IsAddressFixed(0xC000))
	assert.True(t, cfg.IsAddressFixed(0xFFFF))
	assert.Equal(t, []uint16{0x8000, 0xa000}, cfg.SwitchableWindows())
}

func TestMmc3Config(t *testing.T) {
	cfg := &mmc3Config{}

	assert.Equal(t, 0x2000, cfg.BankWindowSize())
	assert.False(t, cfg.IsAddressFixed(0x8000))
	assert.False(t, cfg.IsAddressFixed(0xDFFF))
	assert.True(t, cfg.IsAddressFixed(0xE000))
	assert.True(t, cfg.IsAddressFixed(0xFFFF))
	assert.Equal(t, []uint16{0x8000, 0xa000, 0xc000}, cfg.SwitchableWindows())
}

func TestAxromConfig(t *testing.T) {
	cfg := &axromConfig{}

	assert.Equal(t, 0x8000, cfg.BankWindowSize())
	assert.False(t, cfg.IsAddressFixed(0x8000))
	assert.False(t, cfg.IsAddressFixed(0xE000))
	assert.False(t, cfg.IsAddressFixed(0xFFFF))
	assert.Equal(t, []uint16{0x8000, 0xa000, 0xc000, 0xe000}, cfg.SwitchableWindows())
}
