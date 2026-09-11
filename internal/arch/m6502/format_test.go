package m6502

import (
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestApplyOperand(t *testing.T) {
	// Formatting strings as hexadecimal previously turned symbol names into ASCII bytes.
	ar := New(log.NewTestLogger(t), parameter.New(parameter.Config{}))
	for _, test := range []struct {
		data       []byte
		expression string
		value      uint16
		want       string
	}{
		{[]byte{0xa9, 0x10}, "BootOffset", 0x10, "lda #BootOffset"},
		{[]byte{0xd0, 0xfc}, "Loop", 0x7ffe, "bne Loop"},
		{[]byte{0xbd, 0x10, 0x80}, "Table+1", 0x8010, "lda Table+1,X"},
		{[]byte{0xa9, 0x10}, "<Table+1", 0x10, "lda #<(Table+1)"},
	} {
		info := &offset.DisasmOffset{
			Offset: program.Offset{Data: test.data, Type: program.CodeOffset},
			Opcode: &Opcode{op: cpu6502.Opcodes[test.data[0]]},
		}
		assert.NoError(t, ar.ApplyOperand(0x8000, info, test.expression, test.value))
		assert.Equal(t, test.want, info.Code)
		assert.Error(t, ar.ApplyOperand(0x8000, info, test.expression, test.value+1))
		assert.False(t, strings.Contains(info.Code, "#$426F6F74"))
	}
	assert.Error(t, ar.ApplyOperand(0x8000, &offset.DisasmOffset{}, "Bad", 0))
}
