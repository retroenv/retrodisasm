package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestNew(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)

	assert.NotNil(t, p)
	assert.NotNil(t, p.logger)
	assert.NotNil(t, p.detector)
	assert.NotNil(t, p.loader)
}

//nolint:funlen // test functions can be long
func TestCreateDisassemblerForSystem(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)

	// Create minimal valid NES ROM in memory
	nesData := buildMinimalNESROM(1, 0)

	cart, err := p.loader.LoadFromBytes(nesData, false, arch.NES)
	assert.NoError(t, err)

	disasmOpts := options.Disassembler{
		System: arch.NES,
	}

	tests := []struct {
		name       string
		system     arch.System
		wantErr    bool
		errContain string
	}{
		{
			name:    "create NES disassembler",
			system:  arch.NES,
			wantErr: false,
		},
		{
			name:    "create CHIP8 disassembler",
			system:  arch.CHIP8System,
			wantErr: false,
		},
		{
			name:       "unsupported system",
			system:     arch.System("unknown"),
			wantErr:    true,
			errContain: "unsupported system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use empty converter and nil fileWriterConstructor in tests
			converter := parameter.New(parameter.Config{})
			dis, err := p.createDisassemblerForSystem(tt.system, converter, cart, disasmOpts, nil)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContain != "" {
					assert.ErrorContains(t, err, tt.errContain)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, dis)
		})
	}
}

