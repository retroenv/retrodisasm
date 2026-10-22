package disasm

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
	"github.com/retroenv/retrogolib/assert"
)

func TestMappingTraceStopsAtUnofficial(t *testing.T) {
	// Mapping continuations must not resume code after an unofficial opcode.
	// Direct branches and explicit code hints must still permit that opcode.
	for _, test := range []struct {
		name       string
		prefix     []byte
		annotation bool
		wantCode   bool
	}{
		{
			name: "fallthrough",
		},
		{
			name:     "direct branch",
			prefix:   []byte{0x4c, 3, 0xc0},
			wantCode: true,
		},
		{
			name:     "conditional branch",
			prefix:   []byte{0xd0, 2, 0xea, 0xea},
			wantCode: true,
		},
		{
			name:       "code hint",
			annotation: true,
			wantCode:   true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cart := &cartridge.Cartridge{
				Mapper: 2,
				PRG:    make([]byte, 0x10000),
			}
			code := append(bytes.Clone(test.prefix), 0x04, 0, 0xa9, 1, 0x8d, 0, 0x80, 0xa9, 0x42, 0x60)
			copy(cart.PRG[0xc000:], code)
			copy(cart.PRG[0xfffc:], []byte{0, 0xc0})
			opts := options.NewDisassembler(assembler.Ca65, "nes")
			opts.StopAtUnofficial = true
			dis := testProgram(t, opts, cart, nil)
			if test.annotation {
				assert.NoError(t, dis.ApplyAnnotations(writeConfig(t, "[code]\n$C000=$C001")))
			}
			app, err := dis.analyzeMappings(t.Context())
			assert.NoError(t, err)
			continuation := app.PRG[3].Offsets[len(test.prefix)+7]
			assert.Equal(t, test.wantCode, continuation.IsType(program.CodeOffset))
		})
	}
}

func TestMappingHintsDiscoverMMC3Mode(t *testing.T) {
	// Code outside the vector paths must discover the same bank switch as a
	// vector entry. Operands must not become independent exploration roots.
	for _, source := range []string{"vector", "annotation", "annotation after return", "cdl"} {
		t.Run(source, func(t *testing.T) {
			cart := &cartridge.Cartridge{
				Mapper: 4,
				PRG:    make([]byte, 0x10000),
			}
			cart.PRG[0xe000] = 0x40
			copy(cart.PRG[0xfffc:], []byte{0, 0xe0})
			entry := 0xe100
			if source == "annotation after return" {
				cart.PRG[entry] = 0x60
				entry++
			}
			copy(cart.PRG[entry:], []byte{0xa9, 0x46, 0x8d, 0, 0x80, 0x4c, 0, 0xc1})
			copy(cart.PRG[0x100:], []byte{0xa9, 0x42, 0x60})
			opts := options.NewDisassembler(assembler.Ca65, "nes")
			switch source {
			case "vector":
				copy(cart.PRG[0xfffc:], []byte{0, 0xe1})

			case "cdl":
				flags := make([]byte, len(cart.PRG))
				for i := range 8 {
					flags[entry+i] = byte(codedatalog.Code)
				}
				opts.CodeDataLog = io.NopCloser(bytes.NewReader(flags))
			}
			dis := testProgram(t, opts, cart, nil)
			if source == "annotation" || source == "annotation after return" {
				config := fmt.Sprintf("[code]\n$E100=$%04X", entry+7)
				assert.NoError(t, dis.ApplyAnnotations(writeConfig(t, config)))
			}
			app, err := dis.analyzeMappings(t.Context())
			assert.NoError(t, err)
			target := app.PRG[0].Offsets[0x100]
			assert.True(t, target.IsType(program.CodeOffset|program.CodeAsData))
			assert.Equal(t, byte(0xa9), target.Data[0])
		})
	}
}

func TestMappingTraceRetainsRAMStoreContinuation(t *testing.T) {
	// The same indexed store can write RAM or a mapper register. A mapping
	// stop from one path must not remove the other path's continuation.
	for _, stopAtUnofficial := range []bool{false, true} {
		t.Run(strconv.FormatBool(stopAtUnofficial), func(t *testing.T) {
			cart := &cartridge.Cartridge{
				Mapper: 4,
				PRG:    make([]byte, 0x10000),
			}
			copy(cart.PRG[0xe000:], []byte{
				0xa9, 0x46, 0xa2, 0,
				0x24, 0, 0xd0, 2, 0xa2, 1,
				0x9d, 0xff, 0x7f, 0x4c, 0, 0x80,
			})
			copy(cart.PRG[0xfffc:], []byte{0, 0xe0})
			copy(cart.PRG, []byte{0xa9, 0x42, 0x60})
			opts := options.NewDisassembler(assembler.Ca65, "nes")
			opts.StopAtUnofficial = stopAtUnofficial
			dis := testProgram(t, opts, cart, nil)
			app, err := dis.analyzeMappings(t.Context())
			assert.NoError(t, err)
			assert.Equal(t, "lda #$42", app.PRG[0].Offsets[0].Code)
			assert.True(t, app.PRG[0].Offsets[0].IsType(program.CodeOffset))
		})
	}
}

