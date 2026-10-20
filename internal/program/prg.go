package program

import (
	"github.com/retroenv/retrodisasm/internal/options"
)

// PRGBank defines a PRG bank.
type PRGBank struct {
	Name        string
	BaseAddress uint16
	// ID and ROMOffset identify physical storage independently of the CPU origin.
	ID        int
	ROMOffset int

	Offsets    []Offset
	Vectors    [3]uint16
	HasVectors bool

	Constants map[string]uint16
	Variables map[string]uint16
}

// NewPRGBank creates a new PRG bank.
func NewPRGBank(size int) *PRGBank {
	return &PRGBank{
		Offsets:   make([]Offset, size),
		Constants: map[string]uint16{},
		Variables: map[string]uint16{},
	}
}

// LastNonZeroByte searches for the last byte in PRG that is not zero.
func (bank PRGBank) LastNonZeroByte(options options.Disassembler) int {
	vectorSpace := 0
	if bank.HasVectors && !options.Binary {
		vectorSpace = 6
	}

	endIndex := len(bank.Offsets) - vectorSpace
	if endIndex < 0 {
		endIndex = len(bank.Offsets)
	}

	if options.ZeroBytes {
		return endIndex
	}

	for i := endIndex - 1; i >= 0; i-- {
		offset := bank.Offsets[i]
		// Skip zero bytes that are not code instructions and have no label
		// BRK instruction (0x00) should be included if it's marked as CodeOffset
		isZeroByte := len(offset.Data) == 0 || offset.Data[0] == 0
		isCodeOrHasLabel := offset.IsType(CodeOffset|CodeAsData|ExpressionData) || offset.Label != "" ||
			offset.Comment != "" || offset.CommentBefore != "" || offset.BlankLines != nil

		if isZeroByte && !isCodeOrHasLabel {
			continue
		}
		return min(i+max(1, len(offset.Data)), endIndex)
	}

	return endIndex
}
