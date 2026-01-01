package mapper

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

// mockArchitecture is a mock implementation of the architecture interface
type mockArchitecture struct {
	bankWindowSize int
}

func (m *mockArchitecture) BankWindowSize(cart *cartridge.Cartridge) int {
	return m.bankWindowSize
}

func TestNew_SingleBank(t *testing.T) {
	// Create minimal cartridge with single bank (CHIP-8 style)
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000), // 4KB program
	}

	arch := &mockArchitecture{bankWindowSize: 0} // 0 = single bank

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	assert.NotNil(t, mapper)
	assert.Equal(t, 1, len(mapper.banks))
	assert.Equal(t, 0, mapper.bankWindowSize)
}

func TestNew_MultiBank(t *testing.T) {
	// Create cartridge with multiple banks (NES style)
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x8000), // 32KB program (2 x 16KB banks)
	}

	arch := &mockArchitecture{bankWindowSize: 0x4000} // 16KB banks

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	assert.NotNil(t, mapper)
	assert.Equal(t, 1, len(mapper.banks))
	assert.Equal(t, 0x4000, mapper.bankWindowSize)
}

func TestSetCodeBaseAddress(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	mapper.SetCodeBaseAddress(0x200)
	assert.Equal(t, uint16(0x200), mapper.codeBaseAddress)
}

func TestReadMemory_SingleBank(t *testing.T) {
	// Create single bank cartridge
	cart := &cartridge.Cartridge{
		PRG: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Set code base address for CHIP-8
	mapper.SetCodeBaseAddress(0x200)

	// Read memory at offset 0 (address 0x200)
	b := mapper.ReadMemory(0x200)
	assert.Equal(t, byte(0x00), b)

	// Read memory at offset 2 (address 0x202)
	b = mapper.ReadMemory(0x202)
	assert.Equal(t, byte(0x02), b)

	// Read memory at offset 5 (address 0x205)
	b = mapper.ReadMemory(0x205)
	assert.Equal(t, byte(0x05), b)
}

func TestReadMemory_MultiBank(t *testing.T) {
	// Create multi-bank cartridge
	prg := make([]byte, 0x4000) // 16KB
	prg[0] = 0xAA
	prg[0x2000] = 0xBB
	prg[0x3FFF] = 0xCC

	cart := &cartridge.Cartridge{PRG: prg}
	arch := &mockArchitecture{bankWindowSize: 0x2000} // 8KB banks

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Read from first bank window (0x8000)
	b := mapper.ReadMemory(0x8000)
	assert.Equal(t, byte(0xAA), b)

	// Read from second bank window (0xA000)
	b = mapper.ReadMemory(0xA000)
	assert.Equal(t, byte(0xBB), b)

	// Read from end of last bank
	b = mapper.ReadMemory(0xFFFF)
	assert.Equal(t, byte(0xCC), b)
}

func TestMappedBank_SingleBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	mapped := mapper.MappedBank(0x200)
	assert.NotNil(t, mapped)
	assert.Equal(t, 0, mapped.ID())
}

func TestMappedBank_MultiBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x4000),
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Get mapped bank for 0x8000 window
	mapped := mapper.MappedBank(0x8000)
	assert.NotNil(t, mapped)
	assert.Equal(t, 0, mapped.ID())

	// Get mapped bank for 0xA000 window
	mapped = mapper.MappedBank(0xA000)
	assert.NotNil(t, mapped)
}

func TestMappedBankIndex_SingleBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x200)

	// Index should be address - codeBaseAddress
	index := mapper.MappedBankIndex(0x200)
	assert.Equal(t, uint16(0), index)

	index = mapper.MappedBankIndex(0x250)
	assert.Equal(t, uint16(0x50), index)
}

func TestMappedBankIndex_MultiBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x4000),
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Index should be address % bankWindowSize
	index := mapper.MappedBankIndex(0x8000)
	assert.Equal(t, uint16(0), index)

	index = mapper.MappedBankIndex(0x8100)
	assert.Equal(t, uint16(0x100), index)

	index = mapper.MappedBankIndex(0xA000)
	assert.Equal(t, uint16(0), index)
}

func TestOffsetInfo(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x200)

	// Get offset info
	offsetInfo := mapper.OffsetInfo(0x200)
	assert.NotNil(t, offsetInfo)

	// Set some data in the offset
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	offsetInfo = mapper.OffsetInfo(0x200)
	assert.True(t, offsetInfo.IsType(program.CodeOffset))
}

func TestOffsetInfo_MultiBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x4000),
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Mark an offset in the first bank
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)

	// Get offset info from first bank window
	offsetInfo := mapper.OffsetInfo(0x8000)
	assert.NotNil(t, offsetInfo)
	assert.True(t, offsetInfo.IsType(program.CodeOffset))
}

