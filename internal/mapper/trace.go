package mapper

import (
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
)

// Mapping identifies the physical bytes visible in the eight 8 KiB CPU windows.
// Unmapped windows contain -1. Register latches are deliberately excluded so
// traces with identical address translation can share decoded instructions.
type Mapping [8]int

// Exploration contains discovered mappings and reports bounded analysis.
// Limited exploration retains static fallthrough to avoid dropping continuations.
type Exploration struct {
	Traces  []Trace
	Limited bool
}

// ExplorationHints supplies CPU annotations and physical CDL flags to exploration.
type ExplorationHints struct {
	CodeHint    func(uint16) program.OffsetType
	CodeDataLog []codedatalog.PrgFlag
}

// Trace describes entry points discovered for one CPU mapping. Stops prevent
// static fallthrough past register writes; their successors have their own entries.
type Trace struct {
	State   Snapshot
	Entries []uint16
	Stops   []uint16
}

// Mapping returns the address-translation identity independently of register latches.
func (m *Mapper) Mapping(state Snapshot) Mapping {
	var key Mapping
	for i := range key {
		key[i] = -1
	}

	if m.layout == nil {
		return key
	}
	for _, win := range m.layout.Windows(state) {
		for i := 0; i < win.size; i += 0x2000 {
			key[(int(win.address)+i)/0x2000] = win.romOffset + i
		}
	}

	return key
}
