package assembler

import (
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestFormatExpression(t *testing.T) {
	// NESASM uses LOW/HIGH functions, not the unary selectors used by ca65 and asm6.
	assert.Equal(t, "LOW(Table+1)", FormatExpression(Nesasm, "<Table+1"))
	assert.Equal(t, "HIGH(Table)", FormatExpression(Nesasm, ">Table"))
	assert.Equal(t, "<(Table)", FormatExpression(Ca65, "<Table"))
	assert.Equal(t, ">(Table)", FormatExpression(Asm6, ">Table"))
	assert.Equal(t, "Table+1", FormatExpression(Nesasm, "Table+1"))
}
