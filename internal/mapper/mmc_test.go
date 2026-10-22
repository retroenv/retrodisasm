package mapper

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestMMC1RegisterModes(t *testing.T) {
	mem, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
		Mapper: 1,
		PRG:    make([]byte, 0x10000),
	})
	assert.NoError(t, err)
	state := mem.Snapshot()
	state = writeSerial(t, mem, state, 0xe000, 2)
	assertMapping(t, mem.NewTrace(state), [4]int{0x8000, 0xa000, 0xc000, 0xe000})
	state = writeSerial(t, mem, state, 0x8000, 8)
	assertMapping(t, mem.NewTrace(state), [4]int{0, 0x2000, 0x8000, 0xa000})
	state = writeSerial(t, mem, state, 0x8000, 0)
	assertMapping(t, mem.NewTrace(state), [4]int{0x8000, 0xa000, 0xc000, 0xe000})
	state, _ = mem.Write(state, 0xe000, 1)
	assert.NotEqual(t, mem.Snapshot(), state)
	state, _ = mem.Write(state, 0x8000, 0x80)
	assert.Equal(t, byte(0), state.registers[mmc1Count])
	assertMapping(t, mem.NewTrace(state), [4]int{0x8000, 0xa000, 0xc000, 0xe000})
	assertMapping(t, mem, [4]int{0, 0x2000, 0xc000, 0xe000})
}

func TestMMC1SerialLatchUsesFifthWriteAddress(t *testing.T) {
	mem, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
		Mapper: 1,
		PRG:    make([]byte, 0x10000),
	})
	assert.NoError(t, err)
	state := mem.Snapshot()
	for i := range 5 {
		address := uint16(0x8000)
		if i == 4 {
			address = 0xffff
		}
		state, _ = mem.Write(state, address, (3>>i)&1)
		if i < 4 {
			assert.Equal(t, mem.Mapping(mem.Snapshot()), mem.Mapping(state))
		}
	}
	assertMapping(t, mem.NewTrace(state), [4]int{0xc000, 0xe000, 0xc000, 0xe000})
}

func TestMMC3IndependentWindows(t *testing.T) {
	mem, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
		Mapper: 4,
		PRG:    make([]byte, 0x10000),
	})
	assert.NoError(t, err)
	state := mem.Snapshot()
	for _, write := range [][2]uint16{{0x8000, 6}, {0x8001, 3}, {0x8000, 7}, {0x8001, 4}} {
		var handled bool
		state, handled = mem.Write(state, write[0], byte(write[1]))
		assert.True(t, handled)
	}
	assertMapping(t, mem.NewTrace(state), [4]int{0x6000, 0x8000, 0xc000, 0xe000})
	state, _ = mem.Write(state, 0x9ffe, 0x46)
	assertMapping(t, mem.NewTrace(state), [4]int{0xc000, 0x8000, 0x6000, 0xe000})
	state, _ = mem.Write(state, 0x9fff, 0xff)
	assertMapping(t, mem.NewTrace(state), [4]int{0xc000, 0x8000, 0xe000, 0xe000})
	unchanged, handled := mem.Write(state, 0x6000, 0)
	assert.False(t, handled)
	assert.Equal(t, state, unchanged)
	assertMapping(t, mem, [4]int{0, 0x2000, 0xc000, 0xe000})
}

func TestMappingViewsHaveStablePhysicalStorage(t *testing.T) {
	mem, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
		Mapper: 4,
		PRG:    make([]byte, 0x10000),
	})
	assert.NoError(t, err)
	state, _ := mem.Write(mem.Snapshot(), 0x8000, 0x46)
	next := mem.NewTrace(state)
	assert.Equal(t, mem.banks[0].storage, next.banks[0].storage)
	assert.Equal(t, uint16(0x8000), next.banks[0].baseAddress)
	assert.Equal(t, uint16(0xc000), next.banks[6].baseAddress)
	assert.Equal(t, uint16(0xc000), next.mapped[6].bank.baseAddress)
	assert.Equal(t, 0, next.mapped[6].bank.id)
	assert.False(t, next.mapped[6].bank.hasVectors)
	assert.True(t, next.mapped[7].bank.hasVectors)
	assert.True(t, mem.OffsetInfo(0x8000) != next.OffsetInfo(0xc000))
	assert.Equal(t, 16, mem.StateCount())
	count := 0
	for range mem.Seeds() {
		count++
		break
	}
	assert.Equal(t, 1, count)
}

func TestMMCRejectsUnmodeledBoardSizes(t *testing.T) {
	for _, id := range []uint16{1, 4} {
		for _, size := range []int{0, 0x4000, 0xc000, 0x100000} {
			_, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
				Mapper: id,
				PRG:    make([]byte, size),
			})
			assert.Error(t, err)
		}
	}
}

func writeSerial(t *testing.T, mem *Mapper, state Snapshot, address uint16, value byte) Snapshot {
	t.Helper()
	for i := range 5 {
		var handled bool
		state, handled = mem.Write(state, address, (value>>i)&1)
		assert.True(t, handled)
	}
	return state
}

func assertMapping(t *testing.T, mem *Mapper, expected [4]int) {
	t.Helper()
	for i, want := range expected {
		physical, ok := mem.Resolve(uint16(0x8000 + i*0x2000))
		assert.True(t, ok)
		assert.Equal(t, want, physical)
	}
}

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
