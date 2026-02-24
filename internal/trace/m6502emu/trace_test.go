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
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})

	bus.Write(0x0003, 0xAB)
	assert.Equal(t, byte(0xAB), bus.Read(0x0003))
	assert.Equal(t, byte(0xAB), bus.Read(0x0803)) // mirrored
	assert.Equal(t, byte(0xAB), bus.Read(0x1003)) // mirrored
}

func TestNesBusPPUStatusStub(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})
	assert.Equal(t, byte(0x00), bus.Read(0x2002))
	assert.Equal(t, byte(0x80), bus.Read(0x3FFA)) // mirrored to $2002
	assert.Equal(t, byte(0x00), bus.Read(0x2002))
}

func TestNesBusPPUStatusSnapshotRestore(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})

	assert.Equal(t, byte(0x00), bus.Read(0x2002))
	assert.Equal(t, byte(0x80), bus.Read(0x2002))

	snapshot := bus.snapshot()
	expected := bus.Read(0x2002)
	bus.restore(snapshot)
	assert.Equal(t, expected, bus.Read(0x2002))
}

func TestNesBusPPUCtrlNMIEnabledAndSnapshotRestore(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})
	assert.False(t, bus.ppuNMIEnabled())

	bus.Write(0x2000, 0x80) // PPUCTRL: enable NMI
	assert.True(t, bus.ppuNMIEnabled())

	snapshot := bus.snapshot()
	bus.Write(0x2000, 0x00)
	assert.False(t, bus.ppuNMIEnabled())
	bus.restore(snapshot)
	assert.True(t, bus.ppuNMIEnabled())
}

func TestNesBusJoypadStrobeLatchShift(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})

	// Latch neutral controller state.
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)

	for range joypadButtonCount {
		value := bus.Read(joypad1Address)
		assert.Equal(t, byte(0), value&joypadButtonAMask)
		assert.Equal(t, byte(joypadOpenBusBit6), value&joypadOpenBusBit6)
	}

	// After the 8 button reads, controller keeps returning 1.
	assert.Equal(t, byte(1), bus.Read(joypad1Address)&joypadButtonAMask)
}

func TestNesBusJoypadConfiguredState(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{
		Joypad1State: 0x08, // Start pressed
	})

	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)

	// Read A,B,Select,Start bits.
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask)
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask)
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask)
	assert.Equal(t, byte(1), bus.Read(joypad1Address)&joypadButtonAMask)
}

func TestNesBusJoypadSequenceAdvancesPerLatchCycle(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{
		Joypad1Sequence: []byte{0x00, 0x08}, // no button, then Start
	})

	// First strobe cycle -> sequence[0]
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // A
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // B
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // Select
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // Start

	// Second strobe cycle -> sequence[1]
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // A
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // B
	assert.Equal(t, byte(0), bus.Read(joypad1Address)&joypadButtonAMask) // Select
	assert.Equal(t, byte(1), bus.Read(joypad1Address)&joypadButtonAMask) // Start
}

func TestNesBusJoypadSnapshotRestore(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{})

	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)

	// Consume three button bits, then snapshot.
	_ = bus.Read(joypad1Address)
	_ = bus.Read(joypad1Address)
	_ = bus.Read(joypad1Address)
	snapshot := bus.snapshot()

	expected := bus.Read(joypad1Address)
	bus.restore(snapshot)
	assert.Equal(t, expected, bus.Read(joypad1Address))
}

func TestNesBusJoypadSequenceSnapshotRestore(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{},
	}
	bus := newNesBus(&cartridge.Cartridge{}, mapper, Config{
		Joypad1Sequence: []byte{0x00, 0x08, 0x01},
	})

	// Advance to second sequence state.
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)
	snapshot := bus.snapshot()

	// Next strobe cycle should move to third sequence state (A pressed).
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)
	expectedA := bus.Read(joypad1Address) & joypadButtonAMask

	bus.restore(snapshot)
	bus.Write(joypad1Address, 0x01)
	bus.Write(joypad1Address, 0x00)
	actualA := bus.Read(joypad1Address) & joypadButtonAMask
	assert.Equal(t, expectedA, actualA)
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

func TestRunEscapesStartupCounterLoopWithDefaultVisitBudget(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA2, // ldx #$00
			0x8001: 0x00,
			0x8002: 0x8A, // txa
			0x8003: 0x48, // pha
			0x8004: 0xCA, // dex
			0x8005: 0xD0, // bne $8003 (256-iteration delay)
			0x8006: 0xFC,
			0x8007: 0xA9, // lda #$01
			0x8008: 0x01,
			0x8009: 0x8D, // sta $8000 (mapper write marker)
			0x800A: 0x00,
			0x800B: 0x80,
			0x800C: 0x4C, // jmp $800C
			0x800D: 0x0C,
			0x800E: 0x80,
		},
		signature: 0x55,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 5000,
		MaxVisitsPerPC:  8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(res.BankSwitchWrites))
	assert.Equal(t, uint16(0x8009), res.BankSwitchWrites[0].PC)
	assert.True(t, res.UniquePCCount >= 8)
}

