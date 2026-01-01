package chip8

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
)

// mockMapper is a minimal mock for testing.
type mockMapper struct {
	offsets map[uint16]*offset.DisasmOffset
}

func newMockMapper() *mockMapper {
	return &mockMapper{
		offsets: make(map[uint16]*offset.DisasmOffset),
	}
}

func (m *mockMapper) OffsetInfo(address uint16) *offset.DisasmOffset {
	if offset, ok := m.offsets[address]; ok {
		return offset
	}
	offset := &offset.DisasmOffset{
		Offset: program.Offset{
			Type: program.DataOffset,
			Data: []byte{0},
		},
	}
	m.offsets[address] = offset
	return offset
}

func (m *mockMapper) ReadMemory(address uint16) byte {
	return 0
}

func (m *mockMapper) MappedBank(uint16) offset.MappedBank {
	return nil
}

func (m *mockMapper) MappedBankIndex(uint16) uint16 {
	return 0
}

func (m *mockMapper) BankCount() int {
	return 1
}

func (m *mockMapper) BankVectors(_ int) [3]uint16 {
	return [3]uint16{}
}

func (m *mockMapper) MapBank(_ int) {
}

func (m *mockMapper) RestoreDefaultMapping() {
}

func (m *mockMapper) IsAddressFixed(_ uint16) bool {
	return false
}

// mockDisasm is a minimal mock for testing.
type mockDisasm struct {
	Memory []byte
}

const mockMemorySize = 0x1000

func newMockDisasm() *mockDisasm {
	return &mockDisasm{
		Memory: make([]byte, mockMemorySize),
	}
}

func (m *mockDisasm) AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool) {
}

func (m *mockDisasm) ProgramCounter() uint16 {
	return 0
}

func (m *mockDisasm) ReadMemory(address uint16) (byte, error) {
	if int(address) >= len(m.Memory) {
		return 0, nil
	}
	return m.Memory[address], nil
}

func (m *mockDisasm) SetCodeBaseAddress(uint16) {
}
