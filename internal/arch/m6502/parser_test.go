package m6502

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestMarkIndexedReferenceAsDataPreservesExistingOwner(t *testing.T) {
	owner := &offset.DisasmOffset{Offset: program.Offset{Data: []byte{0x13, 0x1C}}}
	target := &offset.DisasmOffset{}
	arch := &Arch6502{mapper: &mockMapper{offsets: map[uint16]*offset.DisasmOffset{
		0xF847: owner,
		0xF848: target,
	}}}

	arch.markIndexedReferenceAsData(0xF848)

	assert.True(t, target.IsType(program.DataOffset))
	assert.Len(t, target.Data, 0)
}

func TestMarkIndexedReferenceAsDataClaimsUnownedByte(t *testing.T) {
	target := &offset.DisasmOffset{}
	arch := &Arch6502{mapper: &mockMapper{offsets: map[uint16]*offset.DisasmOffset{
		0xF848: target,
	}}}

	arch.markIndexedReferenceAsData(0xF848)

	assert.True(t, target.IsType(program.DataOffset))
	assert.Equal(t, []byte{0}, target.Data)
}

func TestInitializeOffsetInfoPreservesFunctionReference(t *testing.T) {
	dis := &mockDisasm{
		pc:  0x8000,
		rom: []byte{0xa9, 0x01},
	}
	arch := &Arch6502{dis: dis}
	offsetInfo := &offset.DisasmOffset{Offset: program.Offset{
		Data: []byte{0x25, 0x81},
		Type: program.FunctionReference,
	}}

	inspectCode, err := arch.initializeOffsetInfo(offsetInfo)

	assert.NoError(t, err)
	assert.False(t, inspectCode)
	assert.Equal(t, []byte{0x25, 0x81}, offsetInfo.Data)
}

func TestInstructionBytesInMappedBankStopsAtWindowChange(t *testing.T) {
	firstBank := &mockMappedBank{id: 0}
	lastBank := &mockMappedBank{id: 3}
	arch := &Arch6502{mapper: &mockMapper{mapped: map[uint16]offset.MappedBank{
		0xbfff: firstBank,
		0xc000: lastBank,
		0xc001: lastBank,
	}}}

	length := arch.instructionBytesInMappedBank(0xbfff, 3)

	assert.Equal(t, 1, length)
}

func TestInstructionBytesInMappedBankStopsAtMirroredBoundary(t *testing.T) {
	// Mapping the fixed UNROM bank twice keeps its ID but wraps the physical
	// ROM offset at $C000. An instruction must not own bytes past that wrap.
	ar := &Arch6502{}
	mem, err := mapper.New(ar, &cartridge.Cartridge{
		Mapper: 2,
		PRG:    make([]byte, 0x8000),
	})
	assert.NoError(t, err)
	assert.NoError(t, mem.SelectState(1))
	ar.mapper = mem
	assert.Equal(t, mem.MappedBank(0xbfff).ID(), mem.MappedBank(0xc000).ID())
	assert.Equal(t, 1, ar.instructionBytesInMappedBank(0xbfff, 3))
	assert.Equal(t, 3, ar.instructionBytesInMappedBank(0x9fff, 3))
}

type mockMappedBank struct {
	id int
}

func (m *mockMappedBank) ID() int {
	return m.id
}

func (m *mockMappedBank) OffsetInfo(uint16) *offset.DisasmOffset {
	return nil
}
