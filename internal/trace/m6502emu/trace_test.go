package m6502emu

import (
	"context"
	"testing"

	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestNesBusRAMMirroring(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper)

	bus.Write(0x0003, 0xAB)
	assert.Equal(t, byte(0xAB), byte(bus.Read(0x0003)))
	assert.Equal(t, byte(0xAB), byte(bus.Read(0x0803))) // mirrored
	assert.Equal(t, byte(0xAB), byte(bus.Read(0x1003))) // mirrored
}

func TestNesBusPPUStatusStub(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper)
	assert.Equal(t, byte(0x80), byte(bus.Read(0x2002)))
	assert.Equal(t, byte(0x80), byte(bus.Read(0x3FFA))) // mirrored to $2002
}

func TestRunCollectsStepsAndMapperWrites(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$01
			0x8001: 0x01,
			0x8002: 0x8D, // sta $8000
			0x8003: 0x00,
			0x8004: 0x80,
			0x8005: 0x4C, // jmp $8005 (self loop)
			0x8006: 0x05,
			0x8007: 0x80,
		},
		signature: 0x42,
	}

	cart := &cartridge.Cartridge{}
	res, err := Run(context.Background(), cart, mapper, Config{
		MaxInstructions: 32,
		MaxVisitsPerPC:  4,
	})
	assert.NoError(t, err)
	assert.True(t, len(res.Steps) > 0)
	assert.True(t, res.UniquePCCount > 0)
	assert.True(t, res.UniqueMappingCount > 0)
	assert.Equal(t, 1, len(res.BankSwitchWrites))
	assert.Equal(t, uint16(0x8000), res.BankSwitchWrites[0].Address)
	assert.Equal(t, byte(0x01), res.BankSwitchWrites[0].Value)
}

type mockMapper struct {
	memory    map[uint16]byte
	signature uint64
}

func (m *mockMapper) ReadMemory(address uint16) byte {
	return m.memory[address]
}

func (m *mockMapper) MappingSignature() uint64 {
	return m.signature
}

func (m *mockMapper) ResolveAddress(address uint16) (int, uint32, bool) {
	if address < 0x8000 {
		return 0, 0, false
	}
	return 0, uint32(address - 0x8000), true
}

func (m *mockMapper) ApplyMapperWrite(address uint16, _ byte) bool {
	if address < 0x8000 {
		return false
	}
	m.signature++
	return true
}
