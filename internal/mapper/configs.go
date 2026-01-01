package mapper

// nromConfig implements MapperConfig for NROM (mapper 0) and CNROM (mapper 3).
// These mappers have no PRG bank switching - all 32KB is fixed.
type nromConfig struct{}

func (c *nromConfig) BankWindowSize() int          { return 0x8000 } // 32KB
func (c *nromConfig) IsAddressFixed(_ uint16) bool { return true }
func (c *nromConfig) SwitchableWindows() []uint16  { return nil }

// uxromConfig implements MapperConfig for UxROM (mapper 2).
// 16KB switchable at $8000-$BFFF, 16KB fixed at $C000-$FFFF (last bank).
type uxromConfig struct{}

func (c *uxromConfig) BankWindowSize() int             { return 0x4000 } // 16KB
func (c *uxromConfig) IsAddressFixed(addr uint16) bool { return addr >= 0xC000 }
func (c *uxromConfig) SwitchableWindows() []uint16     { return []uint16{0x8000, 0xa000} }

// mmc3Config implements MapperConfig for MMC3 (mapper 4).
// 8KB windows with $E000-$FFFF typically fixed to the last bank.
type mmc3Config struct{}

func (c *mmc3Config) BankWindowSize() int             { return 0x2000 } // 8KB
func (c *mmc3Config) IsAddressFixed(addr uint16) bool { return addr >= 0xE000 }
func (c *mmc3Config) SwitchableWindows() []uint16     { return []uint16{0x8000, 0xa000, 0xc000} }

// axromConfig implements MapperConfig for AxROM (mapper 7).
// Full 32KB switchable, no fixed region.
type axromConfig struct{}

func (c *axromConfig) BankWindowSize() int          { return 0x8000 } // 32KB
func (c *axromConfig) IsAddressFixed(_ uint16) bool { return false }
func (c *axromConfig) SwitchableWindows() []uint16  { return []uint16{0x8000, 0xa000, 0xc000, 0xe000} }

// mapperConfigs maps NES mapper numbers to their configuration.
var mapperConfigs = map[byte]MapperConfig{
	0: &nromConfig{},  // NROM
	2: &uxromConfig{}, // UxROM
	3: &nromConfig{},  // CNROM (PRG fixed like NROM)
	4: &mmc3Config{},  // MMC3
	7: &axromConfig{}, // AxROM
}

// GetMapperConfig returns the configuration for the given mapper number.
// Returns NROM configuration (no switching) for unsupported mappers.
func GetMapperConfig(mapperNum byte) MapperConfig {
	if cfg, ok := mapperConfigs[mapperNum]; ok {
		return cfg
	}
	return &nromConfig{}
}
