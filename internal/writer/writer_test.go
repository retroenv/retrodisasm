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
