package retroasm

import (
	"fmt"
	"io"
	"strings"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/writer"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

const (
	headerByte = ".byte $%02x %-22s ; %s\n"
	vectors    = ".word %s, %s, %s\n\n"
)

// writeNES writes NES-format assembly.
func (w *FileWriter) writeNES() error {
	if !w.options.CodeOnly {
		if err := w.writeHeader(); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}
	}

	for i, bank := range w.app.PRG {
		lastBank := i == len(w.app.PRG)-1
		if err := w.writeBank(bank, i == 0, lastBank); err != nil {
			return fmt.Errorf("writing bank %d: %w", i, err)
		}
	}

	if !w.options.CodeOnly {
		if err := w.writeCHR(); err != nil {
			return fmt.Errorf("writing CHR: %w", err)
		}
	}

	return nil
}

// writeHeader writes the iNES header.
func (w *FileWriter) writeHeader() error {
	if _, err := fmt.Fprintf(w.mainWriter, "; NES ROM Disassembly\n"); err != nil {
		return fmt.Errorf("writing header comment: %w", err)
	}

	if err := w.writer.WriteCommentHeader(); err != nil {
		return fmt.Errorf("writing comment header: %w", err)
	}

	control1, control2 := cartridge.ControlBytes(w.app.Battery, byte(w.app.Mirror), w.app.Mapper, len(w.app.Trainer) > 0)

	// iNES 2.0: mapper numbers > 0xFF require extended header encoding.
	ramByte := w.app.RAM
	if w.app.Mapper > 0xFF {
		control2 = (control2 &^ 0x0C) | 0x08
		ramByte = byte((w.app.Mapper >> 8) & 0x0F)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "%-30s ; %s\n", ".byte \"NES\", $1a", "Magic string that always begins an iNES header"); err != nil {
		return fmt.Errorf("writing magic string: %w", err)
	}

	headerWrites := []struct {
		value   byte
		comment string
	}{
		{byte(w.app.PrgSize() / 16384), "Number of 16KB PRG-ROM banks"},
		{byte(len(w.app.CHR) / 8192), "Number of 8KB CHR-ROM banks"},
		{control1, "Control bits 1"},
		{control2, "Control bits 2"},
		{ramByte, "Number of 8KB PRG-RAM banks"},
		{w.app.VideoFormat, "Video format NTSC/PAL"},
	}

	for _, hw := range headerWrites {
		if _, err := fmt.Fprintf(w.mainWriter, headerByte, hw.value, "", hw.comment); err != nil {
			return fmt.Errorf("writing header byte: %w", err)
		}
	}

	if _, err := fmt.Fprintf(w.mainWriter, "%-30s ; %s\n", ".dsb 6", "Padding to fill 16 BYTE iNES Header"); err != nil {
		return fmt.Errorf("writing padding: %w", err)
	}

	return nil
}

