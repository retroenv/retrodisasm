package nesasm

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestSetPrgBankSelectorSplitsFunctionReference(t *testing.T) {
	prg := make([]program.Offset, 2)
	prg[0].Data = []byte{0x8A, 0xBF}
	prg[0].SetType(program.FunctionReference | program.JumpTable)
	prg[1].SetType(program.FunctionReference)
	bankAddress := 0xA000
	bankNumber := 1

	setPrgBankSelector(prg, 1, &bankAddress, &bankNumber)

	assert.Equal(t, []byte{0x8A}, prg[0].Data)
	assert.Equal(t, []byte{0xBF}, prg[1].Data)
	assert.False(t, prg[0].IsType(program.FunctionReference|program.JumpTable))
	assert.False(t, prg[1].IsType(program.FunctionReference|program.JumpTable))
	assert.True(t, prg[0].IsType(program.CodeAsData|program.DataOffset))
	assert.True(t, prg[1].IsType(program.CodeAsData|program.DataOffset))
	assert.NotNil(t, prg[1].WriteCallback)
}
