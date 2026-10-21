package mapper

import (
	"fmt"
	"iter"
)

// StateID identifies a seed mapping used for static analysis.
type StateID int

// Snapshot is a comparable register state owned by a cartridge's mapping model.
// It includes latches that affect future writes even if the CPU windows agree.
type Snapshot struct {
	registers [8]byte
}

// StateCount returns the number of seed mappings, without materializing them.
func (m *Mapper) StateCount() int {
	if m.layout == nil {
		return 1
	}
	return m.layout.SeedCount()
}

// SelectState selects a seed mapping before initializing a trace.
func (m *Mapper) SelectState(id StateID) error {
	if id < 0 || int(id) >= m.StateCount() {
		return fmt.Errorf("invalid mapping state %d", id)
	}
	if m.layout != nil {
		m.selectSnapshot(m.layout.Seed(int(id)))
	}
	return nil
}

// Snapshot returns the selected hardware register state.
func (m *Mapper) Snapshot() Snapshot {
	return m.state
}

// Seeds yields initial mappings lazily, without enumerating independent banks.
func (m *Mapper) Seeds() iter.Seq[Snapshot] {
	return func(yield func(Snapshot) bool) {
		for i := range m.StateCount() {
			state := Snapshot{}
			if m.layout != nil {
				state = m.layout.Seed(i)
			}
			if !yield(state) {
				return
			}
		}
	}
}

// Write returns the effect of a CPU write without mutating an existing trace.
// The boolean reports whether the address is a modeled mapper register.
func (m *Mapper) Write(state Snapshot, address uint16, value byte) (Snapshot, bool) {
	if m.layout == nil {
		return state, false
	}
	return m.layout.Write(state, address, value)
}

// WriteCandidates includes both bus-conflict and conflict-free board wiring
// when the cartridge's legacy metadata does not distinguish them.
func (m *Mapper) WriteCandidates(state Snapshot, address uint16, value byte) iter.Seq[Snapshot] {
	return func(yield func(Snapshot) bool) {
		next, handled := m.Write(state, address, value)
		if !handled || !yield(next) || !m.layout.BusConflicts() {
			return
		}
		rom, ok := m.ReadSnapshot(state, address)
		if ok {
			conflict, _ := m.Write(state, address, value&rom)
			if conflict != next {
				yield(conflict)
			}
		}
	}
}

// IsRegister reports addresses whose writes affect the modeled mapper state.
func (m *Mapper) IsRegister(address uint16) bool {
	_, handled := m.Write(m.state, address, 0)
	return handled
}

// ReadSnapshot reads ROM in a register state without allocating analysis views.
func (m *Mapper) ReadSnapshot(state Snapshot, address uint16) (byte, bool) {
	if m.layout == nil {
		index, ok := m.Resolve(address)
		if !ok {
			return 0, false
		}
		return m.banks[0].prg[index], true
	}
	for _, win := range m.layout.Windows(state) {
		index := int(address) - int(win.address)
		if index >= 0 && index < win.size {
			physical := win.romOffset + index
			bnk := m.banks[physical/m.layout.BankSize()]
			return bnk.prg[physical-bnk.romOffset], true
		}
	}
	return 0, false
}

// NewTrace shares immutable ROM and model data while allocating independent views.
func (m *Mapper) NewTrace(state Snapshot) *Mapper {
	next := &Mapper{
		layout:          m.layout,
		addressShifts:   m.addressShifts,
		bankWindowSize:  m.bankWindowSize,
		codeBaseAddress: m.codeBaseAddress,
		banksMapped:     make([]mappedBank, len(m.banksMapped)),
		mapped:          make([]mappedBank, len(m.mapped)),
	}
	for _, bnk := range m.banks {
		next.banks = append(next.banks, &bank{
			storage:     bnk.storage,
			baseAddress: bnk.baseAddress,
			hasVectors:  bnk.hasVectors,
		})
	}
	for i, mapped := range m.banksMapped {
		mapped.bank = next.banks[mapped.id]
		next.banksMapped[i] = mapped
	}
	if m.layout == nil {
		next.banks[0].initializeOffsets()
		next.banks[0].mapped = true
		next.mapped[0] = mappedBank{bank: next.banks[0]}
	} else {
		next.selectSnapshot(state)
	}
	return next
}

// Resolve returns the physical ROM offset for an address in the selected mapping.
func (m *Mapper) Resolve(address uint16) (int, bool) {
	if m.bankWindowSize == 0 {
		index := int(address) - int(m.codeBaseAddress)
		return index, index >= 0 && index < len(m.banks[0].prg)
	}
	mapped := m.mapped[int(address)/m.bankWindowSize]
	if mapped.bank == nil {
		return 0, false
	}
	return mapped.bank.romOffset + mapped.dataStart + int(address)%m.bankWindowSize, true
}

func (m *Mapper) selectSnapshot(state Snapshot) {
	m.state = state
	clear(m.mapped)
	m.views = nil
	for _, bnk := range m.banks {
		bnk.mapped = false
	}
	for _, win := range m.layout.Windows(state) {
		for off := 0; off < win.size; off += m.bankWindowSize {
			mapped := m.banksMapped[(win.romOffset+off)/m.bankWindowSize]
			origin := win.address + uint16(off) - uint16(mapped.dataStart)
			if len(m.banks) > 1 && origin != mapped.bank.baseAddress {
				mapped.bank = m.alternateView(mapped.bank, origin)
			}
			mapped.bank.initializeOffsets()
			mapped.bank.mapped = true
			m.setMappedBank(win.address+uint16(off), mapped)
		}
	}
}

func (m *Mapper) alternateView(physical *bank, origin uint16) *bank {
	for _, view := range m.views {
		if view.id == physical.id && view.baseAddress == origin {
			return view
		}
	}
	view := &bank{
		storage:     physical.storage,
		baseAddress: origin,
		hasVectors:  int(origin)+len(physical.prg) == 0x10000,
	}
	m.views = append(m.views, view)
	return view
}
