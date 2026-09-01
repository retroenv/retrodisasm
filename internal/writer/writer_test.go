package writer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestOutputAliasMap_SkipsDuplicateReEmission(t *testing.T) {
	app := &program.Program{}
	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	aliases := map[string]uint16{
		"PPU_CTRL": 0x2000,
		"PPU_MASK": 0x2001,
	}

	assert.NoError(t, w.OutputAliasMap(aliases))
	assert.NoError(t, w.OutputAliasMap(aliases))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "PPU_CTRL = $2000"))
	assert.Equal(t, 1, strings.Count(out, "PPU_MASK = $2001"))
}

func TestOutputAliasMap_EmitsWhenAddressDiffers(t *testing.T) {
	app := &program.Program{}
	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"FOO": 0x0010}))
	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"FOO": 0x0020}))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "FOO = $0010"))
	assert.Equal(t, 1, strings.Count(out, "FOO = $0020"))
}

func TestForOutputSharesAliasEmissionState(t *testing.T) {
	app := &program.Program{}
	var first, second bytes.Buffer
	w := New(app, &first, Options{})

	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"PPU_CTRL": 0x2000}))
	child := w.ForOutput(&second, Options{})
	assert.NoError(t, child.OutputAliasMap(map[string]uint16{"PPU_CTRL": 0x2000}))

	assert.Contains(t, first.String(), "PPU_CTRL = $2000")
	assert.NotContains(t, second.String(), "PPU_CTRL = $2000")
}

func TestProcessPRGWritesCrossSegmentBranchAsBytes(t *testing.T) {
	var buf bytes.Buffer
	bank := program.NewPRGBank(2)
	bank.Offsets[0] = program.Offset{
		Data: []byte{0xD0, 0x2E},
		Type: program.CodeOffset,
		Code: "bne _label_c010",
	}
	w := New(&program.Program{}, &buf, Options{LiteralCrossSegmentBranches: true})

	assert.NoError(t, w.ProcessPRG(bank, 2))

	assert.Equal(t, "  .byte $d0, $2e\n", buf.String())
}

func TestProcessPRGWritesLiteralBranchOutsideSegmentAsBytes(t *testing.T) {
	var buf bytes.Buffer
	bank := program.NewPRGBank(0x8000)
	bank.BaseAddress = 0x8000
	bank.Offsets[0] = program.Offset{
		Address: 0x8000,
		Data:    []byte{0x30, 0x80},
		Type:    program.CodeOffset,
		Code:    "bmi $7F82",
	}
	w := New(&program.Program{}, &buf, Options{LiteralCrossSegmentBranches: true})

	assert.NoError(t, w.ProcessPRG(bank, 2))

	assert.Equal(t, "  .byte $30, $80\n", buf.String())
}

func TestGetPrgDataStopsAtBankEnd(t *testing.T) {
	bank := program.NewPRGBank(3)
	bank.Offsets[0] = program.Offset{
		Data: []byte{0xa9, 0x01, 0x02},
		Type: program.DataOffset,
	}

	data := getPrgData(bank, 0, 2)

	assert.Equal(t, []byte{0xa9, 0x01}, data)
}
