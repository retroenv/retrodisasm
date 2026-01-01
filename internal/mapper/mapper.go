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

	banksMapped []mappedBank
	mapped      []mappedBank

	dis    disasm          // Reference to disasm for single-bank systems
	vars   variableManager // Reference to variable manager
	consts constantManager // Reference to constant manager

	config MapperConfig // Mapper-specific configuration
}

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

	return &Mapper{
		banks: []*bank{bnk},
		mapped: []mappedBank{
			{bank: bnk},
		},
	}, nil
}

// createMultiBankMapper creates a mapper for multi-bank systems (e.g., NES)
func createMultiBankMapper(cart *cartridge.Cartridge, bankWindowSize int) (*Mapper, error) {
	config := GetMapperConfig(cart.Mapper)

	prgSize := len(cart.PRG)
	mappedBanks := prgSize / bankWindowSize
	mappedWindows := 0x10000 / bankWindowSize

	m := &Mapper{
		addressShifts:  16 - log2(mappedWindows),
		bankWindowSize: bankWindowSize,
		banksMapped:    make([]mappedBank, mappedBanks),
		mapped:         make([]mappedBank, mappedWindows),
		config:         config,
	}

	m.initializeBanks(cart.PRG)

	if err := m.populateBankMappings(bankWindowSize); err != nil {
		return nil, err
	}

	m.configureDefaultBankMapping()

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

// configureDefaultBankMapping sets up default bank mappings for NES systems.
// Maps first bank to $8000/$A000 and last bank to $C000/$E000.
func (m *Mapper) configureDefaultBankMapping() {
	if m.bankWindowSize == 0x2000 {
		m.setMappedBank(0x8000, m.banksMapped[0])
		m.setMappedBank(0xa000, m.banksMapped[1])
		m.setMappedBank(0xc000, m.banksMapped[len(m.banksMapped)-2])
		m.setMappedBank(0xe000, m.banksMapped[len(m.banksMapped)-1])
	}
}

// SetCodeBaseAddress sets the code base address for single-bank systems.
func (m *Mapper) SetCodeBaseAddress(address uint16) {
	m.codeBaseAddress = address
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

// BankCount returns the number of PRG banks.
func (m *Mapper) BankCount() int {
	return len(m.banks)
}

// MapBank maps the switchable windows of the specified bank to the address space.
// Only windows defined as switchable by the mapper config are affected.
// Fixed regions (e.g., $C000-$FFFF for UNROM) remain mapped to the last bank.
func (m *Mapper) MapBank(bankIndex int) {
	if m.bankWindowSize == 0 || bankIndex < 0 || bankIndex >= len(m.banks) {
		return
	}

	// Get switchable windows from mapper config
	var switchable []uint16
	if m.config != nil {
		switchable = m.config.SwitchableWindows()
	}
	if len(switchable) == 0 {
		return // No switchable windows (e.g., NROM)
	}

	// Calculate how many windows per 32KB bank
	windowsPerBank := 0x8000 / m.bankWindowSize
	baseWindow := bankIndex * windowsPerBank

	// Map only the switchable windows
	for _, addr := range switchable {
		windowOffset := int(addr-0x8000) / m.bankWindowSize
		windowIndex := baseWindow + windowOffset
		if windowIndex < len(m.banksMapped) {
			m.setMappedBank(addr, m.banksMapped[windowIndex])
		}
	}
}

// RestoreDefaultMapping restores the default bank mapping.
func (m *Mapper) RestoreDefaultMapping() {
	m.configureDefaultBankMapping()
}

// IsAddressFixed returns true if the given address is in a fixed (non-switchable) region.
// Fixed regions always map to the last bank regardless of bank switching.
func (m *Mapper) IsAddressFixed(addr uint16) bool {
	if m.config == nil {
		return false
	}
	return m.config.IsAddressFixed(addr)
}

// BankVectors reads the three interrupt vectors from the specified bank's raw PRG data.
// Returns [3]uint16 containing NMI, Reset, IRQ addresses in order.
func (m *Mapper) BankVectors(bankIndex int) [3]uint16 {
	if bankIndex < 0 || bankIndex >= len(m.banks) {
		return [3]uint16{}
	}
	bnk := m.banks[bankIndex]
	if len(bnk.prg) < 6 {
		return [3]uint16{}
	}
	var vectors [3]uint16
	idx := len(bnk.prg) - 6
	for i := range 3 {
		b1 := bnk.prg[idx]
		idx++
		b2 := bnk.prg[idx]
		idx++
		vectors[i] = uint16(b2)<<8 | uint16(b1)
	}
	return vectors
}
