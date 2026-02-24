// Package mapper provides memory mapping and bank management for ROM disassembly.
package mapper

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// Mapper manages memory banking and address mapping for ROM disassembly.
type Mapper struct {
	banks []*bank

	addressShifts   int
	bankWindowSize  int
	codeBaseAddress uint16 // Code base address for single-bank systems
	mapperNumber    uint16

	banksMapped []mappedBank
	mapped      []mappedBank

	mappingSnapshots map[uint64][]mappedBank
	mmc1             mmc1Runtime
	mmc5             mmc5Runtime

	dis    disasm          // Reference to disasm for single-bank systems
	vars   variableManager // Reference to variable manager
	consts constantManager // Reference to constant manager

	offsetAddressCache     map[*offset.DisasmOffset]uint16
	offsetAddressCacheBase uint16
}

var prgWindowAddresses = []uint16{0x8000, 0xA000, 0xC000, 0xE000}

// New creates a new mapper manager.
func New(ar architecture, cart *cartridge.Cartridge) (*Mapper, error) {
	bankWindowSize := ar.BankWindowSize(cart)

	if bankWindowSize == 0 {
		return createSingleBankMapper(cart)
	}

	return createMultiBankMapper(cart, bankWindowSize)
}

// createSingleBankMapper creates a mapper for single bank systems (e.g., CHIP-8)
func createSingleBankMapper(cart *cartridge.Cartridge) (*Mapper, error) {
	bnk := newBank(cart.PRG)

	m := &Mapper{
		banks: []*bank{bnk},
		mapped: []mappedBank{
			{bank: bnk},
		},
		mapperNumber:     cart.Mapper,
		mappingSnapshots: make(map[uint64][]mappedBank, 1),
	}
	m.rememberCurrentMapping()
	return m, nil
}

// createMultiBankMapper creates a mapper for multi-bank systems (e.g., NES)
func createMultiBankMapper(cart *cartridge.Cartridge, bankWindowSize int) (*Mapper, error) {
	prgSize := len(cart.PRG)
	mappedBanks := prgSize / bankWindowSize
	mappedWindows := 0x10000 / bankWindowSize

	m := &Mapper{
		addressShifts:    16 - log2(mappedWindows),
		bankWindowSize:   bankWindowSize,
		mapperNumber:     cart.Mapper,
		banksMapped:      make([]mappedBank, mappedBanks),
		mapped:           make([]mappedBank, mappedWindows),
		mappingSnapshots: make(map[uint64][]mappedBank, 16),
	}

	m.initializeBanks(cart.PRG)

	if err := m.populateBankMappings(bankWindowSize); err != nil {
		return nil, err
	}

	m.configureDefaultBankMapping()
	m.initializeRuntimeState()
	m.rememberCurrentMapping()

	return m, nil
}

// populateBankMappings creates the bank mappings for multi-bank systems
func (m *Mapper) populateBankMappings(bankWindowSize int) error {
	bankNumber := 0
	for bankIndex, bnk := range m.banks {
		if len(bnk.prg)%bankWindowSize != 0 {
			return fmt.Errorf("invalid bank alignment for bank size %d", len(bnk.prg))
		}

		for pointer := 0; pointer < len(bnk.prg); pointer += bankWindowSize {
			mapped := mappedBank{
				bank:      bnk,
				id:        bankIndex,
				dataStart: pointer,
			}
			m.banksMapped[bankNumber] = mapped
			bankNumber++
		}
	}
	return nil
}

// configureDefaultBankMapping sets up default bank mappings for NES systems
func (m *Mapper) configureDefaultBankMapping() {
	if m.bankWindowSize == 0x2000 {
		m.setMappedBank(0x8000, m.banksMapped[0])
		m.setMappedBank(0xa000, m.banksMapped[1])
		m.setMappedBank(0xc000, m.banksMapped[len(m.banksMapped)-2])
		m.setMappedBank(0xe000, m.banksMapped[len(m.banksMapped)-1])
	}
}

// BankCount returns the amount of PRG banks.
func (m *Mapper) BankCount() int {
	return len(m.banks)
}

// BankVectors reads the NMI/Reset/IRQ vectors from the raw PRG bank data.
// If the bank index is invalid or the bank is too small, zero vectors are returned.
func (m *Mapper) BankVectors(bankIndex int) [3]uint16 {
	var vectors [3]uint16
	if bankIndex < 0 || bankIndex >= len(m.banks) {
		return vectors
	}

	prg := m.banks[bankIndex].prg
	if len(prg) < 6 {
		return vectors
	}

	idx := len(prg) - 6
	for i := range 3 {
		low := uint16(prg[idx])
		idx++
		high := uint16(prg[idx])
		idx++
		vectors[i] = (high << 8) | low
	}
	return vectors
}

// MapBank maps the provided PRG bank into the full $8000-$FFFF CPU range.
// For 16KB banks this mirrors the bank as needed to cover all 4 8KB windows.
func (m *Mapper) MapBank(bankIndex int) {
	if m.bankWindowSize != 0x2000 || bankIndex < 0 || bankIndex >= len(m.banks) {
		return
	}

	entries := m.mappedEntriesForBank(bankIndex)
	if len(entries) == 0 {
		return
	}

	for i, window := range prgWindowAddresses {
		m.setMappedBank(window, entries[i%len(entries)])
	}
	m.rememberCurrentMapping()
}

// RestoreDefaultMapping restores the default PRG window mapping.
func (m *Mapper) RestoreDefaultMapping() {
	m.configureDefaultBankMapping()
	m.rememberCurrentMapping()
}