func TestTraceInstructionAcrossUNROMBoundary(t *testing.T) {
	// Splitting a physical output record must not stop CPU execution at the
	// bank boundary or treat the operand in the next bank as an opcode.
	cart := cartridge.New()
	cart.Mapper = 2
	cart.PRG = make([]byte, 3*0x4000)
	cart.PRG[0x3fff] = 0xa9
	copy(cart.PRG[0x8000:], []byte{0x42, 0xa9, 0x37, 0x60})
	copy(cart.PRG[len(cart.PRG)-4:], []byte{0xff, 0xbf, 0, 0})
	dis := testProgram(t, options.NewDisassembler(assembler.Ca65, "nes"), cart, nil)
	app, err := dis.analyzeMappings(t.Context())
	assert.NoError(t, err)
	assert.True(t, app.PRG[0].Offsets[0x3fff].IsType(program.CodeAsData))
	assert.Equal(t, []byte{0xa9}, app.PRG[0].Offsets[0x3fff].Data)
	assert.Equal(t, "lda #$37", app.PRG[2].Offsets[1].Code)
	assert.Equal(t, "rts", app.PRG[2].Offsets[3].Code)
}

func TestMergeMappingDependentReferences(t *testing.T) {
	// The same fixed-bank JSR can resolve to different physical lower banks.
	// Its original bytes must survive without choosing either bank's symbol.
	left := analysisBank([]byte{0x20, 0, 0x80, 0x60})
	right := analysisBank([]byte{0x20, 0, 0x80, 0x60})
	left.Offsets[0].Data = []byte{0x20, 0, 0x80}
	left.Offsets[0].Code = "jsr Entry_bank0"
	left.Offsets[0].Type = program.CodeOffset
	right.Offsets[0] = left.Offsets[0]
	right.Offsets[0].Code = "jsr Entry_bank1"
	for _, bank := range []*program.PRGBank{left, right} {
		bank.Offsets[1].Data = nil
		bank.Offsets[2].Data = nil
		bank.Offsets[3].Code = "rts"
		bank.Offsets[3].Type = program.CodeOffset
	}
	mergeBankAnalysis(left, right)
	for i, value := range []byte{0x20, 0, 0x80} {
		assert.Equal(t, []byte{value}, left.Offsets[i].Data)
		assert.True(t, left.Offsets[i].IsType(program.CodeAsData))
		assert.Empty(t, left.Offsets[i].Code)
	}
	assert.Equal(t, "rts", left.Offsets[3].Code)
	mergeBankAnalysis(left, right)
	assert.Empty(t, left.Offsets[0].Code)
}

func TestMergeMappingLabelInsideInstruction(t *testing.T) {
	// A destination found in another mapping must not disappear inside a
	// multi-byte output record copied from the first mapping.
	left := analysisBank([]byte{0xa9, 0x60})
	left.Offsets[0].Data = []byte{0xa9, 0x60}
	left.Offsets[0].Type = program.CodeOffset
	left.Offsets[0].Code = "lda #$60"
	left.Offsets[1].Data = nil
	right := analysisBank([]byte{0xa9, 0x60})
	right.Offsets[1].Type = program.CodeOffset
	right.Offsets[1].Code = "rts"
	right.Offsets[1].Label = "Entry"
	mergeBankAnalysis(left, right)
	assert.Equal(t, []byte{0xa9}, left.Offsets[0].Data)
	assert.Equal(t, []byte{0x60}, left.Offsets[1].Data)
	assert.Equal(t, "Entry", left.Offsets[1].Label)
}

func TestRewriteBankSymbols(t *testing.T) {
	names := map[string]string{"Entry": "Entry_bank1", "ABCD": "ABCD_bank1"}
	assert.Equal(t, "lda Entry_bank1+1,X", rewriteSymbols("lda Entry+1,X", names))
	assert.Equal(t, ".word $ABCD, ABCD_bank1", rewriteSymbols(".word $ABCD, ABCD", names))
	assert.Equal(t, "EntryExtra", rewriteSymbols("EntryExtra", names))
}

func TestMergeAlternateCPUOrigin(t *testing.T) {
	// Alternate-origin code cannot be relocated symbolically into the fixed
	// bank. Keep its bytes and define its labels at their actual CPU addresses.
	left := analysisBank([]byte{0xa9, 0x42})
	right := analysisBank([]byte{0xa9, 0x42})
	right.BaseAddress = 0x8000
	right.Offsets[0] = program.Offset{
		Address: 0x8000,
		Data:    []byte{0xa9, 0x42},
		Type:    program.CodeOffset,
		Code:    "lda #$42",
		Label:   "Entry_at8000",
	}
	right.Offsets[1].Data = nil
	mergeBankAnalysis(left, right)
	assert.Equal(t, uint16(0x8000), left.Constants["Entry_at8000"])
	assert.Empty(t, left.Offsets[0].Label)
	assert.Empty(t, left.Offsets[0].Code)
	assert.Equal(t, []byte{0xa9}, left.Offsets[0].Data)
	assert.Equal(t, []byte{0x42}, left.Offsets[1].Data)
	assert.True(t, left.Offsets[0].IsType(program.CodeAsData))
}

func analysisBank(data []byte) *program.PRGBank {
	bank := program.NewPRGBank(len(data))
	bank.BaseAddress = 0xc000
	for i := range data {
		bank.Offsets[i] = program.Offset{
			Address: 0xc000 + uint16(i),
			Data:    data[i : i+1],
			Type:    program.DataOffset,
		}
	}
	return bank
}
