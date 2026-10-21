package mapper

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
)

const (
	singleBankName        = "CODE"
	multiBankNameTemplate = "PRG_BANK_%d"
)

var _ offset.MappedBank = mappedBank{}

type storage struct {
	prg       []byte
	romOffset int
	id        int
}

// bank is an analysis view over immutable physical ROM storage.
type bank struct {
	*storage

	baseAddress uint16
	hasVectors  bool
	mapped      bool

	offsets []*offset.DisasmOffset
}

type mappedBank struct {
	bank      *bank
	id        int
	dataStart int
}

func newBank(prg []byte) *bank {
	b := &bank{
		storage: &storage{prg: prg},
	}
	return b
}

func (bnk *bank) initializeOffsets() {
	if bnk.offsets != nil {
		return
	}
	bnk.offsets = make([]*offset.DisasmOffset, len(bnk.prg))
	storage := make([]offset.DisasmOffset, len(bnk.prg))
	for i := range bnk.offsets {
		bnk.offsets[i] = &storage[i]
	}
}

func (m mappedBank) OffsetInfo(index uint16) *offset.DisasmOffset {
	offset := int(index) + m.dataStart
	offsetInfo := m.bank.offsets[offset]
	return offsetInfo
}

func (m mappedBank) ID() int {
	return m.id
}

func setBankVectors(bnk *bank, prgBank *program.PRGBank) {
	// Skip vector extraction for small binaries (less than 6 bytes)
	if !bnk.hasVectors || len(bnk.prg) < 6 {
		return
	}

	idx := len(bnk.prg) - 6
	for i := range 3 {
		b1 := bnk.prg[idx]
		idx++
		b2 := bnk.prg[idx]
		idx++
		addr := uint16(b2)<<8 | uint16(b1)
		prgBank.Vectors[i] = addr
	}
}

func setBankName(prgBank *program.PRGBank, bnkIndex, numBanks int) {
	if bnkIndex == 0 && numBanks == 1 {
		prgBank.Name = singleBankName
		return
	}

	prgBank.Name = fmt.Sprintf(multiBankNameTemplate, bnkIndex)
}
