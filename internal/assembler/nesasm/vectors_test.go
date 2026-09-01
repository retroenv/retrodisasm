package nesasm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestWriteBankVectorsSelectsFinalPrgBank(t *testing.T) {
	var output bytes.Buffer
	fileWriter := FileWriter{}
	bank := program.NewPRGBank(0x8000)
	bank.BaseAddress = 0x8000
	bank.Vectors = [3]uint16{0xcf5d, 0xd706, 0xd706}

	assert.NoError(t, fileWriter.writeBankVectorsTo(&output, bank, 7))

	text := output.String()
	assert.True(t, strings.Index(text, ".bank 7") < strings.Index(text, ".org $FFFA"))
	assert.Contains(t, text, ".dw $CF5D, $D706, $D706")
}
