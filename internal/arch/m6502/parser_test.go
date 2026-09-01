package m6502

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
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
	dis := &mockDisasm{pc: 0x8000, rom: []byte{0xa9, 0x01}}
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

type mockMappedBank struct {
	id int
}

func (m *mockMappedBank) ID() int {
	return m.id
}

func (m *mockMappedBank) OffsetInfo(uint16) *offset.DisasmOffset {
	return nil
}
