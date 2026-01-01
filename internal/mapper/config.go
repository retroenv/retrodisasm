// Package mapper provides memory mapping and bank management for ROM disassembly.
package mapper

// MapperConfig defines mapper-specific bank management behavior.
// Different NES mappers have different bank switching capabilities:
// - NROM (0): No switching, all 32KB fixed
// - UxROM (2): 16KB switchable at $8000, 16KB fixed at $C000
// - MMC3 (4): 8KB windows, $E000-$FFFF fixed
// - AxROM (7): Full 32KB switchable
type MapperConfig interface {
	// BankWindowSize returns the size of each bank window in bytes.
	// This determines how PRG ROM is divided into switchable units.
	BankWindowSize() int

	// IsAddressFixed returns true if the address is in a fixed (non-switchable) region.
	// Fixed regions always map to the last bank regardless of bank switching.
	IsAddressFixed(addr uint16) bool

	// SwitchableWindows returns the base addresses of switchable windows.
	// For example, UxROM returns []uint16{0x8000, 0xa000} since only
	// $8000-$BFFF is switchable while $C000-$FFFF is fixed.
	SwitchableWindows() []uint16
}