// writeBank writes a PRG bank.
func (w *FileWriter) writeBank(bank *program.PRGBank, firstBank, lastBank bool) error {
	bankWriteCloser, err := w.newBankWriter(bank.Name)
	if err != nil {
		return fmt.Errorf("creating bank writer: %w", err)
	}
	defer func() { _ = bankWriteCloser.Close() }()

	if w.options.SplitBanks {
		if firstBank {
			if _, err := fmt.Fprintln(w.mainWriter); err != nil {
				return fmt.Errorf("writing newline: %w", err)
			}
		}
		bankFile := bankFilename(w.options.OutputFilename, bank.Name)
		if _, err := fmt.Fprintf(w.mainWriter, ".include \"%s\"\n", bankFile); err != nil {
			return fmt.Errorf("writing include directive: %w", err)
		}
	}

	bankW := writer.New(w.app, bankWriteCloser, writer.Options{
		OffsetComments: w.options.OffsetComments,
	})

	if _, err := fmt.Fprintf(bankWriteCloser, "\n.org $%04x\n\n", bank.BaseAddress); err != nil {
		return fmt.Errorf("writing org directive: %w", err)
	}

	if err := bankW.OutputAliasMap(bank.Constants); err != nil {
		return fmt.Errorf("writing constants: %w", err)
	}

	if err := bankW.OutputAliasMap(bank.Variables); err != nil {
		return fmt.Errorf("writing variables: %w", err)
	}

	endIndex := bank.LastNonZeroByte(w.options)
	if err := bankW.ProcessPRG(bank, endIndex); err != nil {
		return fmt.Errorf("writing PRG: %w", err)
	}

	if !w.options.CodeOnly {
		vectorsAddr := bank.BaseAddress + uint16(len(bank.Offsets)) - 6
		if lastBank {
			if err := writeVectorsTo(bankWriteCloser, vectorsAddr, w.app.Handlers.NMI, w.app.Handlers.Reset, w.app.Handlers.IRQ); err != nil {
				return fmt.Errorf("writing vectors: %w", err)
			}
		} else {
			nmi := fmt.Sprintf("$%04X", bank.Vectors[0])
			reset := fmt.Sprintf("$%04X", bank.Vectors[1])
			irq := fmt.Sprintf("$%04X", bank.Vectors[2])
			if err := writeVectorsTo(bankWriteCloser, vectorsAddr, nmi, reset, irq); err != nil {
				return fmt.Errorf("writing vectors: %w", err)
			}
		}
	}

	return nil
}

// writeCHR writes the CHR content.
func (w *FileWriter) writeCHR() error {
	if len(w.app.CHR) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w.mainWriter); err != nil {
		return fmt.Errorf("writing newline: %w", err)
	}
	if w.options.CHRFilename != "" {
		// CHR labels use PPU address space even though the bytes follow PRG-ROM.
		if _, err := fmt.Fprint(w.mainWriter, ".org $0000\n\n"); err != nil {
			return fmt.Errorf("writing CHR origin: %w", err)
		}
		if err := assembler.WriteCHRInclude(w.mainWriter, w.options.CHRFilename, ""); err != nil {
			return fmt.Errorf("writing CHR include directive: %w", err)
		}
		return nil
	}

	if w.options.ZeroBytes {
		if err := w.writer.BundleDataWrites(w.app.CHR, nil); err != nil {
			return fmt.Errorf("writing CHR data: %w", err)
		}
		return nil
	}

	lastNonZeroByte := w.app.CHR.LastNonZeroByte()
	if err := w.writer.BundleDataWrites(w.app.CHR[:lastNonZeroByte], nil); err != nil {
		return fmt.Errorf("writing CHR data: %w", err)
	}

	remaining := len(w.app.CHR) - lastNonZeroByte
	if remaining > 0 {
		if _, err := fmt.Fprintf(w.mainWriter, "\n.dsb %d\n", remaining); err != nil {
			return fmt.Errorf("writing CHR remainder: %w", err)
		}
	}

	return nil
}

// writeVectorsTo writes the IRQ vectors to the specified writer.
func writeVectorsTo(w io.Writer, vectorsAddr uint16, nmi, reset, irq string) error {
	addr := fmt.Sprintf("$%04X", vectorsAddr)

	if _, err := fmt.Fprintf(w, "\n.org %s\n\n", addr); err != nil {
		return fmt.Errorf("writing vector org: %w", err)
	}

	if _, err := fmt.Fprintf(w, vectors, nmi, reset, irq); err != nil {
		return fmt.Errorf("writing vectors: %w", err)
	}

	return nil
}

// bankFilename derives the bank output filename from the main output filename
// and the bank name. Matches the logic in pipeline.generateBankFilename.
func bankFilename(outputFilename, bankName string) string {
	base := strings.TrimSuffix(outputFilename, ".asm")
	suffix := strings.TrimPrefix(strings.ToLower(bankName), "prg_")
	return base + "_" + suffix + ".asm"
}
