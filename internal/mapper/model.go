package mapper

import (
	"fmt"

	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

type window struct {
	address   uint16
	size      int
	romOffset int
}

// model separates hardware register behavior from storage and trace scheduling.
// Seeds are representative static mappings, not hardware power-on guarantees.
type model interface {
	BankSize() int
	BusConflicts() bool
	Origin(bank int) uint16
	Seed(index int) Snapshot
	SeedCount() int
	Windows(Snapshot) []window
	Write(Snapshot, uint16, byte) (Snapshot, bool)
}

func nesLayout(cart *cartridge.Cartridge) (model, error) { //nolint:ireturn
	size := len(cart.PRG)
	switch cart.Mapper {
	case 0, 3:
		if size != 0x4000 && size != 0x8000 {
			return nil, fmt.Errorf("invalid PRG size %d for mapper %d: expected 16 or 32 KiB", size, cart.Mapper)
		}
		return simpleModel{
			size: size,
			mode: fixed,
		}, nil

	case 2:
		if size < 0x8000 || size%0x4000 != 0 {
			return nil, fmt.Errorf("invalid UNROM PRG size %d: expected at least two complete 16 KiB banks", size)
		}
		return simpleModel{
			size: size,
			mode: switchLower,
		}, nil

	case 7:
		if size == 0 || size%0x8000 != 0 {
			return nil, fmt.Errorf("invalid AxROM PRG size %d: expected complete 32 KiB banks", size)
		}
		return simpleModel{
			size: size,
			mode: switchWhole,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported NES mapper %d: no PRG mapping model", cart.Mapper)
	}
}
