// Package assembler defines the available assembler output formats.
package assembler

import (
	"fmt"
	"io"
	"slices"

	"github.com/retroenv/retrogolib/arch"
)

const (
	Asm6     = "asm6"
	Ca65     = "ca65"
	Nesasm   = "nesasm"
	Retroasm = "retroasm"
)

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
