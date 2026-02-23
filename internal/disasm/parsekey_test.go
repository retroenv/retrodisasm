package disasm

import (
	"context"
	"testing"

	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/assembler/ca65"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
	"github.com/retroenv/retrogolib/set"
)

func TestAddAddressToParse_AllowsSameAddressAcrossMappings(t *testing.T) {
	dis := newParseKeyTestDisasm(t, newTwoBankParseKeyCart())
	resetParseQueues(dis)

	dis.AddAddressToParse(0xC000, 0, 0, nil, false)
	defaultSignature := dis.mapper.MappingSignature()

	dis.mapper.MapBank(0)
	mappedSignature := dis.mapper.MappingSignature()
	assert.True(t, defaultSignature != mappedSignature)

	dis.AddAddressToParse(0xC000, 0, 0, nil, false)

	assert.Equal(t, 2, len(dis.offsetsToParseAdded))
	assert.Equal(t, 2, len(dis.offsetsToParse))
}

func TestFollowExecutionFlow_ParsesSamePCAcrossMappings(t *testing.T) {
	dis := newParseKeyTestDisasm(t, newTwoBankParseKeyCart())
	resetParseQueues(dis)

	dis.AddAddressToParse(0xC000, 0, 0, nil, false)
	defaultKey := dis.currentParseKey(0xC000)

	err := dis.followExecutionFlow(context.Background())
	assert.NoError(t, err)
	assert.True(t, dis.offsetsParsed.Contains(defaultKey))

	dis.mapper.MapBank(0)
	mappedKey := dis.currentParseKey(0xC000)
	assert.True(t, mappedKey.MappingID != defaultKey.MappingID)

	dis.AddAddressToParse(0xC000, 0, 0, nil, false)
	err = dis.followExecutionFlow(context.Background())
	assert.NoError(t, err)
	assert.True(t, dis.offsetsParsed.Contains(mappedKey))

	samePCCounter := 0
	for key := range dis.offsetsParsed {
		if key.PC == 0xC000 {
			samePCCounter++
		}
	}
	assert.Equal(t, 2, samePCCounter)
}

func newParseKeyTestDisasm(t *testing.T, cart *cartridge.Cartridge) *Disasm {
	t.Helper()

	opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
	opts.CodeOnly = true
	opts.OffsetComments = false
	opts.HexComments = false

	logger := log.NewTestLogger(t)
	ar := m6502.New(logger, parameter.New(ca65.ParamConfig))
	ar.SetOptions(opts)

	dis, err := New(logger, ar, cart, opts, ca65.New)
	assert.NoError(t, err)
	return dis
}

func newTwoBankParseKeyCart() *cartridge.Cartridge {
	cart := cartridge.New()
	cart.PRG = make([]byte, 0x10000) // 2 x 32KB PRG banks

	// Default mapping executes from CPU $C000 backed by bank 1.
	cart.PRG[0xC000] = 0x40 // RTI

	// When bank 0 is fully mapped via MapBank(0), CPU $C000 points to bank0+$4000.
	cart.PRG[0x4000] = 0x40 // RTI

	// Reset vector in the last bank -> $C000.
	cart.PRG[0xFFFC] = 0x00
	cart.PRG[0xFFFD] = 0xC0

	return cart
}

func resetParseQueues(dis *Disasm) {
	dis.offsetsToParse = nil
	dis.offsetsToParseAdded = set.New[ParseKey]()
	dis.offsetsParsed = set.New[ParseKey]()
	dis.functionReturnsToParse = nil
	dis.functionReturnsToParseAdded = set.New[ParseKey]()
}
