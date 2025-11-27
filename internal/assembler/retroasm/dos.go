package retroasm

import (
	"fmt"
	"strings"

	"github.com/retroenv/retrodisasm/internal/program"
)

// writeDOS writes DOS .com file NASM-compatible assembly.
func (w *FileWriter) writeDOS() error {
	if _, err := fmt.Fprintf(w.mainWriter, "; DOS .COM File Disassembly\n"); err != nil {
		return fmt.Errorf("writing header comment: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "; Assemble with: nasm -f bin -o output.com input.asm\n\n"); err != nil {
		return fmt.Errorf("writing assemble comment: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "[bits 16]\n"); err != nil {
		return fmt.Errorf("writing bits directive: %w", err)
	}

	if _, err := fmt.Fprintf(w.mainWriter, "[org 0x0100]\n\n"); err != nil {
		return fmt.Errorf("writing org directive: %w", err)
	}

	for _, bank := range w.app.PRG {
		if err := w.writeDOSBank(bank); err != nil {
			return fmt.Errorf("writing bank: %w", err)
		}
	}

	return nil
}

// writeDOSBank writes a DOS program bank.
func (w *FileWriter) writeDOSBank(bank *program.PRGBank) error {
	endIndex := w.getDOSEndIndex(bank)

	for i := range endIndex {
		offset := bank.Offsets[i]

		if len(offset.Data) == 0 && offset.Code == "" {
			continue
		}

		if err := w.writeDOSLabel(offset); err != nil {
			return fmt.Errorf("writing label: %w", err)
		}

		if err := w.writeDOSOffset(offset, i); err != nil {
			return fmt.Errorf("writing offset: %w", err)
		}
	}

	return nil
}

// writeDOSLabel writes a label if present in the offset.
func (w *FileWriter) writeDOSLabel(offset program.Offset) error {
	if offset.Label != "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s:\n", offset.Label); err != nil {
			return fmt.Errorf("writing label %s: %w", offset.Label, err)
		}
	}
	return nil
}

// writeDOSOffset writes either code or data for an offset.
func (w *FileWriter) writeDOSOffset(offset program.Offset, index int) error {
	if offset.Code != "" {
		return w.writeDOSCode(offset)
	}
	if len(offset.Data) > 0 {
		return w.writeDOSData(offset, index)
	}
	return nil
}

// writeDOSCode writes an x86 instruction.
func (w *FileWriter) writeDOSCode(offset program.Offset) error {
	line := "    " + offset.Code

	if offset.Comment == "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s\n", line); err != nil {
			return fmt.Errorf("writing code: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.mainWriter, "%-40s ; %s\n", line, offset.Comment); err != nil {
			return fmt.Errorf("writing code with comment: %w", err)
		}
	}

	return nil
}

// writeDOSData writes raw data bytes in NASM format.
func (w *FileWriter) writeDOSData(offset program.Offset, _ int) error {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("    db 0x%02x", offset.Data[0]))

	for j := 1; j < len(offset.Data); j++ {
		buf.WriteString(fmt.Sprintf(", 0x%02x", offset.Data[j]))
	}

	line := buf.String()

	if offset.Comment == "" {
		if _, err := fmt.Fprintf(w.mainWriter, "%s\n", line); err != nil {
			return fmt.Errorf("writing data: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.mainWriter, "%-40s ; %s\n", line, offset.Comment); err != nil {
			return fmt.Errorf("writing data with comment: %w", err)
		}
	}

	return nil
}

// getDOSEndIndex finds the last meaningful byte in a DOS .com file.
func (w *FileWriter) getDOSEndIndex(bank *program.PRGBank) int {
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
