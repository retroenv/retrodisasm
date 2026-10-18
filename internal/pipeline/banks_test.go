package pipeline

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/verification"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

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
