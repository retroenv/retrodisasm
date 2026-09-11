package program

import (
	"fmt"
	"strings"
)

// OffsetType defines the type of a program offset.
type OffsetType uint16

// addressing modes.
const (
	UnknownOffset OffsetType = 0
	CodeOffset    OffsetType = 1 << iota
	DataOffset
	CodeAsData      // for branches into instructions and unofficial instructions
	CallDestination // opcode is the destination of a jsr call, indicating a subroutine
	JumpEngine
	JumpTable
	FunctionReference // reference to a function
	ExpressionData    // data emitted with validated symbolic byte/word expressions
)

// IsType returns whether the offset is of given type.
func (o *Offset) IsType(typ OffsetType) bool {
	ret := o.Type&typ != 0
	return ret
}

// SetType sets the type of the offset.
func (o *Offset) SetType(typ OffsetType) {
	o.Type |= typ
}

// ClearType unsets the type of the offset.
func (o *Offset) ClearType(typ OffsetType) {
	mask := ^(typ)
	o.Type &= mask
}

// HexCodeComment formats the offset data as hexadecimal string for code comments.
func (o *Offset) HexCodeComment() (string, error) {
	buf := &strings.Builder{}

	for _, b := range o.Data {
		if _, err := fmt.Fprintf(buf, "%02X ", b); err != nil {
			return "", fmt.Errorf("writing hex comment: %w", err)
		}
	}

	comment := strings.TrimRight(buf.String(), " ")
	return comment, nil
}
