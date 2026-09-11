package jumpengine

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
)

// mockArchitecture is a minimal mock for testing.
type mockArchitecture struct{}

func (a *mockArchitecture) AddressingParam(param any) (uint16, bool) {
	return 0, false
}

func (a *mockArchitecture) LastCodeAddress() uint16 {
	return 0xFFFF
}

func (a *mockArchitecture) ReadOpParam(int, uint16) (any, []byte, error) {
	return nil, nil, nil
}

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

// mockDisasm is a minimal mock for testing.
type mockDisasm struct {
	Memory []byte
}

func newMockDisasm() *mockDisasm {
	return &mockDisasm{
		Memory: make([]byte, 0x10000),
	}
}

func (m *mockDisasm) AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool) {
}

func (m *mockDisasm) DeleteFunctionReturnToParse(uint16) {
}

func (m *mockDisasm) ReadMemory(address uint16) (byte, error) {
	if int(address) >= len(m.Memory) {
		return 0, nil
	}
	return m.Memory[address], nil
}

func (m *mockDisasm) ReadMemoryWord(address uint16) (uint16, error) {
	low, err := m.ReadMemory(address)
	if err != nil {
		return 0, err
	}
	high, err := m.ReadMemory(address + 1)
	if err != nil {
		return 0, err
	}
	return uint16(low) | uint16(high)<<8, nil
}
