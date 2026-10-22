package pipeline

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/verification"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestUNROMBankOutput(t *testing.T) {
	// Switchable bank tails are code/data, not interrupt vectors.
	// The fixed bank must retain its $C000 origin and calls into bank 0.
	for _, format := range []string{"ca65", "asm6", "nesasm", "retroasm"} {
		for _, split := range []bool{false, true} {
			name := format + "/combined"
			if split {
				name = format + "/split"
			}
			t.Run(name, func(t *testing.T) {
				testUNROMBankOutput(t, format, split)
			})
		}
	}
}

func TestJumpTableAcrossPhysicalBanks(t *testing.T) {
	// A pointer across physical banks must retain both bytes and its target.
	// It must not create a record that extends past either output bank.
	for _, id := range []uint16{2} {
		for _, format := range []string{"ca65", "asm6", "nesasm", "retroasm"} {
			for _, split := range []bool{false, true} {
				name := strconv.Itoa(int(id)) + "/" + format + "/" + strconv.FormatBool(split)
				t.Run(name, func(t *testing.T) {
					cart := boundaryJumpTableCartridge(id)
					app, opts, _ := disassembleBanked(t, cart, format, split)
					assert.True(t, app.PRG[0].Offsets[0x100].IsType(program.CodeOffset|program.CodeAsData))
					for _, bank := range app.PRG {
						var data []byte
						for i, info := range bank.Offsets {
							assert.True(t, i+len(info.Data) <= len(bank.Offsets))
							data = append(data, info.Data...)
						}
						end := bank.ROMOffset + len(bank.Offsets)
						assert.Equal(t, cart.PRG[bank.ROMOffset:end], data)
					}
					t.Run("reassembly", func(t *testing.T) {
						verifyBankedOutput(t, format, opts, cart, app)
					})
				})
			}
		}
	}
}

func TestUNROMOddBankCount(t *testing.T) {
	// A partial 32 KiB output chunk previously shifted the entire code base
	// to $C000, preventing calls into the lower UNROM window from being traced.
	cart := unromCartridge()
	cart.PRG = append(cart.PRG[:2*0x4000:2*0x4000], cart.PRG[7*0x4000:]...)
	opts := options.Program{
		Flags: options.Flags{
			Assembler: "ca65",
			Quiet:     true,
		},
	}

	var output bytes.Buffer
	app, err := New(log.NewTestLogger(t)).ExecuteWithCartridge(t.Context(), cart, opts,
		options.NewDisassembler("ca65", "nes"), &output, arch.NES)
	assert.NoError(t, err)
	assert.Equal(t, uint16(0x8000), app.CodeBaseAddress)
	assert.Len(t, app.PRG, 3)
	assert.True(t, app.PRG[0].Offsets[0].IsType(program.CodeOffset))
	assert.Contains(t, output.String(), "lda #$42")
}

func TestUNROMTracesEveryLowerBank(t *testing.T) {
	// Each fixed-bank call to $8000 must be followed in every mapping, even
	// when the CPU address and interrupt vectors match earlier traces.
	cart := unromCartridge()
	for i := range 7 {
		copy(cart.PRG[i*0x4000:], []byte{0xa9, byte(i + 1), 0x60})
	}
	app, _, _ := disassembleBanked(t, cart, "ca65", false)
	for i := range 7 {
		assert.True(t, app.PRG[i].Offsets[0].IsType(program.CodeOffset))
		assert.Equal(t, []byte{0xa9, byte(i + 1)}, app.PRG[i].Offsets[0].Data)
		assert.Contains(t, app.PRG[i].Offsets[0].Label, "_bank")
	}
	assert.True(t, app.PRG[7].Offsets[0].IsType(program.CodeAsData))
	assert.Equal(t, []byte{0x20}, app.PRG[7].Offsets[0].Data)
}