//nolint:funlen // test functions can be long
func TestExecute(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)

	t.Run("execute pipeline successfully", func(t *testing.T) {
		nesData := buildMinimalNESROM(1, 0)
		cart, err := p.loader.LoadFromBytes(nesData, false, arch.NES)
		assert.NoError(t, err)

		opts := options.Program{
			Parameters: options.Parameters{Input: "test.nes"}, // Filename only used for logging
			Flags:      options.Flags{Assembler: "ca65", Quiet: true},
		}
		disasmOpts := options.Disassembler{}

		var buf bytes.Buffer
		ctx := context.Background()

		result, err := p.ExecuteWithCartridge(ctx, cart, opts, disasmOpts, &buf, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("execute with binary mode", func(t *testing.T) {
		nesData := buildMinimalNESROM(1, 0)
		cart, err := p.loader.LoadFromBytes(nesData, false, arch.NES)
		assert.NoError(t, err)

		opts := options.Program{
			Parameters: options.Parameters{Input: "test.nes"},
			Flags:      options.Flags{Assembler: "ca65", Binary: true, Quiet: true},
		}
		disasmOpts := options.Disassembler{}

		var buf bytes.Buffer
		ctx := context.Background()

		result, err := p.ExecuteWithCartridge(ctx, cart, opts, disasmOpts, &buf, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("execute with invalid assembler", func(t *testing.T) {
		nesData := buildMinimalNESROM(1, 0)
		cart, err := p.loader.LoadFromBytes(nesData, false, arch.NES)
		assert.NoError(t, err)

		opts := options.Program{
			Parameters: options.Parameters{Input: "test.nes"},
			Flags:      options.Flags{Assembler: "invalid", Quiet: true},
		}
		disasmOpts := options.Disassembler{}

		var buf bytes.Buffer
		ctx := context.Background()

		_, err = p.ExecuteWithCartridge(ctx, cart, opts, disasmOpts, &buf, arch.NES)
		assert.Error(t, err)
	})

	t.Run("execute with non-existent file", func(t *testing.T) {
		opts := options.Program{
			Parameters: options.Parameters{Input: "/nonexistent/file.nes"},
			Flags:      options.Flags{Assembler: "ca65", Quiet: true},
		}
		disasmOpts := options.Disassembler{}

		var buf bytes.Buffer
		ctx := context.Background()

		_, err := p.Execute(ctx, opts, disasmOpts, &buf)
		assert.Error(t, err)
	})
}

func TestExecuteWithCartridgeExportsCHR(t *testing.T) {
	chr := make([]byte, 8192)
	chr[0] = 0x12
	chr[len(chr)-1] = 0x34

	tests := []struct {
		assembler string
		wantSetup string
	}{
		{assembler: "asm6", wantSetup: ".base $0000"},
		{assembler: "ca65", wantSetup: ".segment \"TILES\""},
		{assembler: "nesasm", wantSetup: " .DATA"},
		{assembler: "retroasm", wantSetup: ".org $0000"},
	}

	for _, tt := range tests {
		t.Run(tt.assembler, func(t *testing.T) {
			logger := log.NewTestLogger(t)
			p := New(logger)
			cart, err := p.loader.LoadFromBytes(buildMinimalNESROMWithCHR(chr), false, arch.NES)
			assert.NoError(t, err)

			var gotFilename string
			var gotData []byte
			var gotPerm fs.FileMode
			p.writeFile = func(filename string, data []byte, perm fs.FileMode) error {
				gotFilename = filename
				gotData = append([]byte(nil), data...)
				gotPerm = perm
				return nil
			}

			opts := options.Program{
				Parameters: options.Parameters{Input: "roms/game.nes", Output: filepath.FromSlash("build/game.asm")},
				Flags:      options.Flags{Assembler: tt.assembler, Quiet: true},
				ExportCHR:  true,
			}

			var output bytes.Buffer
			result, err := p.ExecuteWithCartridge(context.Background(), cart, opts, options.Disassembler{}, &output, arch.NES)
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, filepath.FromSlash("build/game.chr"), gotFilename)
			assert.Equal(t, chr, gotData)
			assert.Equal(t, fs.FileMode(0o644), gotPerm)
			assert.Contains(t, output.String(), tt.wantSetup)
			assert.Contains(t, output.String(), "_chr_0000:\n")
			assert.Contains(t, output.String(), ".incbin \"game.chr\"")
		})
	}
}

func TestExecuteWithCartridgeAnnotatesInlineCHR(t *testing.T) {
	chr := make([]byte, 8192)
	for i := range 18 {
		chr[i] = byte(i + 1)
	}

	assemblers := []string{"asm6", "ca65", "nesasm", "retroasm"}
	for _, assembler := range assemblers {
		t.Run(assembler, func(t *testing.T) {
			p := New(log.NewTestLogger(t))
			cart, err := p.loader.LoadFromBytes(buildMinimalNESROMWithCHR(chr), false, arch.NES)
			assert.NoError(t, err)

			opts := options.Program{
				Parameters: options.Parameters{Input: "game.nes"},
				Flags:      options.Flags{Assembler: assembler, Quiet: true},
			}
			disasmOpts := options.NewDisassembler(assembler, arch.NES.String())

			var output bytes.Buffer
			_, err = p.ExecuteWithCartridge(context.Background(), cart, opts, disasmOpts, &output, arch.NES)
			assert.NoError(t, err)
			assert.Contains(t, output.String(), "; $0000")
			assert.Contains(t, output.String(), "; $0010")
		})
	}
}

func TestCHRFilenames(t *testing.T) {
	tests := []struct {
		name        string
		opts        options.Program
		wantOutput  string
		wantInclude string
	}{
		{
			name:        "derived from assembly output",
			opts:        options.Program{Parameters: options.Parameters{Output: filepath.FromSlash("build/game.asm")}},
			wantOutput:  filepath.FromSlash("build/game.chr"),
			wantInclude: "game.chr",
		},
		{
			name: "custom filename",
			opts: options.Program{Parameters: options.Parameters{
				Output:      filepath.FromSlash("build/game.asm"),
				CHRFilename: filepath.FromSlash("assets/tiles.chr"),
			}},
			wantOutput:  filepath.FromSlash("assets/tiles.chr"),
			wantInclude: "../assets/tiles.chr",
		},
		{
			name:        "derived from input when writing to stdout",
			opts:        options.Program{Parameters: options.Parameters{Input: filepath.FromSlash("roms/game.nes"), Output: options.OutputStdout}},
			wantOutput:  filepath.FromSlash("roms/game.chr"),
			wantInclude: "roms/game.chr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, include, err := chrFilenames(tt.opts)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantOutput, output)
			assert.Equal(t, tt.wantInclude, include)
		})
	}
}

func TestCHRFilenamesRejectsOverwrite(t *testing.T) {
	tests := []struct {
		name        string
		opts        options.Program
		wantMessage string
	}{
		{
			name:        "missing source filename",
			opts:        options.Program{},
			wantMessage: "cannot derive CHR-ROM filename",
		},
		{
			name: "input ROM",
			opts: options.Program{Parameters: options.Parameters{
				Input:       "game.nes",
				CHRFilename: "game.nes",
			}},
			wantMessage: "overwrite the input ROM",
		},
		{
			name: "assembly output",
			opts: options.Program{Parameters: options.Parameters{
				Output:      "game.asm",
				CHRFilename: "game.asm",
			}},
			wantMessage: "overwrite the assembly output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := chrFilenames(tt.opts)
			assert.ErrorContains(t, err, tt.wantMessage)
		})
	}
}

func TestExportCHRRejectsInvalidInputs(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)

	tests := []struct {
		name        string
		cart        *cartridge.Cartridge
		opts        options.Program
		system      arch.System
		wantMessage string
	}{
		{
			name:        "non NES system",
			cart:        &cartridge.Cartridge{CHR: []byte{1}},
			system:      arch.CHIP8System,
			wantMessage: "only supported for NES",
		},
		{
			name:        "binary input",
			cart:        &cartridge.Cartridge{CHR: []byte{1}},
			opts:        options.Program{Flags: options.Flags{Binary: true}},
			system:      arch.NES,
			wantMessage: "cannot be used with -binary",
		},
		{
			name:        "CHR RAM cartridge",
			cart:        &cartridge.Cartridge{},
			system:      arch.NES,
			wantMessage: "no CHR-ROM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.exportCHR(tt.cart, tt.opts, tt.system)
			assert.ErrorContains(t, err, tt.wantMessage)
		})
	}
}

