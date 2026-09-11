package program

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/assert"
)

func TestLastNonZeroBytePreservesComment(t *testing.T) {
	// A comment on a trailing zero byte must keep that byte in the output.
	bank := NewPRGBank(16)
	bank.Offsets[0] = Offset{Data: []byte{0x60}, Type: CodeOffset}
	bank.Offsets[8] = Offset{Data: []byte{0}, Type: DataOffset, Comment: "Annotated zero"}
	assert.Equal(t, 9, bank.LastNonZeroByte(options.Disassembler{}))
}

func TestLastNonZeroBytePreservesExpressionLength(t *testing.T) {
	// A grouped zero-valued word must retain both of its bytes.
	bank := NewPRGBank(16)
	bank.Offsets[6] = Offset{Data: []byte{0, 0}, Type: DataOffset | ExpressionData}
	assert.Equal(t, 8, bank.LastNonZeroByte(options.Disassembler{}))
}

func TestLastNonZeroBytePreservesPresentation(t *testing.T) {
	for _, withComment := range []bool{false, true} {
		bank := NewPRGBank(16)
		bank.Offsets[0] = Offset{Data: []byte{0x60}, Type: CodeOffset}
		bank.Offsets[8] = Offset{Data: []byte{0}, Type: DataOffset}
		if withComment {
			bank.Offsets[8].CommentBefore = "Trailing table"
		} else {
			count := 0
			bank.Offsets[8].BlankLines = &count
		}
		assert.Equal(t, 9, bank.LastNonZeroByte(options.Disassembler{}))
	}
}
