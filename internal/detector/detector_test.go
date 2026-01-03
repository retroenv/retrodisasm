package detector

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

//nolint:funlen
func TestDetect(t *testing.T) {
	logger := log.NewTestLogger(t)
	d := New(logger)

	tests := []struct {
		name       string
		systemOpt  string
		inputFile  string
		wantSystem arch.System
	}{
		{
			name:       "explicit NES system option",
			systemOpt:  "nes",
			inputFile:  "game.bin",
			wantSystem: arch.NES,
		},
		{
			name:       "explicit CHIP8 system option",
			systemOpt:  "chip8",
			inputFile:  "game.bin",
			wantSystem: arch.CHIP8System,
		},
		{
			name:       "explicit DOS system option",
			systemOpt:  "dos",
			inputFile:  "game.bin",
			wantSystem: arch.DOS,
		},
		{
			name:       "detect from .nes extension",
			systemOpt:  "",
			inputFile:  "game.nes",
			wantSystem: arch.NES,
		},
		{
			name:       "detect from .ch8 extension",
			systemOpt:  "",
			inputFile:  "game.ch8",
			wantSystem: arch.CHIP8System,
		},
		{
			name:       "detect from .rom extension",
			systemOpt:  "",
			inputFile:  "game.rom",
			wantSystem: arch.CHIP8System,
		},
		{
			name:       "detect from .com extension",
			systemOpt:  "",
			inputFile:  "game.com",
			wantSystem: arch.DOS,
		},
		{
			name:       "unknown extension defaults to NES",
			systemOpt:  "",
			inputFile:  "game.bin",
			wantSystem: arch.NES,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := options.Program{
				Parameters: options.Parameters{Input: tt.inputFile},
				Flags:      options.Flags{System: tt.systemOpt},
			}

			got := d.Detect(opts)
			assert.Equal(t, tt.wantSystem, got)
		})
	}
}

func TestDetectFromFile(t *testing.T) {
	logger := log.NewTestLogger(t)
	d := New(logger)

	tests := []struct {
		name       string
		filename   string
		wantSystem arch.System
	}{
		{
			name:       ".nes extension",
			filename:   "super_mario.nes",
			wantSystem: arch.NES,
		},
		{
			name:       ".NES extension (uppercase)",
			filename:   "ZELDA.NES",
			wantSystem: arch.NES,
		},
		{
			name:       ".ch8 extension",
			filename:   "pong.ch8",
			wantSystem: arch.CHIP8System,
		},
		{
			name:       ".rom extension",
			filename:   "game.rom",
			wantSystem: arch.CHIP8System,
		},
		{
			name:       ".com extension",
			filename:   "program.com",
			wantSystem: arch.DOS,
		},
		{
			name:       ".COM extension (uppercase)",
			filename:   "PROGRAM.COM",
			wantSystem: arch.DOS,
		},
		{
			name:       "no extension",
			filename:   "game",
			wantSystem: arch.NES,
		},
		{
			name:       ".bin extension",
			filename:   "game.bin",
			wantSystem: arch.NES,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.detectFromFile(tt.filename)
			assert.Equal(t, tt.wantSystem, got)
		})
	}
}
