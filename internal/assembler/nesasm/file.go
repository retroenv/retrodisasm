package nesasm

import (
	"fmt"
	"io"
	"strings"

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

// New creates a new file writer.
// nolint: ireturn
func New(app *program.Program, options options.Disassembler, mainWriter io.Writer, newBankWriter assembler.NewBankWriter) writer.AssemblerWriter {
	opts := writer.Options{
		DirectivePrefix:             " ",
		LiteralCrossSegmentBranches: true,
		OffsetComments:              options.OffsetComments,
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
	prgBankNumber := 0
	for i, bank := range f.app.PRG {
		vectorBank := prgBankNumber + (len(bank.Offsets)-1)/bankSize
		writes = append(writes,
			prgBankWrite{bank: bank, isMultiBank: isMultiBank, firstBank: i == 0, vectorBank: vectorBank},
		)
		prgBankNumber += (len(bank.Offsets) + bankSize - 1) / bankSize
	}

	// Only write global vectors for single-bank ROMs
	if !isMultiBank {
		writes = append(writes, customWrite(f.writeVectors(nextBank-1)))
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
	bankWriteCloser, err := f.newBankWriter(t.bank.Name)
	if err != nil {
		return fmt.Errorf("creating bank writer: %w", err)
	}
	defer func() { _ = bankWriteCloser.Close() }()

	if f.options.SplitBanks {
		if t.firstBank {
			if _, err := fmt.Fprintln(f.mainWriter); err != nil {
				return fmt.Errorf("writing newline: %w", err)
			}
		}
		bankFile := bankFilename(f.options.OutputFilename, t.bank.Name)
		if _, err := fmt.Fprintf(f.mainWriter, " .include \"%s\"\n", bankFile); err != nil {
			return fmt.Errorf("writing include directive: %w", err)
		}
	}

	bankW := f.writer.ForOutput(bankWriteCloser, writer.Options{
		DirectivePrefix:             " ",
		LiteralCrossSegmentBranches: true,
		OffsetComments:              f.options.OffsetComments,
	})

	if err := bankW.OutputAliasMap(t.bank.Constants); err != nil {
		return fmt.Errorf("writing constants output alias map: %w", err)
	}
	if err := bankW.OutputAliasMap(t.bank.Variables); err != nil {
		return fmt.Errorf("writing variables output alias map: %w", err)
	}

	endIndex := t.bank.LastNonZeroByte(f.options)
	if err := bankW.ProcessPRG(t.bank, endIndex); err != nil {
		return fmt.Errorf("writing PRG: %w", err)
	}

	// For multi-bank ROMs, write vectors at end of each bank
	if t.isMultiBank && !f.options.CodeOnly {
		if err := f.writeBankVectorsTo(bankWriteCloser, t.bank, t.vectorBank); err != nil {
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
	if f.app.Battery != 0 {
		if _, err := fmt.Fprintf(f.mainWriter, headerByte, "inesbat", f.app.Battery, " ", "Battery-backed RAM"); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}
	}
	return nil
}

// writeCHR writes the CHR content to the output.
func (f FileWriter) writeCHR(nextBank int) func() error {
	return func() error {
		if _, err := fmt.Fprint(f.mainWriter, "\n .DATA"); err != nil {
			return fmt.Errorf("writing CHR bank: %w", err)
		}
		if f.options.CHRFilename != "" {
			writeFunc := writeBankSelector(nextBank, -1)
			if err := writeFunc(f.mainWriter); err != nil {
				return fmt.Errorf("writing bank switch: %w", err)
			}
			if err := assembler.WriteCHRInclude(f.mainWriter, f.options.CHRFilename, " "); err != nil {
				return fmt.Errorf("writing CHR include directive: %w", err)
			}
			return nil
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
func (f FileWriter) writeVectors(bankNumber int) func() error {
	return func() error {
		if f.options.CodeOnly {
			return nil
		}

		if err := writeBankSelector(bankNumber, -1)(f.mainWriter); err != nil {
			return fmt.Errorf("writing vector bank: %w", err)
		}
		if _, err := fmt.Fprintf(f.mainWriter, "\n .org $%04X\n", f.app.VectorsStartAddress); err != nil {
			return fmt.Errorf("writing segment: %w", err)
		}

		nmi, reset, irq := assembler.VectorReferences(f.app.PRG[0], f.app.Handlers)
		if _, err := fmt.Fprintf(f.mainWriter, vectors, nmi, reset, irq); err != nil {
			return fmt.Errorf("writing vectors: %w", err)
		}
		return nil
	}
}

// writeBankVectorsTo writes vectors at the end of a bank for multi-bank ROMs.
func (f FileWriter) writeBankVectorsTo(w io.Writer, bank *program.PRGBank, bankNumber int) error {
	// Trailing zeroes can omit the callback that would otherwise select the
	// final 8 KiB PRG bank before the vectors are written.
	if err := writeBankSelector(bankNumber, -1)(w); err != nil {
		return fmt.Errorf("writing vector bank: %w", err)
	}
	vectorsAddr := bank.BaseAddress + uint16(len(bank.Offsets)) - 6
	if _, err := fmt.Fprintf(w, "\n .org $%04X\n", vectorsAddr); err != nil {
		return fmt.Errorf("writing vector org: %w", err)
	}

	nmi := fmt.Sprintf("$%04X", bank.Vectors[0])
	reset := fmt.Sprintf("$%04X", bank.Vectors[1])
	irq := fmt.Sprintf("$%04X", bank.Vectors[2])

	if _, err := fmt.Fprintf(w, vectors, nmi, reset, irq); err != nil {
		return fmt.Errorf("writing bank vectors: %w", err)
	}
	return nil
}

type prgBankWrite struct {
	bank        *program.PRGBank
	isMultiBank bool
	firstBank   bool
	vectorBank  int
}

type customWrite func() error

type lineWrite string

// bankFilename derives the bank output filename from the main output filename
// and the bank name. Matches the logic in pipeline.generateBankFilename.
func bankFilename(outputFilename, bankName string) string {
	base := strings.TrimSuffix(outputFilename, ".asm")
	suffix := strings.TrimPrefix(strings.ToLower(bankName), "prg_")
	return base + "_" + suffix + ".asm"
}
