package pipeline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestExecuteWithAnnotations(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "game.ini")
	assert.NoError(t, os.WriteFile(filename, []byte("[vectors]\nreset=Boot\n[comments]\n$8000=Boot annotation\n"+
		"[comments_before]\n$8000=Boot section\\nSecond line\n[blank_lines]\n$8000=2\n"+
		"[symbols]\nSecond=2\nFirst=1\n[symbol_groups]\nOrdered=Second, First"), 0o600))
	for _, format := range []string{"ca65", "asm6", "nesasm", "retroasm"} {
		t.Run(format, func(t *testing.T) {
			cart := cartridge.New()
			cart.PRG[0], cart.PRG[0x7ffd] = 0x60, 0x80
			opts := options.Program{
				Parameters: options.Parameters{Annotations: filename},
				Flags:      options.Flags{Assembler: format, Quiet: true},
			}
			var output bytes.Buffer
			pipe := New(log.NewTestLogger(t))
			app, err := pipe.ExecuteWithCartridge(t.Context(), cart, opts,
				options.NewDisassembler(format, "nes"), &output, arch.NES)
			assert.NoError(t, err)
			assert.Equal(t, "Boot", app.Handlers.Reset)
			assert.True(t, strings.Contains(output.String(), "Boot:"))
			assert.True(t, strings.Contains(output.String(), "Boot annotation"))
			assert.True(t, strings.Contains(output.String(), "\n\n; Boot section\n; Second line\nBoot:"))
			assert.True(t, strings.Contains(output.String(), "; Ordered\nSecond = $0002\nFirst = $0001"))
		})
	}
}

func TestExecuteWithAnnotationsChecksumFailure(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "wrong-game.ini")
	assert.NoError(t, os.WriteFile(filename, []byte("[rom]\nprg_crc32=00000000"), 0o600))
	opts := options.Program{
		Parameters: options.Parameters{Annotations: filename},
		Flags:      options.Flags{Assembler: "ca65", Quiet: true},
	}
	var output bytes.Buffer
	pipe := New(log.NewTestLogger(t))
	_, err := pipe.ExecuteWithCartridge(t.Context(), cartridge.New(), opts,
		options.NewDisassembler("ca65", "nes"), &output, arch.NES)
	assert.ErrorContains(t, err, "prg_crc32 mismatch")
	assert.Equal(t, 0, output.Len())
}
