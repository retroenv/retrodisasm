package mapper

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestNESMappingStates(t *testing.T) {
	// Address translation must follow complete hardware windows rather than
	// combining the beginning and end of unrelated 32 KiB output chunks.
	tests := []struct {
		name   string
		mapper uint16
		size   int
		states [][4]int
	}{
		{"NROM mirror", 0, 0x4000, [][4]int{{0, 0x2000, 0, 0x2000}}},
		{"NROM", 0, 0x8000, [][4]int{{0, 0x2000, 0x4000, 0x6000}}},
		{"CNROM", 3, 0x8000, [][4]int{{0, 0x2000, 0x4000, 0x6000}}},
		{"UNROM", 2, 0x10000, [][4]int{{0, 0x2000, 0xc000, 0xe000}, {0x4000, 0x6000, 0xc000, 0xe000}, {0x8000, 0xa000, 0xc000, 0xe000}, {0xc000, 0xe000, 0xc000, 0xe000}}},
		{"AxROM", 7, 0x10000, [][4]int{{0, 0x2000, 0x4000, 0x6000}, {0x8000, 0xa000, 0xc000, 0xe000}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cart := &cartridge.Cartridge{
				Mapper: tt.mapper,
				PRG:    make([]byte, tt.size),
			}
			mp, err := New(&mockArchitecture{bankWindowSize: 0x2000}, cart)
			assert.NoError(t, err)
			assert.Len(t, tt.states, mp.StateCount())
			for state, expected := range tt.states {
				assert.NoError(t, mp.SelectState(StateID(state)))
				for i, want := range expected {
					got, ok := mp.Resolve(0x8000 + uint16(i)*0x2000)
					assert.True(t, ok)
					assert.Equal(t, want, got)
				}
				vector, ok := mp.Resolve(0xfffa)
				assert.True(t, ok)
				assert.Equal(t, expected[3]+0x1ffa, vector)
			}
			assert.NoError(t, mp.SelectState(0))
			assert.Error(t, mp.SelectState(-1))
			assert.Error(t, mp.SelectState(StateID(mp.StateCount())))
			got, ok := mp.Resolve(0x8000)
			assert.True(t, ok)
			assert.Equal(t, 0, got)
			_, ok = mp.Resolve(0x7fff)
			assert.False(t, ok)
		})
	}
}

func TestNESMappingRejectsUnsupportedLayouts(t *testing.T) {
	for _, id := range []uint16{5, 256} {
		_, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
			Mapper: id,
			PRG:    make([]byte, 0x8000),
		})
		assert.ErrorContains(t, err, "no PRG mapping model")
	}
	for _, id := range []uint16{0, 3, 7} {
		_, err := New(&mockArchitecture{bankWindowSize: 0x2000}, &cartridge.Cartridge{
			Mapper: id,
			PRG:    make([]byte, 0x9000),
		})
		assert.Error(t, err)
	}
}
