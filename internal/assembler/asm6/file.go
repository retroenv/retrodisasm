package asm6

import (
	"fmt"
	"io"
	"strings"

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

// New creates a new file writer.
// nolint: ireturn
func New(app *program.Program, options options.Disassembler, mainWriter io.Writer, newBankWriter assembler.NewBankWriter) writer.AssemblerWriter {
	opts := writer.Options{
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
				address:   fmt.Sprintf("$%04x", bank.BaseAddress),
				bank:      bank,
				lastBank:  lastBank,
				firstBank: i == 0,
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
	bankWriteCloser, err := f.newBankWriter(w.bank.Name)
	if err != nil {
		return fmt.Errorf("creating bank writer: %w", err)
	}
	defer func() { _ = bankWriteCloser.Close() }()

	if f.options.SplitBanks {
		if w.firstBank {
			if _, err := fmt.Fprintln(f.mainWriter); err != nil {
				return fmt.Errorf("writing newline: %w", err)
			}
		}
		bankFile := bankFilename(f.options.OutputFilename, w.bank.Name)
		if _, err := fmt.Fprintf(f.mainWriter, ".include \"%s\"\n", bankFile); err != nil {
			return fmt.Errorf("writing include directive: %w", err)
		}
	}

	bankW := f.writer.ForOutput(bankWriteCloser, writer.Options{
		LiteralCrossSegmentBranches: true,
		OffsetComments:              f.options.OffsetComments,
	})

	if _, err := fmt.Fprintf(bankWriteCloser, "\n.base %s\n\n", w.address); err != nil {
		return fmt.Errorf("writing segment: %w", err)
	}

	if err := bankW.OutputAliasMap(w.bank.Constants); err != nil {
		return fmt.Errorf("writing constants output alias map: %w", err)
	}
	if err := bankW.OutputAliasMap(w.bank.Variables); err != nil {
		return fmt.Errorf("writing variables output alias map: %w", err)
	}

	endIndex := w.bank.LastNonZeroByte(f.options)
	if err := bankW.ProcessPRG(w.bank, endIndex); err != nil {
		return fmt.Errorf("writing PRG: %w", err)
	}

	vectorsAddr := w.bank.BaseAddress + uint16(len(w.bank.Offsets)) - 6

	if w.lastBank {
		nmi, reset, irq := assembler.VectorReferences(w.bank, f.app.Handlers)
		if err := f.writeVectorsTo(bankWriteCloser, vectorsAddr, nmi, reset, irq); err != nil {
			return err
		}
	} else {
		nmi := fmt.Sprintf("$%04X", w.bank.Vectors[0])
		reset := fmt.Sprintf("$%04X", w.bank.Vectors[1])
		irq := fmt.Sprintf("$%04X", w.bank.Vectors[2])
		if err := f.writeVectorsTo(bankWriteCloser, vectorsAddr, nmi, reset, irq); err != nil {
			return err
		}
	}
	return nil
}

// writeCHR writes the CHR content to the output.
func (f FileWriter) writeCHR() error {
	if f.options.CHRFilename != "" {
		// CHR labels use PPU address space even though the bytes follow PRG-ROM.
		if _, err := fmt.Fprint(f.mainWriter, "\n.base $0000\n\n"); err != nil {
			return fmt.Errorf("writing CHR base: %w", err)
		}
		if err := assembler.WriteCHRInclude(f.mainWriter, f.options.CHRFilename, ""); err != nil {
			return fmt.Errorf("writing CHR include directive: %w", err)
		}
		return nil
	}

	if f.options.ZeroBytes {
		if err := f.writer.BundleAddressedDataWrites(f.app.CHR, 0); err != nil {
			return fmt.Errorf("writing CHR data: %w", err)
		}
		return nil
	}

	lastNonZeroByte := f.app.CHR.LastNonZeroByte()
	if err := f.writer.BundleAddressedDataWrites(f.app.CHR[:lastNonZeroByte], 0); err != nil {
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

// writeVectorsTo writes the IRQ vectors to the specified writer.
func (f FileWriter) writeVectorsTo(w io.Writer, vectorsAddr uint16, nmi, reset, irq string) error {
	if f.options.CodeOnly {
		return nil
	}

	addr := fmt.Sprintf("$%04X", vectorsAddr)

	_, err := fmt.Fprintf(w, "\n.pad %s\n", addr)
	if err != nil {
		return fmt.Errorf("writing padding: %w", err)
	}

	if _, err := fmt.Fprintf(w, "\n.base %s\n\n", addr); err != nil {
		return fmt.Errorf("writing segment: %w", err)
	}
	if _, err := fmt.Fprintf(w, vectors, nmi, reset, irq); err != nil {
		return fmt.Errorf("writing vectors: %w", err)
	}
	return nil
}

type headerByteWrite struct {
	value   byte
	comment string
}

type prgBankWrite struct {
	address   string
	bank      *program.PRGBank
	lastBank  bool
	firstBank bool
}

type customWrite func() error

type lineWrite struct {
	line    string
	comment string
}

// bankFilename derives the bank output filename from the main output filename
// and the bank name. Matches the logic in pipeline.generateBankFilename.
func bankFilename(outputFilename, bankName string) string {
	base := strings.TrimSuffix(outputFilename, ".asm")
	suffix := strings.TrimPrefix(strings.ToLower(bankName), "prg_")
	return base + "_" + suffix + ".asm"
}