func TestAxROMMappingOutput(t *testing.T) {
	// Both halves of each bank must be selected together, and identical
	// vector addresses must still produce independent traces and symbols.
	for _, format := range []string{"ca65", "asm6", "nesasm", "retroasm"} {
		for _, split := range []bool{false, true} {
			name := format + "/combined"
			if split {
				name = format + "/split"
			}
			t.Run(name, func(t *testing.T) {
				cart := axromCartridge()
				app, opts, _ := disassembleBanked(t, cart, format, split)
				assert.Len(t, app.PRG, 2)
				assert.Equal(t, "Reset_bank0_at8000", app.PRG[0].Offsets[0].Label)
				assert.Equal(t, "Reset_bank1_at8000", app.PRG[1].Offsets[0].Label)
				for i, bank := range app.PRG {
					assert.True(t, bank.HasVectors)
					assert.Equal(t, uint16(0x8000), bank.BaseAddress)
					assert.Equal(t, []byte{0xa9, byte(i + 1)}, bank.Offsets[0x4000].Data)
					assert.True(t, bank.Offsets[0x4000].IsType(program.CodeOffset))
				}
				t.Run("reassembly", func(t *testing.T) {
					verifyBankedOutput(t, format, opts, cart, app)
				})
			})
		}
	}
}

func testUNROMBankOutput(t *testing.T, format string, split bool) {
	t.Helper()
	cart := unromCartridge()
	app, opts, source := disassembleBanked(t, cart, format, split)
	assert.Len(t, app.PRG, 8)
	for i, bank := range app.PRG {
		base := uint16(0x8000)
		if i == 7 {
			base = 0xc000
		}
		assert.Len(t, bank.Offsets, 0x4000)
		assert.Equal(t, base, bank.BaseAddress)
		assert.Equal(t, base, bank.Offsets[0].Address)
		assert.Equal(t, i == 7, bank.HasVectors)
	}
	assert.True(t, app.PRG[0].Offsets[0x3ffa].IsType(program.CodeOffset))
	assert.True(t, app.PRG[0].Offsets[0x3fff].IsType(program.CodeOffset))
	assert.Equal(t, "Reset_bank7_atc000", app.PRG[7].Offsets[0].Label)
	assert.Contains(t, app.PRG[7].Offsets[0].Comment, "$C000")

	if split {
		data, readErr := os.ReadFile(generateBankFilename(opts.Output, app.PRG[0].Name))
		assert.NoError(t, readErr)
		source = string(data)
	}
	assert.Contains(t, source, "lda #$42")
	assert.NotContains(t, strings.ToLower(source), ".org $bffa")
	assert.NotContains(t, strings.ToLower(source), ".pad $bffa")
	t.Run("reassembly", func(t *testing.T) {
		verifyBankedOutput(t, format, opts, cart, app)
	})
}

func verifyBankedOutput(t *testing.T, format string, opts options.Program, cart *cartridge.Cartridge,
	app *program.Program) {

	t.Helper()
	requireNESAssembler(t, format)
	if format == "nesasm" {
		t.Skip("NESASM changes legacy mapper headers during reassembly")
	}
	assert.NoError(t, verification.VerifyOutput(t.Context(), log.NewTestLogger(t), opts, cart, app))
}

func disassembleBanked(t *testing.T, cart *cartridge.Cartridge, format string,
	split bool) (*program.Program, options.Program, string) {

	t.Helper()
	dir := t.TempDir()
	opts := options.Program{
		Parameters: options.Parameters{
			Input:  filepath.Join(dir, "input.nes"),
			Output: filepath.Join(dir, "output.asm"),
		},
		Flags: options.Flags{
			Assembler: format,
			Quiet:     true,
		},
	}
	disOpts := options.NewDisassembler(format, "nes")
	disOpts.SplitBanks = split
	var output bytes.Buffer
	app, err := New(log.NewTestLogger(t)).ExecuteWithCartridge(t.Context(), cart, opts, disOpts, &output, arch.NES)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(opts.Output, output.Bytes(), 0o600))
	var rom bytes.Buffer
	assert.NoError(t, cart.Save(&rom))
	assert.NoError(t, os.WriteFile(opts.Input, rom.Bytes(), 0o600))
	return app, opts, output.String()
}

