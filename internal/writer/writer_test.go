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
