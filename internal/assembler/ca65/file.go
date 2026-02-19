package ca65

import (
	"fmt"
	"io"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/writer"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

var cpuSelector = `.setcpu "6502x"` // allow unofficial opcodes

var iNESHeader = `.byte "NES", $1a                 ; Magic string that always begins an iNES header`

var headerByte = ".byte $%02x %-22s ; %s\n"

var vectors = ".addr %s, %s, %s\n"

// FileWriter writes the assembly file content.
type FileWriter struct {
	app           *program.Program
	options       options.Disassembler
	mainWriter    io.Writer
	newBankWriter assembler.NewBankWriter
	writer        *writer.Writer
}

type headerByteWrite struct {
	value   byte
	comment string
}

type segmentWrite struct {
	name string
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
		OffsetComments: options.OffsetComments,
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
	control1, control2 := cartridge.ControlBytes(f.app.Battery, byte(f.app.Mirror), f.app.Mapper, len(f.app.Trainer) > 0)

	// iNES 2.0: mapper numbers > 0xFF require extended header encoding.
	ramByte := f.app.RAM
	if f.app.Mapper > 0xFF {
		control2 = (control2 &^ 0x0C) | 0x08
		ramByte = byte((f.app.Mapper >> 8) & 0x0F)
	}

	var writes []any // nolint:prealloc

	if !f.options.CodeOnly {
		writes = []any{
			lineWrite("; NES ROM Disassembly"),
			customWrite(f.writer.WriteCommentHeader),
			lineWrite(cpuSelector),
			segmentWrite{name: "HEADER"},
			lineWrite(iNESHeader),
			headerByteWrite{value: byte(f.app.PrgSize() / 16384), comment: "Number of 16KB PRG-ROM banks"},
			headerByteWrite{value: byte(len(f.app.CHR) / 8192), comment: "Number of 8KB CHR-ROM banks"},
			headerByteWrite{value: control1, comment: "Control bits 1"},
			headerByteWrite{value: control2, comment: "Control bits 2"},
			headerByteWrite{value: ramByte, comment: "Number of 8KB PRG-RAM banks"},
			headerByteWrite{value: f.app.VideoFormat, comment: "Video format NTSC/PAL"},
		}
	}

	isMultiBank := len(f.app.PRG) > 1
	for _, bank := range f.app.PRG {
		writes = append(writes,
			prgBankWrite{bank: bank, isMultiBank: isMultiBank},
		)
	}

	if !f.options.CodeOnly {
		writes = append(writes, customWrite(f.writeCHR))
		// Only use separate VECTORS segment for single-bank ROMs
		if !isMultiBank {
			writes = append(writes, segmentWrite{name: "VECTORS"})
		}
	}

	for _, write := range writes {
		if err := f.processWrite(write); err != nil {
			return err
		}
	}

	// For single-bank ROMs, write vectors in separate segment
	if !f.options.CodeOnly && len(f.app.PRG) == 1 {
		if _, err := fmt.Fprintf(f.mainWriter, vectors, f.app.Handlers.NMI, f.app.Handlers.Reset, f.app.Handlers.IRQ); err != nil {
			return fmt.Errorf("writing vectors: %w", err)
		}
	}
	return nil
}

// processWrite handles writing a single item to the output.
func (f FileWriter) processWrite(write any) error {
	switch t := write.(type) {
	case headerByteWrite:
		if _, err := fmt.Fprintf(f.mainWriter, headerByte, t.value, "", t.comment); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}

	case segmentWrite:
		if err := f.writeSegment(t.name); err != nil {
			return err
		}

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

// writeSegment writes a segment header to the output.
func (f FileWriter) writeSegment(name string) error {
	if name != "HEADER" {
		if _, err := fmt.Fprintln(f.mainWriter); err != nil {
			return fmt.Errorf("writing segment: %w", err)
		}
	}

	_, err := fmt.Fprintf(f.mainWriter, ".segment \"%s\"\n\n", name)
	if err != nil {
		return fmt.Errorf("writing segment footer: %w", err)
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
func (f FileWriter) writeCHR() error {
	if err := f.writeSegment("TILES"); err != nil {
		return err
	}

	if f.options.ZeroBytes {
		if err := f.writer.BundleDataWrites(f.app.CHR, nil); err != nil {
			return fmt.Errorf("writing CHR data: %w", err)
		}
		return nil
	}

	lastNonZeroByte := f.app.CHR.LastNonZeroByte()
	if err := f.writer.BundleDataWrites(f.app.CHR[:lastNonZeroByte], nil); err != nil {
		return fmt.Errorf("writing CHR data: %w", err)
	}
	return nil
}

// writeCode writes the code to the output.
func (f FileWriter) writeCode(bank *program.PRGBank) error {
	if !f.options.CodeOnly {
		if err := f.writeSegment(bank.Name); err != nil {
			return err
		}
	}

	endIndex := bank.LastNonZeroByte(f.options)
	if err := f.writer.ProcessPRG(bank, endIndex); err != nil {
		return fmt.Errorf("writing PRG: %w", err)
	}
	return nil
}

// writeBankVectors writes vectors at the end of a bank for multi-bank ROMs.
// Each bank has its own NMI, Reset, and IRQ vectors stored in the last 6 bytes.
func (f FileWriter) writeBankVectors(bank *program.PRGBank) error {
	// Vectors are: [0]=NMI, [1]=Reset, [2]=IRQ
	// Output as .addr directives using the addresses stored in the bank
	nmi := fmt.Sprintf("$%04X", bank.Vectors[0])
	reset := fmt.Sprintf("$%04X", bank.Vectors[1])
	irq := fmt.Sprintf("$%04X", bank.Vectors[2])

	if _, err := fmt.Fprintf(f.mainWriter, "\n.addr %s, %s, %s\n", nmi, reset, irq); err != nil {
		return fmt.Errorf("writing bank vectors: %w", err)
	}
	return nil
}
