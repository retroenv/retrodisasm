package disasm

import (
	"bytes"
	"io"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestMapperRegisterWriteKeepsAddress(t *testing.T) {
	// A bank select write must not refer to the ROM byte at the register
	// address, because that byte belongs to a switchable bank.
	cart := cartridge.New()
	cart.Mapper = 2
	cart.PRG = make([]byte, 3*0x4000)
	copy(cart.PRG[0x8000:], []byte{0xa9, 1, 0x8d, 0x00, 0x80, 0xad, 0x00, 0x80, 0x60})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0x00, 0xc0, 0, 0})
	dis := testProgram(t, options.NewDisassembler(assembler.Ca65, "nes"), cart, nil)
	var output bytes.Buffer
	_, err := dis.Process(t.Context(), &output, func(_ string) (io.WriteCloser, error) {
		return nopWriteCloser{&output}, nil
	})
	assert.NoError(t, err)
	assert.Contains(t, output.String(), "sta a:$8000")
}
