package asm6

import (
	"fmt"
	"io"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/writer"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

var headerByte = ".db $%02x %-22s ; %s\n"

var vectors = ".dw %s, %s, %s\n\n"

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

type prgBankWrite struct {
	address  string
	bank     *program.PRGBank
	lastBank bool
}

type customWrite func() error

type lineWrite struct {
	line    string
	comment string
}

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
// nolint:funlen, cyclop
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
			lineWrite{line: "", comment: "NES ROM Disassembly"},
			customWrite(f.writer.WriteCommentHeader),
			lineWrite{line: ".db \"NES\", $1a", comment: "Magic string that always begins an iNES header"},
			headerByteWrite{value: byte(f.app.PrgSize() / 16384), comment: "Number of 16KB PRG-ROM banks"},
			headerByteWrite{value: byte(len(f.app.CHR) / 8192), comment: "Number of 8KB CHR-ROM banks"},
			headerByteWrite{value: control1, comment: "Control bits 1"},
			headerByteWrite{value: control2, comment: "Control bits 2"},
			headerByteWrite{value: ramByte, comment: "Number of 8KB PRG-RAM banks"},
			headerByteWrite{value: f.app.VideoFormat, comment: "Video format NTSC/PAL"},
			lineWrite{line: ".dsb 6", comment: "Padding to fill 16 BYTE iNES Header"},
		}
	}

	for i, bank := range f.app.PRG {
		lastBank := i == len(f.app.PRG)-1
		writes = append(writes,
			prgBankWrite{
				address:  fmt.Sprintf("$%04x", f.app.CodeBaseAddress),
				bank:     bank,
				lastBank: lastBank,
			},
		)
	}

	writes = append(writes,
		customWrite(f.writeCHR),
	)

	for _, write := range writes {
		switch t := write.(type) {
		case headerByteWrite:
			if _, err := fmt.Fprintf(f.mainWriter, headerByte, t.value, "", t.comment); err != nil {
				return fmt.Errorf("writing header: %w", err)
			}

		case lineWrite:
			if _, err := fmt.Fprintf(f.mainWriter, "%-30s ; %s\n", t.line, t.comment); err != nil {
				return fmt.Errorf("writing line: %w", err)
			}

		case customWrite:
			if err := t(); err != nil {
				return err
			}

		case prgBankWrite:
			if err := f.writeBank(t); err != nil {
				return err
			}
		}
	}

	return nil
}

func (f FileWriter) writeBank(w prgBankWrite) error {
	if err := f.writeSegment(w.address); err != nil {
		return err
	}
	if err := f.writeConstants(w.bank); err != nil {
		return err
	}
	if err := f.writeVariables(w.bank); err != nil {
		return err
	}
	if err := f.writeCode(w.bank); err != nil {
		return err
	}

	if w.lastBank {
		if err := f.writeVectors(f.app.Handlers.NMI, f.app.Handlers.Reset, f.app.Handlers.IRQ); err != nil {
			return err
		}
	} else {
		nmi := fmt.Sprintf("$%04X", w.bank.Vectors[0])
		reset := fmt.Sprintf("$%04X", w.bank.Vectors[1])
		irq := fmt.Sprintf("$%04X", w.bank.Vectors[2])
		if err := f.writeVectors(nmi, reset, irq); err != nil {
			return err
		}
	}
	return nil
}

// writeSegment writes a segment header to the output.
func (f FileWriter) writeSegment(address string) error {
	_, err := fmt.Fprintf(f.mainWriter, "\n.base %s\n\n", address)
	if err != nil {
		return fmt.Errorf("writing segment: %w", err)
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

	remaining := len(f.app.CHR) - lastNonZeroByte
	if remaining > 0 {
		if _, err := fmt.Fprintf(f.mainWriter, "\n.dsb %d\n", remaining); err != nil {
			return fmt.Errorf("writing CHR remainder: %w", err)
		}
	}

	return nil
}

// writeVectors writes the IRQ vectors.
func (f FileWriter) writeVectors(nmi, reset, irq string) error {
	if f.options.CodeOnly {
		return nil
	}

	addr := fmt.Sprintf("$%04X", f.app.VectorsStartAddress)

	_, err := fmt.Fprintf(f.mainWriter, "\n.pad %s\n", addr)
	if err != nil {
		return fmt.Errorf("writing padding: %w", err)
	}

	if err := f.writeSegment(addr); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f.mainWriter, vectors, nmi, reset, irq); err != nil {
		return fmt.Errorf("writing vectors: %w", err)
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
