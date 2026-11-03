package quality

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestExpansionRegisterWarnings(t *testing.T) {
	for _, test := range []struct {
		address uint16
		mapper  uint16
		unused  int
	}{
		{
			address: 0x4100,
			mapper:  79,
		},
		{
			address: 0x5fff,
			mapper:  79,
		},
		{
			address: 0x5000,
			mapper:  111,
		},
		{
			address: 0x5fff,
			mapper:  111,
		},
		{
			address: 0x5000,
			mapper:  79,
			unused:  1,
		},
		{
			address: 0x4fff,
			mapper:  111,
			unused:  1,
		},
		{
			address: 0x4018,
			mapper:  111,
			unused:  1,
		},
		{
			address: 0x5000,
			unused:  1,
		},
	} {
		bank := testBank(code(0x8d, byte(test.address), byte(test.address>>8)))
		report := Analyze(&program.Program{
			PRG:    []*program.PRGBank{bank},
			Mapper: test.mapper,
		})
		assert.Equal(t, test.unused, report.Reasons[UnusedAddress], "mapper %d address %04x", test.mapper, test.address)
	}
}