func TestExportCHRPropagatesWriteError(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)
	wantErr := errors.New("write failed")
	p.writeFile = func(string, []byte, fs.FileMode) error { return wantErr }

	opts := options.Program{Parameters: options.Parameters{Input: "game.nes"}}
	_, err := p.exportCHR(&cartridge.Cartridge{CHR: []byte{1}}, opts, arch.NES)
	assert.ErrorIs(t, err, wantErr)
	assert.ErrorContains(t, err, "writing CHR-ROM file game.chr")
}

func TestNopCloser(t *testing.T) {
	var buf bytes.Buffer
	nc := &nopCloser{&buf}

	// Write some data
	n, err := nc.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)

	// Close should not error
	err = nc.Close()
	assert.NoError(t, err)

	// Check data was written
	assert.Equal(t, "test", buf.String())
}

//nolint:funlen // test functions can be long
func TestInitializeAssembler(t *testing.T) {
	logger := log.NewTestLogger(t)
	p := New(logger)

	tests := []struct {
		name           string
		assemblerName  string
		wantErr        bool
		errContains    string
		checkConverter bool
	}{
		{
			name:           "asm6 assembler",
			assemblerName:  "asm6",
			wantErr:        false,
			checkConverter: true,
		},
		{
			name:           "asm6 uppercase",
			assemblerName:  "ASM6",
			wantErr:        false,
			checkConverter: true,
		},
		{
			name:           "ca65 assembler",
			assemblerName:  "ca65",
			wantErr:        false,
			checkConverter: true,
		},
		{
			name:           "nesasm assembler",
			assemblerName:  "nesasm",
			wantErr:        false,
			checkConverter: true,
		},
		{
			name:           "retroasm assembler",
			assemblerName:  "retroasm",
			wantErr:        false,
			checkConverter: true,
		},
		{
			name:          "invalid assembler",
			assemblerName: "invalid",
			wantErr:       true,
			errContains:   "unsupported assembler",
		},
		{
			name:          "empty assembler name",
			assemblerName: "",
			wantErr:       true,
			errContains:   "unsupported assembler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constructor, converter, err := p.initializeAssembler(tt.assemblerName)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.ErrorContains(t, err, tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, constructor)
			if tt.checkConverter {
				// Converter should be initialized (non-zero struct)
				assert.NotNil(t, converter)
			}
		})
	}
}

//nolint:funlen // test functions can be long
func TestPrintInfo(t *testing.T) {
	tests := []struct {
		name   string
		system arch.System
		mapper byte
		quiet  bool
	}{
		{
			name:   "NES ROM with mapper 0",
			system: arch.NES,
			mapper: 0,
			quiet:  false,
		},
		{
			name:   "NES ROM with mapper 1 (experimental warning)",
			system: arch.NES,
			mapper: 1,
			quiet:  false,
		},
		{
			name:   "NES ROM with mapper 3",
			system: arch.NES,
			mapper: 3,
			quiet:  false,
		},
		{
			name:   "CHIP-8 ROM",
			system: arch.CHIP8System,
			mapper: 0,
			quiet:  false,
		},
		{
			name:   "quiet mode - no output",
			system: arch.NES,
			mapper: 1,
			quiet:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewTestLogger(t)
			p := New(logger)

			// Create test data in memory
			nesData := buildMinimalNESROM(1, tt.mapper)

			cart, err := p.loader.LoadFromBytes(nesData, false, tt.system)
			assert.NoError(t, err)

			opts := options.Program{
				Parameters: options.Parameters{Input: "test.nes"}, // Filename only used for logging
				Flags:      options.Flags{Assembler: "ca65", Quiet: tt.quiet},
			}

			// Call printInfo - should not panic
			p.printInfo(opts, cart, tt.system)
		})
	}
}

// buildMinimalNESROM creates a minimal valid NES ROM in iNES format.
//
//nolint:unparam // prgBanks parameter is useful for future test cases
func buildMinimalNESROM(prgBanks, mapper byte) []byte {
	const nesHeaderSize = 16
	const prgBankSize = 16384 // 16KB

	data := make([]byte, nesHeaderSize+int(prgBanks)*prgBankSize)

	// iNES header
	copy(data[0:4], []byte{'N', 'E', 'S', 0x1A}) // Magic number
	data[4] = prgBanks                           // Number of 16KB PRG-ROM banks
	data[5] = 0                                  // Number of 8KB CHR-ROM banks
	data[6] = mapper << 4                        // Mapper low nibble
	data[7] = mapper & 0xF0                      // Mapper high nibble

	return data
}

func buildMinimalNESROMWithCHR(chr []byte) []byte {
	const chrBankSize = 8192

	data := buildMinimalNESROM(1, 0)
	data[5] = byte(len(chr) / chrBankSize)
	return append(data, chr...)
}
