package nesasm

import (
	"fmt"
	"io"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/writer"
)

var headerByte = " .%s %d %-22s ; %s\n"

var vectors = " .dw %s, %s, %s\n\n"

// FileWriter writes the assembly file content.
type FileWriter struct {
	app           *program.Program
	options       options.Disassembler
	mainWriter    io.Writer
	newBankWriter assembler.NewBankWriter
	writer        *writer.Writer
}

type prgBankWrite struct {
	bank        *program.PRGBank
	isMultiBank bool
}

type customWrite func() error

type lineWrite string

// New creates a new file writer.
// nolint: ireturn
func New(app *program.Program, options options.Disassembler, mainWriter io.Writer, newBankWriter assembler.NewBankWriter) writer.AssemblerWriter {
	opts := writer.Options{
		DirectivePrefix: " ",
		OffsetComments:  options.OffsetComments,
	}
	return FileWriter{
		app:           app,
		options:       options,
		mainWriter:    mainWriter,
		newBankWriter: newBankWriter,
		writer:        writer.New(app, mainWriter, opts),
	}
}

// Write writes the assembly file content including header, footer, code and data.
func (f FileWriter) Write() error {
	var writes []any // nolint:prealloc

	if !f.options.CodeOnly {
		writes = []any{
			lineWrite("; NES ROM Disassembly"),
			customWrite(f.writer.WriteCommentHeader),
			customWrite(f.writeROMHeader),
		}
	}

	nextBank := addPrgBankSelectors(int(f.app.CodeBaseAddress), f.app.PRG)
	isMultiBank := len(f.app.PRG) > 1
	for _, bank := range f.app.PRG {
		writes = append(writes,
			prgBankWrite{bank: bank, isMultiBank: isMultiBank},
		)
	}

	// Only write global vectors for single-bank ROMs
	if !isMultiBank {
		writes = append(writes, customWrite(f.writeVectors))
	}
	writes = append(writes, customWrite(f.writeCHR(nextBank)))

	for _, write := range writes {
		switch t := write.(type) {
		case lineWrite:
			if _, err := fmt.Fprintln(f.mainWriter, t); err != nil {
				return fmt.Errorf("writing line: %w", err)
			}

		case customWrite:
			if err := t(); err != nil {
				return err
			}

		case prgBankWrite:
			if err := f.writePRGBank(t); err != nil {
				return err
			}
		}
	}

	return nil
}

// writePRGBank writes a single PRG bank including constants, variables, code, and vectors.
func (f FileWriter) writePRGBank(t prgBankWrite) error {
	if err := f.writeConstants(t.bank); err != nil {
		return err
	}
	if err := f.writeVariables(t.bank); err != nil {
		return err
	}
	if err := f.writeCode(t.bank); err != nil {
		return err
	}
	// For multi-bank ROMs, write vectors at end of each bank
	if t.isMultiBank && !f.options.CodeOnly {
		if err := f.writeBankVectors(t.bank); err != nil {
			return err
		}
	}
	return nil
}

// writeROMHeader writes the ROM header configuration to the output.
func (f FileWriter) writeROMHeader() error {
	if _, err := fmt.Fprintf(f.mainWriter, headerByte, "inesprg", f.app.PrgSize()/16384, " ", "Number of 16KB PRG-ROM banks"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := fmt.Fprintf(f.mainWriter, headerByte, "ineschr", len(f.app.CHR)/bankSize, " ", "Number of 8KB CHR-ROM banks"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := fmt.Fprintf(f.mainWriter, headerByte, "inesmap", f.app.Mapper, " ", "Mapper"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := fmt.Fprintf(f.mainWriter, headerByte, "inesmir", f.app.Mirror, " ", "Mirror mode"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	return nil
}

// writeConstants writes constant aliases to the output.
func (f FileWriter) writeConstants(bank *program.PRGBank) error {
	if err := f.writer.OutputAliasMap(bank.Constants); err != nil {
		return fmt.Errorf("writing constants output alias map: %w", err)
	}
	return nil
}

// writeVariables writes variable aliases to the output.
func (f FileWriter) writeVariables(bank *program.PRGBank) error {
	if err := f.writer.OutputAliasMap(bank.Variables); err != nil {
		return fmt.Errorf("writing variables output alias map: %w", err)
	}
	return nil
}

// writeCHR writes the CHR content to the output.
func (f FileWriter) writeCHR(nextBank int) func() error {
	return func() error {
		if _, err := fmt.Fprint(f.mainWriter, "\n .DATA"); err != nil {
			return fmt.Errorf("writing CHR bank: %w", err)
		}

		banks := chrBanks(nextBank, f.app.CHR)

		for _, bank := range banks {
			writeFunc := writeBankSelector(nextBank, -1)
			if err := writeFunc(f.mainWriter); err != nil {
				return fmt.Errorf("writing bank switch: %w", err)
			}

			if err := f.writer.BundleDataWrites(bank, nil); err != nil {
				return fmt.Errorf("writing CHR data: %w", err)
			}

			nextBank++
		}

		return nil
	}
}

// writeVectors writes the IRQ vectors for single-bank ROMs.
func (f FileWriter) writeVectors() error {
	if f.options.CodeOnly {
		return nil
	}

	if _, err := fmt.Fprintf(f.mainWriter, "\n .org $%04X\n", f.app.VectorsStartAddress); err != nil {
		return fmt.Errorf("writing segment: %w", err)
	}

	if _, err := fmt.Fprintf(f.mainWriter, vectors, f.app.Handlers.NMI, f.app.Handlers.Reset, f.app.Handlers.IRQ); err != nil {
		return fmt.Errorf("writing vectors: %w", err)
	}
	return nil
}

// writeBankVectors writes vectors at the end of a bank for multi-bank ROMs.
func (f FileWriter) writeBankVectors(bank *program.PRGBank) error {
	if _, err := fmt.Fprintf(f.mainWriter, "\n .org $%04X\n", f.app.VectorsStartAddress); err != nil {
		return fmt.Errorf("writing vector org: %w", err)
	}

	// Use labels if available, otherwise use raw addresses.
	nmi := f.writer.VectorLabel(bank, bank.Vectors[0])
	reset := f.writer.VectorLabel(bank, bank.Vectors[1])
	irq := f.writer.VectorLabel(bank, bank.Vectors[2])

	if _, err := fmt.Fprintf(f.mainWriter, vectors, nmi, reset, irq); err != nil {
		return fmt.Errorf("writing bank vectors: %w", err)
	}
	return nil
}

// writeCode writes the code to the output.
func (f FileWriter) writeCode(bank *program.PRGBank) error {
	endIndex := bank.LastNonZeroByte(f.options)
	if err := f.writer.ProcessPRG(bank, endIndex); err != nil {
		return fmt.Errorf("writing PRG: %w", err)
	}
	return nil
}
