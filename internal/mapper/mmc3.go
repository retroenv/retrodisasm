package mapper

const (
	mmc3Select = iota
	mmc3R6
	mmc3R7
)

type mmc3 struct {
	size int
}

func (mm mmc3) BankSize() int { return 0x2000 }

func (mm mmc3) BusConflicts() bool { return false }

func (mm mmc3) Origin(bank int) uint16 {
	switch bank {
	case mm.size/0x2000 - 1:
		return 0xe000
	case mm.size/0x2000 - 2:
		return 0xc000
	default:
		return 0x8000
	}
}

func (mm mmc3) Seed(index int) Snapshot {
	// Cover each independently selectable bank without a Cartesian product.
	// Register writes discover combinations that the traced program requests.
	count := mm.size / 0x2000
	state := Snapshot{registers: [8]byte{0, 0, 1}}
	if index < count {
		state.registers[mmc3R6] = byte(index)
	} else {
		state.registers[mmc3R7] = byte(index - count)
	}
	return state
}

func (mm mmc3) SeedCount() int { return 2 * (mm.size / 0x2000) }

func (mm mmc3) Windows(state Snapshot) []window {
	first := int(state.registers[mmc3R6]) * 0x2000
	third := mm.size - 0x4000
	if state.registers[mmc3Select]&0x40 != 0 {
		first, third = third, first
	}
	return []window{
		{0x8000, 0x2000, first},
		{0xa000, 0x2000, int(state.registers[mmc3R7]) * 0x2000},
		{0xc000, 0x2000, third},
		{0xe000, 0x2000, mm.size - 0x2000},
	}
}

func (mm mmc3) Write(state Snapshot, address uint16, value byte) (Snapshot, bool) {
	switch address & 0xe001 {
	case 0x8000:
		state.registers[mmc3Select] = value & 0x47

	case 0x8001:
		value &= byte(mm.size/0x2000 - 1)
		switch state.registers[mmc3Select] & 7 {
		case 6:
			state.registers[mmc3R6] = value
		case 7:
			state.registers[mmc3R7] = value
		}

	default:
		return state, false
	}
	return state, true
}
