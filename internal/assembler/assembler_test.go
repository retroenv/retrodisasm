package assembler

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestVectorReferences(t *testing.T) {
	tests := []struct {
		name        string
		vectors     [3]uint16
		handlers    program.Handlers
		definitions map[uint16]program.Offset
		want        [3]string
	}{
		{
			name:     "emitted handler labels",
			vectors:  [3]uint16{0xC100, 0xC200, 0xC300},
			handlers: program.Handlers{NMI: "NMI", Reset: "Reset", IRQ: "IRQ"},
			definitions: map[uint16]program.Offset{
				0xC100: {Label: "NMI", Data: []byte{0x40}},
				0xC200: {Label: "Reset", Data: []byte{0x78}},
				0xC300: {Label: "IRQ", Data: []byte{0x40}},
			},
			want: [3]string{"NMI", "Reset", "IRQ"},
		},
		{
			name:     "addresses outside code range",
			vectors:  [3]uint16{0xBFFF, 0xFFFA, 0xFFFF},
			handlers: program.Handlers{NMI: "NMI", Reset: "Reset", IRQ: "IRQ"},
			want:     [3]string{"$BFFF", "$FFFA", "$FFFF"},
		},
		{
			name:     "labels without emitted definitions",
			vectors:  [3]uint16{0xC100, 0xC200, 0xC300},
			handlers: program.Handlers{NMI: "NMI", Reset: "Reset", IRQ: "IRQ"},
			definitions: map[uint16]program.Offset{
				0xC100: {Label: "Different", Data: []byte{0x40}},
				0xC200: {Label: "Reset"},
				0xC300: {Data: []byte{0x40}},
			},
			want: [3]string{"$C100", "$C200", "$C300"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bank := program.NewPRGBank(0x4000)
			bank.BaseAddress = 0xC000
			bank.Vectors = tt.vectors
			for address, definition := range tt.definitions {
				bank.Offsets[address-bank.BaseAddress] = definition
			}

			nmi, reset, irq := VectorReferences(bank, tt.handlers)

			assert.Equal(t, tt.want[0], nmi)
			assert.Equal(t, tt.want[1], reset)
			assert.Equal(t, tt.want[2], irq)
		})
	}
}
