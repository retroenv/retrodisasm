// Package retroasm provides retroasm assembler file writer implementation.
package retroasm

import (
	"fmt"
	"io"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/writer"
	"github.com/retroenv/retrogolib/arch"
)

// FileWriter writes retroasm assembly files.
type FileWriter struct {
	app           *program.Program
	options       options.Disassembler
	mainWriter    io.Writer
	newBankWriter assembler.NewBankWriter
	writer        *writer.Writer
}

// New creates a new retroasm file writer.
func New(app *program.Program, options options.Disassembler, mainWriter io.Writer, newBankWriter assembler.NewBankWriter) writer.AssemblerWriter {
	opts := writer.Options{
		OffsetComments: options.OffsetComments,
	}
	return &FileWriter{
		app:           app,
		options:       options,
		mainWriter:    mainWriter,
		newBankWriter: newBankWriter,
		writer:        writer.New(app, mainWriter, opts),
	}
}

// Write writes the retroasm assembly file.
func (w *FileWriter) Write() error {
	switch w.options.System {
	case arch.NES:
		return w.writeNES()
	case arch.CHIP8System:
		return w.writeCHIP8()
	case arch.DOS:
		return w.writeDOS()
	default:
		return fmt.Errorf("unsupported system for retroasm: %s", w.options.System)
	}
}
