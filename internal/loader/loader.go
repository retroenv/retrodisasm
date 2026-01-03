// Package loader handles cartridge file loading operations.
package loader

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// Loader handles loading cartridge files from disk.
type Loader struct{}

// New creates a new cartridge loader.
func New() *Loader {
	return &Loader{}
}

// Load loads and parses a cartridge file based on the system type and options.
// It supports both NES ROM format and raw binary mode for systems like CHIP-8.
// Returns the cartridge and an optional Code/Data Log reader if specified.
func (l *Loader) Load(opts options.Program, system arch.System) (*cartridge.Cartridge, io.ReadCloser, error) {
	file, err := os.Open(opts.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("opening file %s: %w", opts.Input, err)
	}
	defer func() { _ = file.Close() }()

	cart, err := l.loadFromReader(file, opts.Binary, system)
	if err != nil {
		return nil, nil, err
	}

	// Open Code/Data Log file if specified
	var cdlReader io.ReadCloser
	if opts.CodeDataLog != "" {
		cdlReader, err = os.Open(opts.CodeDataLog)
		if err != nil {
			return nil, nil, fmt.Errorf("opening CDL file %s: %w", opts.CodeDataLog, err)
		}
	}

	return cart, cdlReader, nil
}

// LoadFromBytes loads and parses a cartridge from a byte slice.
// This is primarily used for testing but can be useful for programmatic usage.
// The binary parameter controls whether to treat the data as raw binary (true) or iNES format (false).
func (l *Loader) LoadFromBytes(data []byte, binary bool, system arch.System) (*cartridge.Cartridge, error) {
	reader := bytes.NewReader(data)
	return l.loadFromReader(reader, binary, system)
}

// loadFromReader loads and parses a cartridge from an io.Reader.
// This shared method handles the logic for choosing between binary/buffer mode and iNES format.
func (l *Loader) loadFromReader(reader io.Reader, binary bool, system arch.System) (*cartridge.Cartridge, error) {
	var cart *cartridge.Cartridge
	var err error

	// DOS .com files need raw loading without padding to preserve file size
	switch {
	case system == arch.DOS:
		cart, err = l.loadRawBinary(reader)
	case binary || system == arch.CHIP8System:
		// Handle CHIP-8 and binary files as raw buffer data (with 16KB padding)
		cart, err = cartridge.LoadBuffer(reader)
	default:
		cart, err = cartridge.LoadFile(reader)
	}
	if err != nil {
		return nil, fmt.Errorf("loading cartridge: %w", err)
	}

	return cart, nil
}

// loadRawBinary loads a raw binary file without padding.
// This preserves the exact file size which is important for DOS .com files
// where the PRG length is used for code bounds checking.
func (l *Loader) loadRawBinary(reader io.Reader) (*cartridge.Cartridge, error) {
	prg, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading binary: %w", err)
	}

	return &cartridge.Cartridge{
		PRG:     prg,
		CHR:     make([]byte, 0),
		RAM:     0,
		Trainer: nil,
		Mapper:  0,
	}, nil
}
