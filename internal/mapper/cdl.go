package mapper

import (
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
)

// ApplyCodeDataLog applies code data log flags to mark code and entry points.
// It processes the CDL file data and marks bytes as code or data, and identifies
// subroutine entry points.
func (m *Mapper) ApplyCodeDataLog(prgFlags []codedatalog.PrgFlag) {
	if len(m.banks) == 0 {
		return
	}

	if m.bankWindowSize != 0x2000 {
		m.applyCodeDataLogSingleBank(prgFlags)
		return
	}

	m.applyCodeDataLogMultiBank(prgFlags)
}

func (m *Mapper) applyCodeDataLogSingleBank(prgFlags []codedatalog.PrgFlag) {
	bank0 := m.banks[0]
	for index, flags := range prgFlags {
		if index >= len(bank0.offsets) {
			return
		}

		if flags&codedatalog.Code != 0 {
			m.dis.AddAddressToParse(m.codeBaseAddress+uint16(index), 0, 0, nil, false)
		}
		if flags&codedatalog.SubEntryPoint != 0 {
			bank0.offsets[index].SetType(program.CallDestination)
		}
	}
}

func (m *Mapper) applyCodeDataLogMultiBank(prgFlags []codedatalog.PrgFlag) {
	currentBank := -1
	defer m.RestoreDefaultMapping()

	for index, flags := range prgFlags {
		bankIndex := index / 0x8000
		if bankIndex >= len(m.banks) {
			return
		}

		bankOffset := index % 0x8000
		bnk := m.banks[bankIndex]
		if bankOffset >= len(bnk.offsets) {
			continue
		}

		offsetInfo := bnk.offsets[bankOffset]
		if flags&codedatalog.SubEntryPoint != 0 {
			offsetInfo.SetType(program.CallDestination)
		}

		if flags&codedatalog.Code == 0 {
			continue
		}

		if currentBank != bankIndex {
			m.MapBank(bankIndex)
			currentBank = bankIndex
		}
		m.dis.AddAddressToParse(0x8000+uint16(bankOffset), 0, 0, nil, false)
	}
}
