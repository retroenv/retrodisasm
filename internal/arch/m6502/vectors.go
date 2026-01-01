package m6502

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes"
	"github.com/retroenv/retrogolib/log"
)

func (ar *Arch6502) Initialize() error {
	if err := ar.initializeIrqHandlers(); err != nil {
		return fmt.Errorf("initializing IRQ handlers: %w", err)
	}
	return nil
}

// InitializeBankVectors adds vector entry points for a specific bank.
// Uses mapper-specific knowledge to determine which vectors to trace:
// - Fixed regions (e.g., $C000-$FFFF for UNROM): Already traced from last bank, skip
// - Switchable regions: Trace if vector differs from last bank's vector
// This is called after the bank is mapped to the address space.
func (ar *Arch6502) InitializeBankVectors(bankIndex int) {
	vectors := ar.mapper.BankVectors(bankIndex)
	vectorNames := []string{"NMI", "Reset", "IRQ"}

	for i, addr := range vectors {
		// Skip vectors in fixed regions - they always point to last bank's code
		// which was already traced during initial processing
		if ar.mapper.IsAddressFixed(addr) {
			ar.logger.Debug("Skipping vector in fixed region",
				log.Int("bank", bankIndex),
				log.String("type", vectorNames[i]),
				log.Hex("address", addr))
			continue
		}

		// Always set label for switchable vectors (even if same address as last bank,
		// since it's different code in each bank)
		offsetInfo := ar.mapper.OffsetInfo(addr)
		if offsetInfo != nil && offsetInfo.Label == "" {
			offsetInfo.Label = fmt.Sprintf("%s_Bank%d", vectorNames[i], bankIndex)
			offsetInfo.SetType(program.CallDestination)
		}

		if !ar.isValidVectorAddress(addr) {
			continue
		}
		if !ar.isValidOpcodeAt(addr) {
			ar.logger.Debug("Skipping bank vector with invalid opcode",
				log.Int("bank", bankIndex),
				log.String("type", vectorNames[i]),
				log.Hex("address", addr))
			continue
		}

		ar.logger.Debug("Adding bank vector entry point",
			log.Int("bank", bankIndex),
			log.String("type", vectorNames[i]),
			log.Hex("address", addr))

		ar.dis.AddAddressToParse(addr, addr, 0, nil, false)
	}
}

// initializeIrqHandlers reads the 3 IRQ handler addresses and adds them to the addresses to be
// followed for execution flow. Multiple handler can point to the same address.
// nolint:funlen
func (ar *Arch6502) initializeIrqHandlers() error {
	opts := ar.dis.Options()
	handlers := program.Handlers{
		NMI:   "0",
		Reset: "Reset",
		IRQ:   "0",
	}

	nmi, err := ar.dis.ReadMemoryWord(m6502.NMIAddress)
	if err != nil {
		return fmt.Errorf("reading NMI address: %w", err)
	}
	if nmi != 0 {
		ar.logger.Debug("NMI handler", log.Hex("address", nmi))
		offsetInfo := ar.mapper.OffsetInfo(nmi)
		if offsetInfo != nil {
			offsetInfo.Label = "NMI"
			offsetInfo.SetType(program.CallDestination)
		}
		handlers.NMI = "NMI"
	}

	var reset uint16
	if opts.Binary {
		reset = uint16(nes.CodeBaseAddress)
	} else {
		reset, err = ar.dis.ReadMemoryWord(m6502.ResetAddress)
		if err != nil {
			return fmt.Errorf("reading reset address: %w", err)
		}
	}

	ar.logger.Debug("Reset handler", log.Hex("address", reset))
	offsetInfo := ar.mapper.OffsetInfo(reset)
	if offsetInfo != nil {
		if offsetInfo.Label != "" {
			handlers.NMI = "Reset"
		}
		offsetInfo.Label = "Reset"
		offsetInfo.SetType(program.CallDestination)
	}

	irq, err := ar.dis.ReadMemoryWord(m6502.IrqAddress)
	if err != nil {
		return fmt.Errorf("reading IRQ address: %w", err)
	}
	if irq != 0 {
		ar.logger.Debug("IRQ handler", log.Hex("address", irq))
		offsetInfo = ar.mapper.OffsetInfo(irq)
		if offsetInfo != nil {
			if offsetInfo.Label == "" {
				offsetInfo.Label = "IRQ"
				handlers.IRQ = "IRQ"
			} else {
				handlers.IRQ = offsetInfo.Label
			}
			offsetInfo.SetType(program.CallDestination)
		}
	}

	if nmi == reset {
		handlers.NMI = handlers.Reset
	}
	if irq == reset {
		handlers.IRQ = handlers.Reset
	}

	ar.calculateCodeBaseAddress(reset)

	// add IRQ handlers to be parsed after the code base address has been calculated
	ar.dis.AddAddressToParse(nmi, nmi, 0, nil, false)
	ar.dis.AddAddressToParse(reset, reset, 0, nil, false)
	ar.dis.AddAddressToParse(irq, irq, 0, nil, false)

	ar.dis.SetHandlers(handlers)
	return nil
}

// calculateCodeBaseAddress calculates the code base address that is assumed by the code.
// If the code size is only 0x4000 it will be mirror-mapped into the 0x8000 byte of RAM starting at
// 0x8000. The handlers can be set to any of the 2 mirrors as base, based on this the code base
// address is calculated. This ensures that jsr instructions will result in the same opcode, as it
// is based on the code base address.
func (ar *Arch6502) calculateCodeBaseAddress(resetHandler uint16) {
	cart := ar.dis.Cart()
	halfPrg := len(cart.PRG) % 0x8000
	codeBaseAddress := uint16(0x8000 + halfPrg)
	vectorsStartAddress := uint16(m6502.InterruptVectorStartAddress)

	// fix up calculated code base address for half sized PRG ROMs that have a different
	// code base address configured in the assembler, like "M.U.S.C.L.E."
	if resetHandler < codeBaseAddress {
		codeBaseAddress = nes.CodeBaseAddress
		vectorsStartAddress -= uint16(halfPrg)
	}

	ar.dis.SetCodeBaseAddress(codeBaseAddress)
	ar.dis.SetVectorsStartAddress(vectorsStartAddress)
}

// isValidVectorAddress checks if a vector address appears valid for tracing.
func (ar *Arch6502) isValidVectorAddress(addr uint16) bool {
	if addr == 0x0000 || addr == 0xFFFF {
		return false
	}
	if addr < nes.CodeBaseAddress { // < 0x8000
		return false
	}
	if addr >= m6502.InterruptVectorStartAddress { // >= 0xFFFA
		return false
	}
	return true
}

// isValidOpcodeAt checks if the byte at addr is a valid 6502 opcode.
func (ar *Arch6502) isValidOpcodeAt(addr uint16) bool {
	b := ar.mapper.ReadMemory(addr)
	opcode := m6502.Opcodes[b]
	return opcode.Instruction != nil
}
