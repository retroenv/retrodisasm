package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestQualityReport(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		report := runQuality(t, enabled)
		if !enabled {
			assert.Equal(t, "", report)
			continue
		}
		assert.Contains(t, report, "instructions 3, suspicious 1 (33.33%)")
		assert.Contains(t, report, "bank 0 $8000 ROM $000000: decimal")
	}
}

// runQuality disassembles a cartridge with one suspicious instruction and
// returns the quality report.
func runQuality(t *testing.T, enabled bool) string {
	t.Helper()

	cart := cartridge.New()
	cart.PRG = make([]byte, 0x8000)
	cart.CHR = make([]byte, 0x2000)
	copy(cart.PRG, []byte{
		0xf8,             // $8000: sed
		0xe8,             // $8001: inx
		0x4c, 0x01, 0x80, // $8002: jmp $8001
	})
	copy(cart.PRG[0x7ffa:], []byte{0, 0x80, 0, 0x80, 0, 0x80})

	opts := options.Program{}
	opts.Assembler = assembler.Ca65
	opts.Output = filepath.Join(t.TempDir(), "output.asm")
	opts.Quality = enabled

	var source, report strings.Builder
	p := New(log.NewTestLogger(t))
	p.report = &report
	_, err := p.ExecuteWithCartridge(t.Context(), cart, opts, options.NewDisassembler(assembler.Ca65, "nes"),
		&source, arch.NES)
	assert.NoError(t, err)
	return report.String()
}
