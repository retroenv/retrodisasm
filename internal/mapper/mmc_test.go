package mapper

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestLegacyBusConflictCandidates(t *testing.T) {
	cart := &cartridge.Cartridge{
		Mapper: 2,
		PRG:    make([]byte, 4*0x4000),
	}
	cart.PRG[0] = 1
	mem, err := New(&mockArchitecture{bankWindowSize: 0x2000}, cart)
	assert.NoError(t, err)
	var offsets []int
	for state := range mem.WriteCandidates(mem.Snapshot(), 0x8000, 3) {
		offsets = append(offsets, mem.Mapping(state)[4])
	}
	assert.Equal(t, []int{3 * 0x4000, 0x4000}, offsets)
}
