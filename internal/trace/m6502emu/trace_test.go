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
	assert.Equal(t, 0, res.ConditionalBranchCount)
	assert.Equal(t, 0, res.BranchAlternateCount)
}

func TestRunCollectsBranchAlternatesForNotTakenBranch(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$01 (Z=0)
			0x8001: 0x01,
			0x8002: 0xF0, // beq +4 (not taken)
			0x8003: 0x04,
			0x8004: 0xEA, // nop (executed)
			0x8005: 0x4C, // jmp $8005
			0x8006: 0x05,
			0x8007: 0x80,
			0x8008: 0xEA, // alternate branch destination
		},
		signature: 0x77,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 64,
		MaxVisitsPerPC:  8,
		MaxBranchStates: 8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, res.ConditionalBranchCount)
	assert.Equal(t, 1, res.BranchAlternateCount)
	assert.Equal(t, uint16(0x8002), res.BranchAlternates[0].FromPC)
	assert.Equal(t, uint16(0x8008), res.BranchAlternates[0].Address)
	assert.Equal(t, uint64(0x77), res.BranchAlternates[0].MappingSignature)
	assert.False(t, res.BranchAlternates[0].Taken)
}

func TestRunCollectsBranchAlternatesForTakenBranch(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$00 (Z=1)
			0x8001: 0x00,
			0x8002: 0xF0, // beq +3 (taken -> $8007)
			0x8003: 0x03,
			0x8004: 0xEA, // alternate fallthrough destination
			0x8005: 0xEA,
			0x8006: 0xEA,
			0x8007: 0x4C, // jmp $8007
			0x8008: 0x07,
			0x8009: 0x80,
		},
		signature: 0x88,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 64,
		MaxVisitsPerPC:  8,
		MaxBranchStates: 8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, res.ConditionalBranchCount)
	assert.Equal(t, 1, res.BranchAlternateCount)
	assert.Equal(t, uint16(0x8002), res.BranchAlternates[0].FromPC)
	assert.Equal(t, uint16(0x8004), res.BranchAlternates[0].Address)
	assert.True(t, res.BranchAlternates[0].Taken)
}

func TestRunBuildsMapperWriteHotspots(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$01
			0x8001: 0x01,
			0x8002: 0x8D, // sta $8000
			0x8003: 0x00,
			0x8004: 0x80,
			0x8005: 0x8D, // sta $A000
			0x8006: 0x00,
			0x8007: 0xA0,
			0x8008: 0x8D, // sta $8000
			0x8009: 0x00,
			0x800A: 0x80,
			0x800B: 0x4C, // jmp $800B
			0x800C: 0x0B,
			0x800D: 0x80,
		},
		signature: 0x42,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 32,
		MaxVisitsPerPC:  4,
	})
	assert.NoError(t, err)

	assert.Equal(t, 3, len(res.BankSwitchWrites))
	assert.Equal(t, 2, res.MapperWriteUniqueAddresses)
	assert.Equal(t, 3, res.MapperWriteUniquePCs)
	assert.Equal(t, 3, res.MapperWriteUniqueTransitions)
	assert.True(t, len(res.MapperWriteAddressHotspots) > 0)
	assert.Equal(t, uint16(0x8000), res.MapperWriteAddressHotspots[0].Address)
	assert.Equal(t, 2, res.MapperWriteAddressHotspots[0].Count)
	assert.Equal(t, 2, res.MapperWriteAddressHotspots[0].ChangedCount)
}

func TestRunBranchAlternatesBudget(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$01
			0x8001: 0x01,
			0x8002: 0xF0, // beq +8 (not taken)
			0x8003: 0x08, // alt=$800C
			0x8004: 0xA9, // lda #$01
			0x8005: 0x01,
			0x8006: 0xF0, // beq +6 (not taken)
			0x8007: 0x06, // alt=$800E
			0x8008: 0x4C, // jmp $8008
			0x8009: 0x08,
			0x800A: 0x80,
			0x800C: 0xEA, // first alternate
			0x800E: 0xEA, // second alternate
		},
		signature: 0x99,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 64,
		MaxVisitsPerPC:  8,
		MaxBranchStates: 1,
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, res.ConditionalBranchCount)
	assert.Equal(t, 1, res.BranchAlternateCount)
	assert.Equal(t, 1, res.BranchAlternateBudgetDrops)
}

func TestRunExecutesQueuedBranchState(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$01 (Z=0)
			0x8001: 0x01,
			0x8002: 0xF0, // beq +4 (not taken on primary path)
			0x8003: 0x04, // alternate target = $8008
			0x8004: 0x4C, // jmp $8004 (primary path loop)
			0x8005: 0x04,
			0x8006: 0x80,
			0x8008: 0xEA, // alternate branch path
			0x8009: 0x4C, // jmp $8008
			0x800A: 0x08,
			0x800B: 0x80,
		},
		signature: 0xAB,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 64,
		MaxVisitsPerPC:  3,
		MaxBranchStates: 8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, res.BranchAlternateCount)
	assert.True(t, res.BranchStatesExecuted > 0)

	var sawAlternatePC bool
	for _, step := range res.Steps {
		if step.PC == 0x8008 {
			sawAlternatePC = true
			break
		}
	}
	assert.True(t, sawAlternatePC)
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

func (m *mockMapper) SnapshotRuntimeState() any {
	return m.signature
}

func (m *mockMapper) RestoreRuntimeState(snapshot any) bool {
	signature, ok := snapshot.(uint64)
	if !ok {
		return false
	}
	m.signature = signature
	return true
}
