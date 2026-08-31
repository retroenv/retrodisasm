package assembler

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestVectorReferencesUsesLiteralsForInvalidHandlers(t *testing.T) {
	bank := program.NewPRGBank(0x4000)
	bank.BaseAddress = 0xC000
	bank.Vectors = [3]uint16{0xC100, 0x4B00, 0xFFFF}
	handlers := program.Handlers{NMI: "NMI", Reset: "0", IRQ: "IRQ"}

	nmi, reset, irq := VectorReferences(bank, handlers)

	assert.Equal(t, "NMI", nmi)
	assert.Equal(t, "$4B00", reset)
	assert.Equal(t, "$FFFF", irq)
}
