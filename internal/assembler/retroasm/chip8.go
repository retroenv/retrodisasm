package retroasm

import (
	"fmt"
	"strings"

	"github.com/retroenv/retrodisasm/internal/program"
)

// writeCHIP8 writes CHIP-8-format assembly.
func (w *FileWriter) writeCHIP8() error {
	if _, err := fmt.Fprintf(w.mainWriter, "; CHIP-8 ROM Disassembly\n"); err != nil {
		return fmt.Errorf("writing header comment: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "; Code base address: $%04X\n", w.app.CodeBaseAddress); err != nil {
		return fmt.Errorf("writing code base address: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "; Program starts at $200 in CHIP-8 memory space\n\n"); err != nil {
		return fmt.Errorf("writing memory space comment: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, ".org $200\n\n"); err != nil {
		return fmt.Errorf("writing org directive: %w", err)
	}

	for _, bank := range w.app.PRG {
		if err := w.writeCHIP8Bank(bank); err != nil {
			return fmt.Errorf("writing bank: %w", err)
		}
	}

	return nil
}

// writeCHIP8Bank writes a CHIP-8 program bank.
func (w *FileWriter) writeCHIP8Bank(bank *program.PRGBank) error {
	endIndex := w.getCHIP8EndIndex(bank)

	for i := range endIndex {
		offset := bank.Offsets[i]

		if len(offset.Data) == 0 && offset.Code == "" {
			continue
		}

		if err := w.writeCHIP8Label(offset); err != nil {
			return fmt.Errorf("writing label: %w", err)
		}

		if err := w.writeCHIP8Offset(offset, i); err != nil {
			return fmt.Errorf("writing offset: %w", err)
		}
	}

	return nil
}

// writeCHIP8Label writes a label if present in the offset.
func (w *FileWriter) writeCHIP8Label(offset program.Offset) error {
	if offset.Label != "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s:\n", offset.Label); err != nil {
			return fmt.Errorf("writing label %s: %w", offset.Label, err)
		}
	}
	return nil
}

// writeCHIP8Offset writes either code or data for an offset.
func (w *FileWriter) writeCHIP8Offset(offset program.Offset, index int) error {
	if offset.Code != "" {
		return w.writeCHIP8Code(offset)
	}
	if len(offset.Data) > 0 {
		return w.writeCHIP8Data(offset, index)
	}
	return nil
}

// writeCHIP8Code writes a CHIP-8 instruction.
func (w *FileWriter) writeCHIP8Code(offset program.Offset) error {
	line := "    " + offset.Code

	if offset.Comment == "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s\n", line); err != nil {
			return fmt.Errorf("writing code: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.mainWriter, "%-32s ; %s\n", line, offset.Comment); err != nil {
			return fmt.Errorf("writing code with comment: %w", err)
		}
	}

	return nil
}

// writeCHIP8Data writes raw data bytes.
func (w *FileWriter) writeCHIP8Data(offset program.Offset, _ int) error {
	var buf strings.Builder
	fmt.Fprintf(&buf, "    .byte $%02X", offset.Data[0])

	for j := 1; j < len(offset.Data); j++ {
		fmt.Fprintf(&buf, ", $%02X", offset.Data[j])
	}

	line := buf.String()

	if offset.Comment == "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s\n", line); err != nil {
			return fmt.Errorf("writing data: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.mainWriter, "%-32s ; %s\n", line, offset.Comment); err != nil {
			return fmt.Errorf("writing data with comment: %w", err)
		}
	}

	return nil
}

// getCHIP8EndIndex finds the last meaningful byte in a CHIP-8 ROM.
func (w *FileWriter) getCHIP8EndIndex(bank *program.PRGBank) int {
	if w.options.ZeroBytes {
		return len(bank.Offsets)
	}

	for i := len(bank.Offsets) - 1; i >= 0; i-- {
		offset := bank.Offsets[i]
		if len(offset.Data) > 0 {
			for _, b := range offset.Data {
				if b != 0 {
					return i + 1
				}
			}
		}
		if offset.Label != "" || offset.Code != "" {
			return i + 1
		}
	}

	return 0
}
