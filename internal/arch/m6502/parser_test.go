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