func axromCartridge() *cartridge.Cartridge {
	cart := cartridge.New()
	cart.Mapper = 7
	cart.PRG = make([]byte, 2*0x8000)
	cart.CHR = nil
	for i := range 2 {
		start := i * 0x8000
		copy(cart.PRG[start:], []byte{0x20, 0, 0xc0, 0x40})                           // JSR $C000, RTI.
		copy(cart.PRG[start+0x4000:], []byte{0xa9, byte(i + 1), 0x8d, 1, 0x20, 0x60}) // LDA, STA PPU_MASK, RTS.
		copy(cart.PRG[start+0x7ffa:], []byte{3, 0x80, 0, 0x80, 3, 0x80})
	}
	return cart
}

func requireNESAssembler(t *testing.T, format string) {
	t.Helper()
	var commands []string
	switch format {
	case "ca65":
		commands = []string{"ca65", "ld65"}
	case "asm6":
		commands = []string{"asm6f"}
	case "nesasm":
		commands = []string{"nesasm"}
	default:
		t.Skip("external verification supports ca65, asm6, and nesasm")
	}

	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s is not installed", command)
		}
	}
}

func unromCartridge() *cartridge.Cartridge {
	cart := cartridge.New()
	cart.Mapper = 2
	cart.PRG = make([]byte, 8*0x4000)
	cart.CHR = nil
	for i := range 7 {
		cart.PRG[i*0x4000+0x100] = byte(i + 1)
	}
	copy(cart.PRG, []byte{0x4c, 0xfa, 0xbf})                            // JMP $BFFA.
	copy(cart.PRG[0x3ffa:], []byte{0xa9, 0x42, 0x8d, 0x00, 0x02, 0x60}) // LDA, STA, RTS.
	copy(cart.PRG[7*0x4000:], []byte{0x20, 0x00, 0x80, 0x40})           // JSR $8000, RTI.
	copy(cart.PRG[len(cart.PRG)-6:], []byte{0x03, 0xc0, 0x00, 0xc0, 0x03, 0xc0})
	return cart
}

func boundaryJumpTableCartridge(id uint16) *cartridge.Cartridge {
	cart := cartridge.New()
	cart.Mapper = id
	cart.PRG = make([]byte, 0x10000)
	cart.CHR = nil
	bankSize, fixedAddress, highByte := 0x4000, uint16(0xc000), 0xc000
	table := uint16(0x8000 + bankSize - 1)
	copy(cart.PRG, []byte{
		0xa5, 0, 0x0a, 0xaa,
		0xbd, byte(table), byte(table >> 8), 0x8d, 0, 2,
		0xbd, byte(table + 1), byte((table + 1) >> 8), 0x8d, 1, 2,
		0x6c, 0, 2,
	})
	cart.PRG[bankSize-1], cart.PRG[highByte] = 0, 0x81
	cart.PRG[0x100] = 0x40
	copy(cart.PRG[len(cart.PRG)-bankSize+0x10:], []byte{0x20, 0, 0x80, 0x40})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0x10, byte(fixedAddress >> 8)})
	return cart
}

func TestRelativeBranchOutsideCode(t *testing.T) {
	// Numeric branches into RAM or the vector table cannot be assembled in
	// relocatable ca65 segments. Preserve their original relative encoding.
	for _, address := range []uint16{0x8000, 0xffab} {
		for _, format := range []string{"ca65", "asm6", "nesasm"} {
			t.Run(format+"/"+strconv.Itoa(int(address)), func(t *testing.T) {
				cart := cartridge.New()
				cart.PRG = make([]byte, 0x8000)
				operand := byte(0xfd)
				if address == 0xffab {
					operand = 0x52
				}
				copy(cart.PRG[int(address)-0x8000:], []byte{0xd0, operand, 0})
				copy(cart.PRG[0x7ffa:], []byte{0, 0, byte(address), byte(address >> 8), 0, 0})
				app, opts, _ := disassembleBanked(t, cart, format, false)
				assert.True(t, app.PRG[0].Offsets[int(address)-0x8000].IsType(program.CodeAsData))
				requireNESAssembler(t, format)
				assert.NoError(t, verification.VerifyOutput(t.Context(), log.NewTestLogger(t), opts, cart, app))
			})
		}
	}
}
