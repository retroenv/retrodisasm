package m6502

import (
	"context"
	"slices"
	"testing"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestExploreMMC1SerialWrites(t *testing.T) {
	cart := &cartridge.Cartridge{
		Mapper: 1,
		PRG:    make([]byte, 4*0x4000),
	}
	copy(cart.PRG[3*0x4000:], []byte{
		0xa9, 2, 0x8d, 0, 0xe0, 0x4a, 0x8d, 0, 0xe0,
		0x4a, 0x8d, 0, 0xe0, 0x4a, 0x8d, 0, 0xe0,
		0x4a, 0x8d, 0, 0xe0, 0x4c, 0, 0x80,
	})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0, 0xc0, 0, 0})
	ar := &Arch6502{}
	mem, err := mapper.New(ar, cart)
	assert.NoError(t, err)
	result, err := ar.ExploreMappings(t.Context(), mem, mapper.ExplorationHints{})
	assert.NoError(t, err)
	// Serial latch states with unchanged windows must not duplicate traces.
	assert.Len(t, result.Traces, 4)
	found := false
	for _, trace := range result.Traces {
		if mem.Mapping(trace.State)[4] == 2*0x4000 {
			found = slices.Contains(trace.Entries, 0xc015)
		}
	}
	assert.True(t, found)
}

func TestExploreUnknownBankWrite(t *testing.T) {
	cart := &cartridge.Cartridge{
		Mapper: 2,
		PRG:    make([]byte, 4*0x4000),
	}
	// LDA $00, STA $8000, RTS in the fixed last bank.
	copy(cart.PRG[3*0x4000:], []byte{0xa5, 0, 0x8d, 0, 0x80, 0x60})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0, 0xc0, 0, 0})

	ar := &Arch6502{}
	mem, err := mapper.New(ar, cart)
	assert.NoError(t, err)

	result, err := ar.ExploreMappings(t.Context(), mem, mapper.ExplorationHints{})
	assert.NoError(t, err)
	assert.Len(t, result.Traces, 4)

	for _, trace := range result.Traces {
		assert.True(t, slices.Contains(trace.Entries, 0xc005))
		assert.True(t, slices.Contains(trace.Stops, 0xc002))
	}
}

func TestExploreMappingsCancellation(t *testing.T) {
	cart := &cartridge.Cartridge{
		Mapper: 2,
		PRG:    make([]byte, 0x8000),
	}
	cart.PRG[0x7ffd] = 0x80

	ar := &Arch6502{}
	mem, err := mapper.New(ar, cart)
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = ar.ExploreMappings(ctx, mem, mapper.ExplorationHints{})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMappingLimitPreservesStaticContinuations(t *testing.T) {
	// Exhausting discovery must not leave a stop at a write whose successor
	// could not be explored. The ordinary static trace can still follow it.
	cart := &cartridge.Cartridge{
		Mapper: 2,
		PRG:    make([]byte, 0x8000),
	}

	ar := &Arch6502{}
	mem, err := mapper.New(ar, cart)
	assert.NoError(t, err)

	ex := &mappingExplorer{
		mem:          mem,
		seen:         make(map[flowLocation]registers),
		traces:       make(map[mapper.Mapping]*mappingTrace),
		mappingLimit: 1,
	}
	state := mem.Snapshot()
	ex.trace(state)

	loc := flowLocation{
		state: state,
		pc:    0xc000,
	}
	next := loc
	next.pc += 3

	value := knownByte{
		value: 1,
		known: true,
	}
	ex.followWrite(loc, next, registers{}, 0x8000, value)

	result := ex.results()
	assert.True(t, result.Limited)
	assert.Len(t, result.Traces, 1)
	assert.Empty(t, result.Traces[0].Stops)
}

func TestPhysicalOffset(t *testing.T) {
	key := mapper.Mapping{-1, -1, -1, -1, 0x4000, 0x6000, -1, -1}

	assert.Equal(t, 0x4000, physicalOffset(key, 0x8000))
	assert.Equal(t, 0x6010, physicalOffset(key, 0xa010))
	assert.Equal(t, -1, physicalOffset(key, 0xc000))
	assert.Equal(t, -1, physicalOffset(key, 0x0000))
}

func TestStopAtUnofficialInstruction(t *testing.T) {
	official := cpu6502.Opcodes[0xea].Instruction
	unofficial := cpu6502.Opcodes[0x02].Instruction
	assert.Equal(t, cpu6502.KilName, unofficial.Name)

	tests := []struct {
		name              string
		ins               *cpu6502.Instruction
		branchDestination bool
		hint              program.OffsetType
		want              bool
	}{
		{name: "official", ins: official, want: false},
		{name: "unofficial", ins: unofficial, want: true},
		{name: "unofficial branch destination", ins: unofficial, branchDestination: true, want: false},
		{name: "unofficial code hint", ins: unofficial, hint: program.CodeOffset, want: false},
		{name: "unofficial data hint", ins: unofficial, hint: program.DataOffset, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stopAtUnofficialInstruction(tt.ins, tt.branchDestination, tt.hint)
			assert.Equal(t, tt.want, got)
		})
	}
}
