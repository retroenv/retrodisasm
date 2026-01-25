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
	assert.Len(t, mapper.banks, 1)
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
	assert.Len(t, mapper.banks, 1)
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
