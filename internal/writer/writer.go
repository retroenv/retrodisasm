// Package writer implements common assembly file writing functionality.
package writer

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/set"
)

const dataBytesPerLine = 16

// AssemblerWriter defines a shared interface used by the different assembler compatibility packages.
// Their constructors need to return this shared interface, having them return the actual type instead of
// the interface results in compiler errors for the constructor variable that they are assigned to.
type AssemblerWriter interface {
	Write() error
}

// Writer implements common assembly file writing functionality.
type Writer struct {
	app     *program.Program
	options Options
	writer  io.Writer

	emittedAliases map[string]uint16
}

// Options of the writer.
type Options struct {
	DirectivePrefix             string // nesasm requires a space before a directive
	LiteralCrossSegmentBranches bool
	OffsetComments              bool
}

// New creates a new writer.
func New(app *program.Program, writer io.Writer, options Options) *Writer {
	return &Writer{
		app:     app,
		options: options,
		writer:  writer,

		emittedAliases: map[string]uint16{},
	}
}

// ForOutput creates a writer that shares alias emission state with its parent.
func (w Writer) ForOutput(output io.Writer, options Options) *Writer {
	return &Writer{
		app:            w.app,
		options:        options,
		writer:         output,
		emittedAliases: w.emittedAliases,
	}
}

// ProcessPRG processes the PRG segment and writes all code offsets, labels and their comments.
func (w Writer) ProcessPRG(bank *program.PRGBank, endIndex int) error {
	var previousLineWasCode bool
	labels := set.New[string]()
	for _, offset := range bank.Offsets {
		if offset.Label != "" {
			labels.Add(offset.Label)
		}
	}

	for i := 0; i < endIndex; i++ {
		offset := bank.Offsets[i]

		if offset.WriteCallback != nil {
			if err := offset.WriteCallback(w.writer); err != nil {
				return fmt.Errorf("calling write callback: %w", err)
			}
		}

		if err := w.writeLabel(i, offset); err != nil {
			return err
		}

		// print an empty line in case of data after code and vice versa
		if i > 0 && offset.Label == "" && offset.IsType(program.CodeOffset|program.CodeAsData) != previousLineWasCode {
			if _, err := fmt.Fprintln(w.writer); err != nil {
				return fmt.Errorf("writing line: %w", err)
			}
		}
		previousLineWasCode = offset.IsType(program.CodeOffset | program.CodeAsData)

		adjustment, err := w.writeOffset(bank, i, endIndex, offset, labels)
		if err != nil {
			return err
		}
		i += adjustment
	}
	return nil
}

// BundleDataWrites bundles writes of data bytes to print dataBytesPerLine bytes per line.
func (w Writer) BundleDataWrites(data []byte, lineWriter lineWriterFunc) error {
	remaining := len(data)
	for i := 0; remaining > 0; {
		toWrite := min(remaining, dataBytesPerLine)

		buf := &strings.Builder{}
		if _, err := fmt.Fprintf(buf, "%s.byte ", w.options.DirectivePrefix); err != nil {
			return fmt.Errorf("writing data prefix: %w", err)
		}

		for j := range toWrite {
			if _, err := fmt.Fprintf(buf, "$%02x, ", data[i+j]); err != nil {
				return fmt.Errorf("writing data byte: %w", err)
			}
		}

		line := strings.TrimRight(buf.String(), ", ")

		if lineWriter != nil {
			if err := lineWriter(line, toWrite); err != nil {
				return fmt.Errorf("writing data line using custom writer: %w", err)
			}
		} else {
			if _, err := fmt.Fprintf(w.writer, "%s\n", line); err != nil {
				return fmt.Errorf("writing data line: %w", err)
			}
		}

		i += toWrite
		remaining -= toWrite
	}

	return nil
}

// OutputAliasMap outputs an alias map, for constants or variables.
func (w Writer) OutputAliasMap(aliases map[string]uint16) error {
	if len(aliases) == 0 {
		return nil
	}

	// sort the aliases by name before outputting to avoid random map order
	toEmit := make([]string, 0, len(aliases))
	for name, address := range aliases {
		if previousAddress, ok := w.emittedAliases[name]; ok && previousAddress == address {
			continue
		}
		toEmit = append(toEmit, name)
	}
	if len(toEmit) == 0 {
		return nil
	}
	slices.Sort(toEmit)

	if _, err := fmt.Fprintln(w.writer); err != nil {
		return fmt.Errorf("writing line: %w", err)
	}

	for _, name := range toEmit {
		address := aliases[name]
		if _, err := fmt.Fprintf(w.writer, "%s = $%04X\n", name, address); err != nil {
			return fmt.Errorf("writing alias: %w", err)
		}
		w.emittedAliases[name] = address
	}

	if _, err := fmt.Fprintln(w.writer); err != nil {
		return fmt.Errorf("writing line: %w", err)
	}
	return nil
}

