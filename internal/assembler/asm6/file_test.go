package asm6

import (
	"bytes"
	"io"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestWriteEmitsOutOfBankBranchAsBytes(t *testing.T) {
	bank := program.NewPRGBank(9)
	bank.BaseAddress = 0xFFF7
	bank.Offsets[0] = program.Offset{
		Address: 0xFFF7,
		Code:    "bmi $0029",
		Data:    []byte{0x30, 0x30},
		Type:    program.CodeOffset,
	}
	app := &program.Program{PRG: []*program.PRGBank{bank}}

	var output bytes.Buffer
	newBankWriter := func(_ string) (io.WriteCloser, error) {
		return testWriteCloser{Writer: &output}, nil
	}
	fileWriter := New(app, options.NewDisassembler("asm6", "nes"), &output, newBankWriter)

	assert.NoError(t, fileWriter.Write())
	assert.Contains(t, output.String(), ".byte $30, $30")
	assert.NotContains(t, output.String(), "bmi $0029")
}

type testWriteCloser struct {
	io.Writer
}

func (testWriteCloser) Close() error { return nil }
