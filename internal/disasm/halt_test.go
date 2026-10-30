package disasm

import (
	"fmt"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestDisasmStopsAtKIL(t *testing.T) {
	for _, opcode := range []byte{0x02, 0x12, 0x22, 0x32, 0x42, 0x52, 0x62, 0x72, 0x92, 0xb2, 0xd2, 0xf2} {
		for _, banked := range []bool{false, true} {
			for _, branch := range []bool{false, true} {
				t.Run(fmt.Sprintf("%02x/banked=%t/branch=%t", opcode, banked, branch), func(t *testing.T) {
					cart := cartridge.New()
					base := 0
					bankIndex := 0

					if banked {
						cart.Mapper = 2
						cart.PRG = make([]byte, 0x10000)
						base = 0xc000
						bankIndex = 3
						copy(cart.PRG[len(cart.PRG)-4:], []byte{0, 0xc0})
					}

					copy(cart.PRG[base:], []byte{0xea, 0xea, opcode, 0xa9, 0x42, 0x60})
					if branch {
						// A separate branch can reach the bytes after KIL.
						copy(cart.PRG[base:], []byte{0xd0, 1})
					}

					dis := testProgram(t, options.NewDisassembler(assembler.Ca65, "nes"), cart, nil)
					app, err := dis.analyzeMappings(t.Context())
					assert.NoError(t, err)
					bank := app.PRG[bankIndex]
					assert.True(t, bank.Offsets[2].IsType(program.CodeOffset|program.CodeAsData))
					assert.Equal(t, branch, bank.Offsets[3].IsType(program.CodeOffset|program.CodeAsData))
					if branch {
						assert.Equal(t, "lda #$42", bank.Offsets[3].Code)
					}
				})
			}
		}
	}
}
