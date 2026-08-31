package jumpengine

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestProcessJumpEngineEntryStopsAtCompositeCodeOffset(t *testing.T) {
	logger := log.NewTestLogger(t)
	mapper := newMockMapper()
	dis := newMockDisasm(0x10000)
	dis.Memory[0x8010] = 0x01
	dis.Memory[0x8011] = 0x80
	mapper.OffsetInfo(0x8011).SetType(program.CodeOffset | program.CallDestination)
	je := New(logger, &mockArchitecture{})
	je.InjectDependencies(Dependencies{Disasm: dis, Mapper: mapper})
	caller := &jumpEngineCaller{}

	found, err := je.processJumpEngineEntry(0x8010, caller, 0x8000)

	assert.NoError(t, err)
	assert.False(t, found)
	assert.True(t, caller.terminated)
	assert.False(t, mapper.OffsetInfo(0x8010).IsType(program.FunctionReference))
	assert.True(t, mapper.OffsetInfo(0x8011).IsType(program.CodeOffset|program.CallDestination))
}

// TestScanForNewJumpEngineEntry_MultipleTerminated verifies that multiple terminated
// jump engine callers are correctly removed from the processing queue.
func TestScanForNewJumpEngineEntry_MultipleTerminated(t *testing.T) {
	logger := log.NewTestLogger(t)
	ar := &mockArchitecture{}
	mapper := newMockMapper()
	dis := newMockDisasm(0x10000)
	je := New(logger, ar)
	je.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Create multiple terminated jump engine callers that should all be removed.
	caller1 := &jumpEngineCaller{
		tableStartAddress: 0x8010,
		entries:           2,
		terminated:        true,
	}
	caller2 := &jumpEngineCaller{
		tableStartAddress: 0x8020,
		entries:           3,
		terminated:        true,
	}
	caller3 := &jumpEngineCaller{
		tableStartAddress: 0x8030,
		entries:           1,
		terminated:        true,
	}
	je.jumpEngineCallers = []*jumpEngineCaller{caller1, caller2, caller3}

	found, err := je.ScanForNewJumpEngineEntry(0x8000)

	assert.NoError(t, err)
	assert.False(t, found, "should not find any new entries when all are terminated")
	assert.Len(t, je.jumpEngineCallers, 0, "all terminated entries should be removed")
}

// TestScanForNewJumpEngineEntry_MixedTerminated tests the scenario where
// some entries are terminated and others are not.
func TestScanForNewJumpEngineEntry_MixedTerminated(t *testing.T) {
	logger := log.NewTestLogger(t)
	ar := &mockArchitecture{}
	mapper := newMockMapper()
	dis := newMockDisasm(0x10000)

	// Set up memory with a valid function reference
	dis.Memory[0x8030] = 0x00 // Low byte
	dis.Memory[0x8031] = 0x80 // High byte (points to 0x8000, within code range)

	je := New(logger, ar)
	je.InjectDependencies(Dependencies{
		Disasm: dis,
		Mapper: mapper,
	})

	// Mix of terminated and active entries
	caller1 := &jumpEngineCaller{
		tableStartAddress: 0x8010,
		entries:           2,
		terminated:        true,
	}
	caller2 := &jumpEngineCaller{
		tableStartAddress: 0x8020,
		entries:           0,
		terminated:        false,
	}
	caller3 := &jumpEngineCaller{
		tableStartAddress: 0x8030,
		entries:           0,
		terminated:        false,
	}
	je.jumpEngineCallers = []*jumpEngineCaller{caller1, caller2, caller3}

	found, err := je.ScanForNewJumpEngineEntry(0x8000)

	assert.NoError(t, err)
	// The active entries should remain
	assert.Len(t, je.jumpEngineCallers, 1)
	assert.Equal(t, je.jumpEngineCallers[0], caller3)
	// At least one of the active entries should be processed
	assert.True(t, found)
	assert.Equal(t, 2, caller1.entries)
	assert.Equal(t, 0, caller2.entries)
	assert.Equal(t, 1, caller3.entries)
}
