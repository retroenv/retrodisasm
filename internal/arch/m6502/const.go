package m6502

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/register"
)

// Constants builds the map of all known NES constants from all
// modules that maps an address to a constant name.
func (ar *Arch6502) Constants() (map[uint16]consts.Constant, error) {
	m := map[uint16]consts.Constant{}
	if err := mergeConstantsMaps(m, register.APUAddressToName); err != nil {
		return nil, fmt.Errorf("processing apu constants: %w", err)
	}
	if err := mergeConstantsMaps(m, register.ControllerAddressToName); err != nil {
		return nil, fmt.Errorf("processing controller constants: %w", err)
	}
	if err := mergeConstantsMaps(m, register.PPUAddressToName); err != nil {
		return nil, fmt.Errorf("processing ppu constants: %w", err)
	}
	return m, nil
}

func mergeConstantsMaps(destination map[uint16]consts.Constant, source map[uint16]cpu6502.AccessModeConstant) error {
	for address, constantInfo := range source {
		translation := destination[address]
		translation.Address = address

		if constantInfo.Mode&cpu6502.ReadAccess != 0 {
			if translation.Read != "" {
				return fmt.Errorf("constant with address 0x%04X and read mode is defined twice", address)
			}
			translation.Read = constantInfo.Constant
		}

		if constantInfo.Mode&cpu6502.WriteAccess != 0 {
			if translation.Write != "" {
				return fmt.Errorf("constant with address 0x%04X and write mode is defined twice", address)
			}
			translation.Write = constantInfo.Constant
		}

		destination[address] = translation
	}
	return nil
}