func (m *Mapper) mappedEntriesForBank(bankIndex int) []mappedBank {
	entries := make([]mappedBank, 0, 4)
	for _, entry := range m.banksMapped {
		if entry.id == bankIndex {
			entries = append(entries, entry)
		}
	}
	return entries
}

// SetCodeBaseAddress sets the code base address for single-bank systems.
func (m *Mapper) SetCodeBaseAddress(address uint16) {
	m.codeBaseAddress = address
	m.offsetAddressCache = nil
	m.offsetAddressCacheBase = 0
}

// EmittedAddressOfOffset returns the output assembly address for a mapper offset.
// This is based on the owning bank offset index plus current code base address.
func (m *Mapper) EmittedAddressOfOffset(offsetInfo *offset.DisasmOffset) (uint16, bool) {
	if offsetInfo == nil {
		return 0, false
	}
	if m.offsetAddressCache == nil || m.offsetAddressCacheBase != m.codeBaseAddress {
		cache := make(map[*offset.DisasmOffset]uint16)
		for _, bnk := range m.banks {
			for i, info := range bnk.offsets {
				if _, exists := cache[info]; exists {
					continue
				}
				cache[info] = m.codeBaseAddress + uint16(i)
			}
		}
		m.offsetAddressCache = cache
		m.offsetAddressCacheBase = m.codeBaseAddress
	}
	address, ok := m.offsetAddressCache[offsetInfo]
	return address, ok
}

func (m *Mapper) setMappedBank(address uint16, bank mappedBank) {
	var bankWindow uint16
	if m.bankWindowSize == 0 {
		// Single bank system (e.g., CHIP-8)
		bankWindow = 0
	} else {
		// Multi-bank system
		bankWindow = address >> m.addressShifts
	}
	m.mapped[bankWindow] = bank
}

func (m *Mapper) MappedBank(address uint16) offset.MappedBank {
	var bankWindow uint16
	if m.bankWindowSize == 0 {
		// Single bank system (e.g., CHIP-8)
		bankWindow = 0
	} else {
		// Multi-bank system
		bankWindow = address >> m.addressShifts
	}
	mapped := m.mapped[bankWindow]
	return mapped
}

func (m *Mapper) MappedBankIndex(address uint16) uint16 {
	var index int
	if m.bankWindowSize == 0 {
		// Single bank system (e.g., CHIP-8) - subtract code base address to get ROM offset
		index = int(address) - int(m.codeBaseAddress)
	} else {
		// Multi-bank system - use modulo for bank window
		index = int(address) % m.bankWindowSize
	}
	return uint16(index)
}

func (m *Mapper) ReadMemory(address uint16) byte {
	var bankWindow uint16
	var index int

	if m.bankWindowSize == 0 {
		// Single bank system (e.g., CHIP-8) - subtract code base address
		bankWindow = 0
		index = int(address) - int(m.codeBaseAddress)
	} else {
		// Multi-bank system - calculate bank window and index
		bankWindow = address >> m.addressShifts
		index = int(address) % m.bankWindowSize
	}

	bnk := m.mapped[bankWindow]
	pointer := bnk.dataStart + index
	b := bnk.bank.prg[pointer]
	return b
}

// MappingSignature returns a stable signature of the current mapped windows.
func (m *Mapper) MappingSignature() uint64 {
	var sig uint64 = 1469598103934665603 // FNV-1 offset basis
	const prime uint64 = 1099511628211

	for _, mapped := range m.mapped {
		var id, start uint64
		if mapped.bank != nil {
			id = uint64(mapped.id + 1)
			start = uint64(mapped.dataStart + 1)
		}

		sig ^= id
		sig *= prime
		sig ^= start
		sig *= prime
	}

	return sig
}

// ResolveAddress resolves a CPU address to a mapped bank ID and physical PRG offset.
func (m *Mapper) ResolveAddress(address uint16) (bankID int, physicalOffset uint32, ok bool) {
	var bankWindow uint16
	var index int

	if m.bankWindowSize == 0 {
		bankWindow = 0
		index = int(address) - int(m.codeBaseAddress)
	} else {
		bankWindow = address >> m.addressShifts
		index = int(address) % m.bankWindowSize
	}

	if int(bankWindow) >= len(m.mapped) {
		return 0, 0, false
	}

	mapped := m.mapped[bankWindow]
	if mapped.bank == nil {
		return 0, 0, false
	}

	pointer := mapped.dataStart + index
	if pointer < 0 || pointer >= len(mapped.bank.prg) {
		return 0, 0, false
	}

	return mapped.id, uint32(pointer), true
}

func (m *Mapper) OffsetInfo(address uint16) *offset.DisasmOffset {
	var bankWindow uint16
	if m.bankWindowSize == 0 {
		// Single bank system (e.g., CHIP-8)
		bankWindow = 0
	} else {
		// Multi-bank system
		bankWindow = address >> m.addressShifts
	}
	bnk := m.mapped[bankWindow]
	if bnk.bank == nil {
		return nil
	}

	var index int
	if m.bankWindowSize > 0 {
		// Multi-bank: use modulo to convert memory address to bank offset
		index = int(address) % m.bankWindowSize
	} else {
		// Single-bank: subtract code base address to convert memory address to ROM offset
		index = int(address) - int(m.codeBaseAddress)
	}
	pointer := bnk.dataStart + index

	// Bounds check: ensure pointer is within the offsets array
	if pointer < 0 || pointer >= len(bnk.bank.offsets) {
		return nil
	}

	offsetInfo := bnk.bank.offsets[pointer]
	return offsetInfo
}

// log2 computes the binary logarithm of x, rounded up to the next integer.
func log2(i int) int {
	var n, p int
	for p = 1; p < i; p += p {
		n++
	}
	return n
}