// WriteCommentHeader writes the CRC32 checksums and code base address as comments to the output.
func (w Writer) WriteCommentHeader() error {
	if _, err := fmt.Fprintf(w.writer, "; PRG CRC32 checksum: %08x\n", w.app.Checksums.PRG); err != nil {
		return fmt.Errorf("writing prg checksum: %w", err)
	}
	if _, err := fmt.Fprintf(w.writer, "; CHR CRC32 checksum: %08x\n", w.app.Checksums.CHR); err != nil {
		return fmt.Errorf("writing chr checksum: %w", err)
	}
	if _, err := fmt.Fprintf(w.writer, "; Overall CRC32 checksum: %08x\n", w.app.Checksums.Overall); err != nil {
		return fmt.Errorf("writing overall checksum: %w", err)
	}
	if _, err := fmt.Fprintf(w.writer, "; Code base address: $%04x\n\n", w.app.CodeBaseAddress); err != nil {
		return fmt.Errorf("writing code base address: %w", err)
	}
	return nil
}

func (w Writer) writeOffset(bank *program.PRGBank, index, endIndex int, offset program.Offset,
	labels set.Set[string]) (int, error) {

	if offset.IsType(program.CodeOffset) && len(offset.Data) == 0 {
		return 0, nil
	}
	if w.options.LiteralCrossSegmentBranches && isBranchToMissingLabel(offset, labels) {
		offset.Code = fmt.Sprintf("%s.byte $%02x, $%02x", w.options.DirectivePrefix, offset.Data[0], offset.Data[1])
		if err := w.writeCodeLine(offset); err != nil {
			return 0, fmt.Errorf("writing cross-segment branch: %w", err)
		}
		return 1, nil
	}
	if offset.IsType(program.FunctionReference) {
		if err := w.writeCodeLine(offset); err != nil {
			return 0, fmt.Errorf("writing function reference: %w", err)
		}
		return 1, nil
	}

	if offset.IsType(program.DataOffset) {
		count, err := w.bundlePRGDataWrites(bank, index, endIndex)
		if err != nil {
			return 0, err
		}
		if count > 0 {
			return count - 1, nil
		}
		return 0, err
	}

	if err := w.writeCodeLine(offset); err != nil {
		return 0, fmt.Errorf("writing code line: %w", err)
	}
	return len(offset.Data) - 1, nil
}

func (w Writer) writeLabel(index int, offset program.Offset) error {
	if offset.Label == "" {
		return nil
	}

	if index > 0 {
		if _, err := fmt.Fprintln(w.writer); err != nil {
			return fmt.Errorf("writing line: %w", err)
		}
	}

	if offset.LabelComment == "" {
		if _, err := fmt.Fprintf(w.writer, "%s:\n", offset.Label); err != nil {
			return fmt.Errorf("writing label: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.writer, "%-32s ; %s\n", offset.Label+":", offset.LabelComment); err != nil {
			return fmt.Errorf("writing label: %w", err)
		}
	}
	return nil
}

func (w Writer) writeCodeLine(offset program.Offset) error {
	if offset.Comment == "" {
		if _, err := fmt.Fprintf(w.writer, "  %s\n", offset.Code); err != nil {
			return fmt.Errorf("writing line: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(w.writer, "  %-30s ; %s\n", offset.Code, offset.Comment); err != nil {
			return fmt.Errorf("writing line: %w", err)
		}
	}
	return nil
}

// bundlePRGDataWrites parses PRG to create bundled writes of data bytes per line.
func (w Writer) bundlePRGDataWrites(bank *program.PRGBank, startIndex, endIndex int) (int, error) {
	data := getPrgData(bank, startIndex, endIndex)
	if len(data) == 0 {
		return 0, nil
	}

	currentIndex := startIndex
	lineWriter := func(line string, byteCount int) error {
		var err error

		offset := bank.Offsets[currentIndex]
		if w.options.OffsetComments && !offset.HasAddressComment {
			comment := fmt.Sprintf("$%04X", offset.Address)
			if offset.Comment == "" {
				offset.Comment = comment
			} else {
				offset.Comment = comment + "  " + offset.Comment
			}
		}

		if offset.Comment == "" {
			_, err = fmt.Fprintf(w.writer, "%s\n", line)
		} else {
			_, err = fmt.Fprintf(w.writer, "%-32s ; %s\n", line, offset.Comment)
		}
		if err != nil {
			return fmt.Errorf("writing prg line: %w", err)
		}

		currentIndex += byteCount
		return nil
	}

	if err := w.BundleDataWrites(data, lineWriter); err != nil {
		return 0, fmt.Errorf("writing PRG data: %w", err)
	}

	return len(data), nil
}

type lineWriterFunc func(line string, byteCount int) error

func isBranchToMissingLabel(offset program.Offset, labels set.Set[string]) bool {
	if len(offset.Data) != 2 || offset.Data[0]&0x1F != 0x10 {
		return false
	}
	fields := strings.Fields(offset.Code)
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "_") {
		return false
	}
	return !labels.Contains(fields[1])
}

func getPrgData(bank *program.PRGBank, startIndex, endIndex int) []byte {
	var data []byte

	for i := startIndex; i < endIndex; i++ {
		offset := bank.Offsets[i]

		// opcode bytes can be nil if data bytes have been combined for an unofficial nop
		if !offset.IsType(program.DataOffset) || len(offset.Data) == 0 {
			break
		}
		// stop at first label or code after start index
		if i > startIndex && (offset.IsType(program.CodeOffset|program.CodeAsData) || offset.Label != "") {
			break
		}
		// break at potential bank switch, do ignore callback on first iteration as it has been handled
		// by the caller already.
		if offset.WriteCallback != nil && i != startIndex {
			break
		}

		data = append(data, offset.Data...)
	}

	return data
}