func TestLog2(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{16, 4},
		{32, 5},
		{64, 6},
	}

	for _, tt := range tests {
		result := log2(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestBankCount_SingleBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	assert.Equal(t, 1, mapper.BankCount())
}

func TestBankCount_MultiBank(t *testing.T) {
	// Create cartridge with 4 x 32KB banks = 128KB
	// Banks are created in 32KB chunks by initializeBanks
	cart := &cartridge.Cartridge{
		PRG:    make([]byte, 0x20000), // 128KB = 4 x 32KB banks
		Mapper: 7,                     // AxROM
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	assert.Equal(t, 4, mapper.BankCount())
}

func TestBankVectors(t *testing.T) {
	// Create cartridge with 2 x 32KB banks = 64KB
	// Banks are created in 32KB chunks, vectors at last 6 bytes of each
	prg := make([]byte, 0x10000) // 64KB = 2 x 32KB banks

	// Set vectors in bank 0 (last 6 bytes: 0x7FFA-0x7FFF)
	prg[0x7FFA] = 0x00 // NMI low
	prg[0x7FFB] = 0x80 // NMI high = $8000
	prg[0x7FFC] = 0x10 // Reset low
	prg[0x7FFD] = 0x80 // Reset high = $8010
	prg[0x7FFE] = 0x20 // IRQ low
	prg[0x7FFF] = 0x80 // IRQ high = $8020

	// Set vectors in bank 1 (last 6 bytes: 0xFFFA-0xFFFF)
	prg[0xFFFA] = 0x00 // NMI low
	prg[0xFFFB] = 0x90 // NMI high = $9000
	prg[0xFFFC] = 0x10 // Reset low
	prg[0xFFFD] = 0x90 // Reset high = $9010
	prg[0xFFFE] = 0x20 // IRQ low
	prg[0xFFFF] = 0x90 // IRQ high = $9020

	cart := &cartridge.Cartridge{PRG: prg, Mapper: 7}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	vectors0 := mapper.BankVectors(0)
	assert.Equal(t, uint16(0x8000), vectors0[0]) // NMI
	assert.Equal(t, uint16(0x8010), vectors0[1]) // Reset
	assert.Equal(t, uint16(0x8020), vectors0[2]) // IRQ

	vectors1 := mapper.BankVectors(1)
	assert.Equal(t, uint16(0x9000), vectors1[0]) // NMI
	assert.Equal(t, uint16(0x9010), vectors1[1]) // Reset
	assert.Equal(t, uint16(0x9020), vectors1[2]) // IRQ

	// Invalid bank index returns empty vectors
	vectorsInvalid := mapper.BankVectors(-1)
	assert.Equal(t, uint16(0), vectorsInvalid[0])
	assert.Equal(t, uint16(0), vectorsInvalid[1])
	assert.Equal(t, uint16(0), vectorsInvalid[2])

	vectorsInvalid = mapper.BankVectors(100)
	assert.Equal(t, uint16(0), vectorsInvalid[0])
}

func TestIsAddressFixed_AxROM(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG:    make([]byte, 0x8000),
		Mapper: 7, // AxROM - fully switchable
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// AxROM has no fixed regions
	assert.False(t, mapper.IsAddressFixed(0x8000))
	assert.False(t, mapper.IsAddressFixed(0xC000))
	assert.False(t, mapper.IsAddressFixed(0xE000))
	assert.False(t, mapper.IsAddressFixed(0xFFFF))
}

func TestIsAddressFixed_UxROM(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG:    make([]byte, 0x8000),
		Mapper: 2, // UxROM - $C000-$FFFF fixed
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// UxROM: $8000-$BFFF switchable, $C000-$FFFF fixed
	assert.False(t, mapper.IsAddressFixed(0x8000))
	assert.False(t, mapper.IsAddressFixed(0xBFFF))
	assert.True(t, mapper.IsAddressFixed(0xC000))
	assert.True(t, mapper.IsAddressFixed(0xFFFF))
}

func TestMapBank_AxROM(t *testing.T) {
	// Create 3 x 32KB banks = 96KB with identifiable data
	// Banks are created in 32KB chunks
	prg := make([]byte, 0x18000) // 96KB = 3 x 32KB banks
	prg[0x00000] = 0xAA          // Bank 0, offset 0
	prg[0x08000] = 0xBB          // Bank 1, offset 0
	prg[0x10000] = 0xCC          // Bank 2, offset 0 (last bank)

	cart := &cartridge.Cartridge{PRG: prg, Mapper: 7}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	assert.Equal(t, 3, mapper.BankCount())

	// Default mapping: bank 0 at $8000, last bank at $E000
	assert.Equal(t, byte(0xAA), mapper.ReadMemory(0x8000))

	// Map bank 1 - AxROM maps all 4 windows
	mapper.MapBank(1)
	assert.Equal(t, byte(0xBB), mapper.ReadMemory(0x8000))

	// Map bank 2
	mapper.MapBank(2)
	assert.Equal(t, byte(0xCC), mapper.ReadMemory(0x8000))

	// Restore default mapping
	mapper.RestoreDefaultMapping()
	assert.Equal(t, byte(0xAA), mapper.ReadMemory(0x8000))
}

func TestMapBank_InvalidIndex(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG:    make([]byte, 0x8000),
		Mapper: 7,
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Store original value
	original := mapper.ReadMemory(0x8000)

	// Invalid bank indices should not change mapping
	mapper.MapBank(-1)
	assert.Equal(t, original, mapper.ReadMemory(0x8000))

	mapper.MapBank(100)
	assert.Equal(t, original, mapper.ReadMemory(0x8000))
}

func TestMapBank_NROM(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG:    make([]byte, 0x8000),
		Mapper: 0, // NROM - no switchable windows
	}
	arch := &mockArchitecture{bankWindowSize: 0x2000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	original := mapper.ReadMemory(0x8000)

	// NROM has no switchable windows, MapBank should do nothing
	mapper.MapBank(1)
	assert.Equal(t, original, mapper.ReadMemory(0x8000))
}
