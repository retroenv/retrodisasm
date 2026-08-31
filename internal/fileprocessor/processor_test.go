package fileprocessor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/loader"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/pipeline"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

// TestBinaryOptionSetsCodeOnly verifies that when Binary option is true,
// CodeOnly is automatically set to true to prevent NES-specific segments
// from being included in the output (fixes issue #82).
func TestBinaryOptionSetsCodeOnly(t *testing.T) {
	testCode := createTestCode()

	tests := []struct {
		name                  string
		binary                bool
		expectHeaderSegment   bool
		expectTilesSegment    bool
		expectVectorsSegment  bool
		expectCodeSegmentName bool
	}{
		{
			name:                  "binary mode excludes NES segments",
			binary:                true,
			expectHeaderSegment:   false,
			expectTilesSegment:    false,
			expectVectorsSegment:  false,
			expectCodeSegmentName: false, // CodeOnly mode doesn't write segment name
		},
		{
			name:                  "non-binary mode includes NES segments",
			binary:                false,
			expectHeaderSegment:   true,
			expectTilesSegment:    true,
			expectVectorsSegment:  true,
			expectCodeSegmentName: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSegmentTest(t, testCode, tt.binary,
				tt.expectHeaderSegment, tt.expectTilesSegment,
				tt.expectVectorsSegment, tt.expectCodeSegmentName)
		})
	}
}

func TestProcessFileExportsCHR(t *testing.T) {
	tempDir := t.TempDir()
	inputFilename := filepath.Join(tempDir, "game.nes")
	outputFilename := filepath.Join(tempDir, "game.asm")
	chrFilename := filepath.Join(tempDir, "game.chr")

	rom := createMinimalNESROM(createTestCode())
	wantCHR := rom[len(rom)-8192:]
	wantCHR[0] = 0x12
	wantCHR[len(wantCHR)-1] = 0x34
	assert.NoError(t, os.WriteFile(inputFilename, rom, 0o644))

	opts := options.Program{
		Parameters: options.Parameters{Input: inputFilename, Output: outputFilename},
		Flags:      options.Flags{Assembler: "ca65", System: arch.NES.String(), Quiet: true},
		ExportCHR:  true,
	}
	disasmOpts := options.NewDisassembler(opts.Assembler, opts.System)
	assert.NoError(t, ProcessFile(context.Background(), log.NewTestLogger(t), opts, disasmOpts))

	gotCHR, err := os.ReadFile(chrFilename)
	assert.NoError(t, err)
	assert.Equal(t, wantCHR, gotCHR)

	assembly, err := os.ReadFile(outputFilename)
	assert.NoError(t, err)
	assert.Contains(t, string(assembly), "_chr_0000:\n.incbin \"game.chr\"")
}

func createTestCode() []byte {
	// Simple 6502 program: LDA #$00, STA $0200, RTS
	return []byte{
		0xa9, 0x00, // LDA #$00
		0x8d, 0x00, 0x02, // STA $0200
		0x60, // RTS
	}
}

func runSegmentTest(t *testing.T, testCode []byte, binary,
	expectHeader, expectTiles, expectVectors, expectCodeSegment bool) {

	t.Helper()

	logger := log.NewTestLogger(t)

	// Create cartridge in memory
	var nesData []byte
	if binary {
		nesData = testCode
	} else {
		nesData = createMinimalNESROM(testCode)
	}

	// Load cartridge from bytes
	ldr := loader.New()
	cart, err := ldr.LoadFromBytes(nesData, binary, arch.NES)
	assert.NoError(t, err)

	programOpts := options.Program{
		Parameters: options.Parameters{Input: "test.nes"}, // Filename only used for logging
		Flags:      options.Flags{Assembler: "ca65", Binary: binary, System: arch.NES.String(), Quiet: true},
	}

	disasmOpts := options.NewDisassembler(programOpts.Assembler, programOpts.System)

	// Write to in-memory buffer
	var buf bytes.Buffer
	ctx := context.Background()

	pipe := pipeline.New(logger)
	_, err = pipe.ExecuteWithCartridge(ctx, cart, programOpts, disasmOpts, &buf, arch.NES)
	assert.NoError(t, err)

	verifyOutput(t, buf.String(), expectHeader, expectTiles, expectVectors, expectCodeSegment)
}

func verifyOutput(t *testing.T, output string, expectHeader, expectTiles, expectVectors, expectCodeSegment bool) {
	t.Helper()

	assert.True(t, len(output) > 0, "output should not be empty")

	// Verify segment presence/absence
	hasHeader := strings.Contains(output, ".segment \"HEADER\"")
	hasTiles := strings.Contains(output, ".segment \"TILES\"")
	hasVectors := strings.Contains(output, ".segment \"VECTORS\"")
	hasCodeSegment := strings.Contains(output, ".segment \"CODE\"")

	assert.Equal(t, expectHeader, hasHeader, "HEADER segment presence mismatch")
	assert.Equal(t, expectTiles, hasTiles, "TILES segment presence mismatch")
	assert.Equal(t, expectVectors, hasVectors, "VECTORS segment presence mismatch")
	assert.Equal(t, expectCodeSegment, hasCodeSegment, "CODE segment name presence mismatch")

	// Verify we have actual code output
	hasCode := strings.Contains(output, "lda") || strings.Contains(output, "sta") || strings.Contains(output, "rts")
	assert.True(t, hasCode, "output should contain disassembled code")
}

// createMinimalNESROM creates a minimal valid NES ROM with the given code
func createMinimalNESROM(code []byte) []byte {
	rom := make([]byte, 0, 16+16384+8192) // Header + 16KB PRG + 8KB CHR

	// iNES header (16 bytes)
	header := []byte{
		0x4E, 0x45, 0x53, 0x1A, // "NES" + MS-DOS EOF
		0x01,       // 1x 16KB PRG-ROM
		0x01,       // 1x 8KB CHR-ROM
		0x00,       // Mapper 0, horizontal mirroring
		0x00,       // Mapper 0
		0x00,       // No PRG-RAM
		0x00,       // NTSC
		0x00, 0x00, // Unused
		0x00, 0x00, 0x00, 0x00, // Padding
	}
	rom = append(rom, header...)

	// PRG-ROM (16384 bytes)
	prgROM := make([]byte, 16384)
	copy(prgROM, code)

	// Set reset vector to point to start of code (0x8000)
	prgROM[0x3FFC] = 0x00
	prgROM[0x3FFD] = 0x80

	rom = append(rom, prgROM...)

	// CHR-ROM (8192 bytes)
	chrROM := make([]byte, 8192)
	rom = append(rom, chrROM...)

	return rom
}
