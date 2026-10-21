// Package mapper provides memory mapping and bank management for ROM disassembly.
package mapper

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// Mapper manages memory banking and address mapping for ROM disassembly.
type Mapper struct {
	banks  []*bank
	layout model
	state  Snapshot
	views  []*bank

	addressShifts   int
	bankWindowSize  int
	codeBaseAddress uint16 // Code base address for single-bank systems

	banksMapped []mappedBank
	mapped      []mappedBank

	dis    disasm          // Reference to disasm for single-bank systems
	vars   variableManager // Reference to variable manager
	consts constantManager // Reference to constant manager
}

// New creates a new mapper manager.
func New(ar architecture, cart *cartridge.Cartridge) (*Mapper, error) {
	bankWindowSize := ar.BankWindowSize(cart)

	if bankWindowSize == 0 {
		return createSingleBankMapper(cart)
	}

	return createMultiBankMapper(cart, bankWindowSize)
}

// SetCodeBaseAddress sets the code base address for single-bank systems.
func (m *Mapper) SetCodeBaseAddress(address uint16) {
	m.codeBaseAddress = address
	if len(m.banks) == 1 {
		m.banks[0].baseAddress = address
	}
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

func (m *Mapper) initializeBanks(prg []byte, bankSize int) {
	for i := 0; i < len(prg); {
		size := min(len(prg)-i, bankSize)

		b := prg[i : i+size]
		bnk := newBank(b)
		bnk.romOffset = i
		bnk.id = len(m.banks)
		m.banks = append(m.banks, bnk)
		i += size
	}
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

// createSingleBankMapper creates a mapper for single bank systems (e.g., CHIP-8)
func createSingleBankMapper(cart *cartridge.Cartridge) (*Mapper, error) {
	bnk := newBank(cart.PRG)
	bnk.initializeOffsets()
	bnk.mapped = true

	return &Mapper{
		banks: []*bank{bnk},
		mapped: []mappedBank{
			{bank: bnk},
		},
	}, nil
}

// createMultiBankMapper creates a mapper for multi-bank systems (e.g., NES)
func createMultiBankMapper(cart *cartridge.Cartridge, bankWindowSize int) (*Mapper, error) {
	prgSize := len(cart.PRG)
	ly, err := nesLayout(cart)
	if err != nil {
		return nil, err
	}
	mappedBanks := prgSize / bankWindowSize
	mappedWindows := 0x10000 / bankWindowSize

	m := &Mapper{
		layout:         ly,
		addressShifts:  16 - log2(mappedWindows),
		bankWindowSize: bankWindowSize,
		banksMapped:    make([]mappedBank, mappedBanks),
		mapped:         make([]mappedBank, mappedWindows),
	}

	m.initializeBanks(cart.PRG, ly.BankSize())
	for _, bnk := range m.banks {
		bnk.baseAddress = ly.Origin(bnk.id)
		bnk.hasVectors = int(bnk.baseAddress)+len(bnk.prg) == 0x10000
	}

	if err := m.populateBankMappings(bankWindowSize); err != nil {
		return nil, err
	}

	if err := m.SelectState(0); err != nil {
		return nil, err
	}

	return m, nil
}

// log2 computes the binary logarithm of x, rounded up to the next integer.
func log2(i int) int {
	var n, p int
	for p = 1; p < i; p += p {
		n++
	}
	return n
}
