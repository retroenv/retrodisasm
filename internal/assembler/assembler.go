// Package assembler defines the available assembler output formats.
package assembler

import (
	"fmt"
	"io"
	"slices"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch"
)

const (
	Asm6     = "asm6"
	Ca65     = "ca65"
	Nesasm   = "nesasm"
	Retroasm = "retroasm"
)

const chrLabel = "_chr_0000"

// SystemAssemblers maps each system to its supported assemblers.
var SystemAssemblers = map[arch.System][]string{
	arch.NES:         {Asm6, Ca65, Nesasm, Retroasm},
	arch.CHIP8System: {Retroasm},
}

// NewBankWriter is a callback that creates a new file for a bank of ROMs
// that have multiple PRG banks.
type NewBankWriter func(baseName string) (io.WriteCloser, error)

// ValidateSystemAssembler checks if the assembler is supported for the given system.
func ValidateSystemAssembler(system arch.System, assembler string) error {
	supported, ok := SystemAssemblers[system]
	if !ok {
		return fmt.Errorf("unknown system: %s", system)
	}

	if !slices.Contains(supported, assembler) {
		return fmt.Errorf("assembler '%s' is not supported for system '%s'. Valid options: %v",
			assembler, system, supported)
	}

	return nil
}

// WriteCHRInclude writes the shared CHR-ROM label and binary include directive.
func WriteCHRInclude(w io.Writer, filename, directivePrefix string) error {
	if _, err := fmt.Fprintf(w, "%s:\n%s.incbin %q\n", chrLabel, directivePrefix, filename); err != nil {
		return fmt.Errorf("writing CHR include: %w", err)
	}
	return nil
}

// VectorReferences returns handler labels only when their definitions are
// emitted at the vector addresses. Invalid vectors remain literal to round-trip.
func VectorReferences(bank *program.PRGBank, handlers program.Handlers) (string, string, string) {
	vectorStart := len(bank.Offsets) - 6
	resolve := func(name string, address uint16) string {
		index := int(address) - int(bank.BaseAddress)
		if name == "" || index < 0 || index >= vectorStart {
			return fmt.Sprintf("$%04X", address)
		}

		offset := bank.Offsets[index]
		// Operand offsets can retain handler names, but the owning instruction
		// advances the writer past them without emitting their labels.
		if offset.Label != name || len(offset.Data) == 0 {
			return fmt.Sprintf("$%04X", address)
		}

		return name
	}

	return resolve(handlers.NMI, bank.Vectors[0]),
		resolve(handlers.Reset, bank.Vectors[1]),
		resolve(handlers.IRQ, bank.Vectors[2])
}
