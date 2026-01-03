package m6502

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes"
	"github.com/retroenv/retrogolib/log"
)

const resetLabel = "Reset"

func (ar *Arch6502) Initialize() error {
	if err := ar.initializeIrqHandlers(); err != nil {
		return fmt.Errorf("initializing IRQ handlers: %w", err)
	}
	return nil
}

// initializeIrqHandlers reads the 3 IRQ handler addresses and adds them to the addresses to be
// followed for execution flow. Multiple handler can point to the same address.
func (ar *Arch6502) initializeIrqHandlers() error {
	opts := ar.dis.Options()
	handlers := program.Handlers{
		NMI:   "0",
		Reset: resetLabel,
		IRQ:   "0",
	}

	// In binary mode, skip reading NMI/IRQ vectors from ROM
	// as they don't exist in raw binary files
	if opts.Binary {
		return ar.initializeBinaryMode(opts, handlers)
	}

	nmi, err := ar.initializeNMIHandler(&handlers)
	if err != nil {
		return err
	}

	reset, err := ar.initializeResetHandler(&handlers)
	if err != nil {
		return err
	}

	irq, err := ar.initializeIRQHandler(&handlers)
	if err != nil {
		return err
	}

	ar.resolveSharedHandlers(&handlers, nmi, reset, irq)
	ar.calculateCodeBaseAddress(reset)
	ar.queueHandlersToParse(nmi, reset, irq)

	ar.dis.SetHandlers(handlers)
	return nil
}

func (ar *Arch6502) initializeNMIHandler(handlers *program.Handlers) (uint16, error) {
	nmi, err := ar.dis.ReadMemoryWord(m6502.NMIAddress)
	if err != nil {
		return 0, fmt.Errorf("reading NMI address: %w", err)
	}
	if nmi == 0 {
		return 0, nil
	}

	ar.logger.Debug("NMI handler", log.Hex("address", nmi))
	offsetInfo := ar.mapper.OffsetInfo(nmi)
	if offsetInfo != nil {
		offsetInfo.Label = "NMI"
		offsetInfo.SetType(program.CallDestination)
	}
	handlers.NMI = "NMI"
	return nmi, nil
}

func (ar *Arch6502) initializeResetHandler(handlers *program.Handlers) (uint16, error) {
	reset, err := ar.dis.ReadMemoryWord(m6502.ResetAddress)
	if err != nil {
		return 0, fmt.Errorf("reading reset address: %w", err)
	}

	ar.logger.Debug("Reset handler", log.Hex("address", reset))
	offsetInfo := ar.mapper.OffsetInfo(reset)
	if offsetInfo != nil {
		if offsetInfo.Label != "" {
			handlers.NMI = resetLabel
		}
		offsetInfo.Label = resetLabel
		offsetInfo.SetType(program.CallDestination)
	}
	return reset, nil
}

func (ar *Arch6502) initializeIRQHandler(handlers *program.Handlers) (uint16, error) {
	irq, err := ar.dis.ReadMemoryWord(m6502.IrqAddress)
	if err != nil {
		return 0, fmt.Errorf("reading IRQ address: %w", err)
	}
	if irq == 0 {
		return 0, nil
	}

	ar.logger.Debug("IRQ handler", log.Hex("address", irq))
	offsetInfo := ar.mapper.OffsetInfo(irq)
	if offsetInfo != nil {
		if offsetInfo.Label == "" {
			offsetInfo.Label = "IRQ"
			handlers.IRQ = "IRQ"
		} else {
			handlers.IRQ = offsetInfo.Label
		}
		offsetInfo.SetType(program.CallDestination)
	}
	return irq, nil
}

func (ar *Arch6502) resolveSharedHandlers(handlers *program.Handlers, nmi, reset, irq uint16) {
	if nmi == reset {
		handlers.NMI = handlers.Reset
	}
	if irq == reset {
		handlers.IRQ = handlers.Reset
	}
}

func (ar *Arch6502) queueHandlersToParse(nmi, reset, irq uint16) {
	if nmi != 0 {
		ar.dis.AddAddressToParse(nmi, nmi, 0, nil, false)
	}
	if reset != 0 {
		ar.dis.AddAddressToParse(reset, reset, 0, nil, false)
	}
	if irq != 0 {
		ar.dis.AddAddressToParse(irq, irq, 0, nil, false)
	}
}

// initializeBinaryMode sets up handlers for binary mode disassembly.
func (ar *Arch6502) initializeBinaryMode(opts options.Disassembler, handlers program.Handlers) error {
	// Use custom base address if specified, otherwise use default NES code base
	var reset uint16
	if opts.BaseAddress != 0 {
		reset = opts.BaseAddress
	} else {
		reset = uint16(nes.CodeBaseAddress)
	}

	ar.logger.Debug("Binary mode reset handler", log.Hex("address", reset))

	// Set code base address before accessing offsets
	ar.dis.SetCodeBaseAddress(reset)
	ar.dis.SetVectorsStartAddress(m6502.InterruptVectorStartAddress)

	offsetInfo := ar.mapper.OffsetInfo(reset)
	if offsetInfo != nil {
		offsetInfo.Label = resetLabel
		offsetInfo.SetType(program.CallDestination)
	}

	// Add reset address to parse
	ar.dis.AddAddressToParse(reset, reset, 0, nil, false)

	ar.dis.SetHandlers(handlers)
	return nil
}

// calculateCodeBaseAddress calculates the code base address that is assumed by the code.
// If the code size is only 0x4000 it will be mirror-mapped into the 0x8000 byte of RAM starting at
// 0x8000. The handlers can be set to any of the 2 mirrors as base, based on this the code base
// address is calculated. This ensures that jsr instructions will result in the same opcode, as it
// is based on the code base address.
func (ar *Arch6502) calculateCodeBaseAddress(resetHandler uint16) {
	// Check if user specified a custom base address
	opts := ar.dis.Options()
	if opts.BaseAddress != 0 {
		// Use custom base address from options
		ar.dis.SetCodeBaseAddress(opts.BaseAddress)
		ar.dis.SetVectorsStartAddress(m6502.InterruptVectorStartAddress)
		return
	}

	cart := ar.dis.Cart()
	if cart == nil || len(cart.PRG) == 0 {
		// No cart data (binary mode), use default
		ar.dis.SetCodeBaseAddress(0x8000)
		ar.dis.SetVectorsStartAddress(m6502.InterruptVectorStartAddress)
		return
	}

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
