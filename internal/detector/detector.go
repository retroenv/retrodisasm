// Package detector handles system architecture detection.
package detector

import (
	"path/filepath"
	"strings"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/log"
)

// Detector handles system architecture detection from file extensions and options.
type Detector struct {
	logger *log.Logger
}

// New creates a new system detector.
func New(logger *log.Logger) *Detector {
	return &Detector{
		logger: logger,
	}
}

// Detect determines the system architecture from options or file auto-detection.
// It first checks if a system is explicitly specified in options, otherwise
// attempts to detect the system from the input filename extension.
func (d *Detector) Detect(opts options.Program) arch.System {
	system, _ := arch.SystemFromString(opts.System)
	if system == "" {
		system = d.detectFromFile(opts.Input)
		d.logger.Debug("Auto-detected system",
			log.Stringer("system", system),
			log.String("file", opts.Input))
	}
	return system
}

// detectFromFile determines the system type based on file extension.
func (d *Detector) detectFromFile(filename string) arch.System {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".ch8", ".rom":
		// ROM files could be CHIP-8, default to CHIP-8 for .rom extension
		return arch.CHIP8System
	case ".nes":
		return arch.NES
	case ".com":
		// DOS .com executable
		return arch.DOS
	default:
		// Default to M6502/NES for unknown extensions
		return arch.NES
	}
}
