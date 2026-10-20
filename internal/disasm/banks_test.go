package disasm

import (
	"io"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestTraceInstructionAcrossBankBoundary(t *testing.T) {
	// Splitting a physical output record must not stop CPU execution at the
	// bank boundary or treat the operand in the next bank as an opcode.
	cart := cartridge.New()
	cart.PRG = make([]byte, 2*0x8000)
	cart.PRG[0x3fff] = 0xa9
	copy(cart.PRG[0xc000:], []byte{0x42, 0xa9, 0x37, 0x60})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0xff, 0xbf, 0, 0})
	dis := testProgram(t, options.NewDisassembler(assembler.Ca65, "nes"), cart, nil)
	app, err := dis.Process(t.Context(), io.Discard, func(_ string) (io.WriteCloser, error) {
		return nopWriteCloser{io.Discard}, nil
	})
	assert.NoError(t, err)
	assert.True(t, app.PRG[0].Offsets[0x3fff].IsType(program.CodeAsData))
	assert.Equal(t, []byte{0xa9}, app.PRG[0].Offsets[0x3fff].Data)
	assert.Equal(t, "lda #$37", app.PRG[1].Offsets[0x4001].Code)
	assert.Equal(t, "rts", app.PRG[1].Offsets[0x4003].Code)
}
