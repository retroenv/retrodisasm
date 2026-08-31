package m6502

import (
	"testing"

	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/assert"
)

func TestInstructionUnofficialRecognizesKIL(t *testing.T) {
	// KIL must remain assembler-independent even when dependency metadata omits its unofficial flag.
	instruction := Instruction{ins: cpu6502.KilInst}

	assert.True(t, instruction.Unofficial())
}