func TestRunEscapesStartupMemoryClearLoopWithDefaultVisitBudget(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA0, // ldy #$00
			0x8001: 0x00,
			0x8002: 0xA9, // lda #$00
			0x8003: 0x00,
			0x8004: 0x85, // sta $10
			0x8005: 0x10,
			0x8006: 0xA9, // lda #$02
			0x8007: 0x02,
			0x8008: 0x85, // sta $11
			0x8009: 0x11,
			0x800A: 0xA9, // lda #$AB
			0x800B: 0xAB,
			0x800C: 0x91, // sta ($10),Y
			0x800D: 0x10,
			0x800E: 0x88, // dey
			0x800F: 0xD0, // bne $800C
			0x8010: 0xFB,
			0x8011: 0xA9, // lda #$01
			0x8012: 0x01,
			0x8013: 0x8D, // sta $8000 (mapper write marker)
			0x8014: 0x00,
			0x8015: 0x80,
			0x8016: 0x4C, // jmp $8016
			0x8017: 0x16,
			0x8018: 0x80,
		},
		signature: 0x56,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 6000,
		MaxVisitsPerPC:  8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(res.BankSwitchWrites))
	assert.Equal(t, uint16(0x8013), res.BankSwitchWrites[0].PC)
}

func TestRunEscapesLongIndexedStartupClearLoopWithDefaultVisitBudget(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$00
			0x8001: 0x00,
			0x8002: 0xA2, // ldx #$00
			0x8003: 0x00,
			0x8004: 0x95, // sta $00,X
			0x8005: 0x00,
			0x8006: 0x9D, // sta $0200,X
			0x8007: 0x00,
			0x8008: 0x02,
			0x8009: 0x9D, // sta $0300,X
			0x800A: 0x00,
			0x800B: 0x03,
			0x800C: 0x9D, // sta $0400,X
			0x800D: 0x00,
			0x800E: 0x04,
			0x800F: 0xCA, // dex
			0x8010: 0xD0, // bne $8004
			0x8011: 0xF2,
			0x8012: 0xA9, // lda #$01
			0x8013: 0x01,
			0x8014: 0x8D, // sta $8000 (mapper write marker)
			0x8015: 0x00,
			0x8016: 0x80,
			0x8017: 0x4C, // jmp $8017
			0x8018: 0x17,
			0x8019: 0x80,
		},
		signature: 0x58,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 12000,
		MaxVisitsPerPC:  8,
	})
	assert.NoError(t, err)
	if len(res.BankSwitchWrites) != 1 {
		t.Fatalf("expected 1 mapper write, got %d (halt=%q unique_pc=%d instructions=%d)",
			len(res.BankSwitchWrites), res.HaltReason, res.UniquePCCount, res.Instructions)
	}
	assert.Equal(t, uint16(0x8014), res.BankSwitchWrites[0].PC)
	assert.True(t, res.UniquePCCount >= 10)
}

func TestRunEscapesPPUDataStreamLoopWithDefaultVisitBudget(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$0F
			0x8001: 0x0F,
			0x8002: 0xA0, // ldy #$00
			0x8003: 0x00,
			0x8004: 0x8D, // sta $2007
			0x8005: 0x07,
			0x8006: 0x20,
			0x8007: 0xC8, // iny
			0x8008: 0xC0, // cpy #$20
			0x8009: 0x20,
			0x800A: 0xD0, // bne $8004
			0x800B: 0xF8,
			0x800C: 0xA9, // lda #$01
			0x800D: 0x01,
			0x800E: 0x8D, // sta $8000 (mapper write marker)
			0x800F: 0x00,
			0x8010: 0x80,
			0x8011: 0x4C, // jmp $8011
			0x8012: 0x11,
			0x8013: 0x80,
		},
		signature: 0x57,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 6000,
		MaxVisitsPerPC:  8,
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(res.BankSwitchWrites))
	assert.Equal(t, uint16(0x800E), res.BankSwitchWrites[0].PC)
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

func TestRunTriggersSyntheticNMIWhenEnabled(t *testing.T) {
	mapper := &mockMapper{
		memory: map[uint16]byte{
			0xFFFA: 0x00, // NMI vector low
			0xFFFB: 0x90, // NMI vector high -> $9000
			0xFFFC: 0x00, // reset vector low
			0xFFFD: 0x80, // reset vector high -> $8000
			0x8000: 0xA9, // lda #$80
			0x8001: 0x80,
			0x8002: 0x8D, // sta $2000 (enable NMI)
			0x8003: 0x00,
			0x8004: 0x20,
			0x8005: 0xEA, // nop
			0x8006: 0x4C, // jmp $8006
			0x8007: 0x06,
			0x8008: 0x80,
			0x9000: 0xA9, // lda #$42
			0x9001: 0x42,
			0x9002: 0x85, // sta $02
			0x9003: 0x02,
			0x9004: 0x40, // rti
		},
		signature: 0xCD,
	}

	res, err := Run(context.Background(), &cartridge.Cartridge{}, mapper, Config{
		MaxInstructions: 3000,
		MaxVisitsPerPC:  6000,
	})
	assert.NoError(t, err)

	var sawNMIHandler bool
	for _, step := range res.Steps {
		if step.PC == 0x9000 {
			sawNMIHandler = true
			break
		}
	}
	assert.True(t, sawNMIHandler)
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
