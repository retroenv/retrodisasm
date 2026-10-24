package mapper

import (
	"slices"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
)

// ApplyCodeDataLog applies code data log flags to mark code and entry points.
// It processes the CDL file data and marks bytes as code or data, and identifies
// subroutine entry points.
func (m *Mapper) ApplyCodeDataLog(prgFlags []codedatalog.PrgFlag) {
	for _, bnk := range slices.Concat(m.banks, m.views) {
		end := min(bnk.romOffset+len(bnk.prg), len(prgFlags))
		if !bnk.mapped {
			continue
		}
		for prgIndex := bnk.romOffset; prgIndex < end; prgIndex++ {
			index := prgIndex - bnk.romOffset
			flags := prgFlags[prgIndex]
			if flags&codedatalog.SubEntryPoint != 0 {
				bnk.offsets[index].SetType(program.CallDestination)
			}

			if flags&codedatalog.Code != 0 {
				if address, ok := m.mappedAddress(bnk, index); ok {
					m.dis.AddAddressToParse(address, 0, 0, nil, false)
				}
			}
		}
	}
}

// mappedAddress resolves a physical ROM offset only when its bank is mapped.
func (m *Mapper) mappedAddress(bnk *bank, index int) (uint16, bool) {
	if m.bankWindowSize == 0 {
		return m.codeBaseAddress + uint16(index), true
	}

	for window, mapped := range m.mapped {
		if mapped.bank != bnk || index < mapped.dataStart || index >= mapped.dataStart+m.bankWindowSize {
			continue
		}
		address := uint16(window*m.bankWindowSize + index - mapped.dataStart)
		if address >= m.codeBaseAddress {
			return address, true
		}
	}

	return 0, false
}
