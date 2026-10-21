package mapper

type banking uint8

const (
	fixed banking = iota
	switchLower
	switchWhole
)

type simpleModel struct {
	size int
	mode banking
}

func (sm simpleModel) BankSize() int {
	if sm.mode == fixed {
		return sm.size
	}
	if sm.mode == switchLower {
		return 0x4000
	}
	return 0x8000
}

func (sm simpleModel) BusConflicts() bool { return sm.mode != fixed }

func (sm simpleModel) Origin(bank int) uint16 {
	if sm.mode == switchLower && bank == sm.SeedCount()-1 || sm.size == 0x4000 {
		return 0xc000
	}
	return 0x8000
}

func (sm simpleModel) Seed(index int) Snapshot {
	return Snapshot{registers: [8]byte{byte(index), byte(index >> 8)}}
}

func (sm simpleModel) SeedCount() int {
	return sm.size / sm.BankSize()
}

func (sm simpleModel) Windows(state Snapshot) []window {
	off := (int(state.registers[0]) | int(state.registers[1])<<8) * sm.BankSize()
	switch sm.mode {
	case switchLower:
		return []window{{0x8000, 0x4000, off}, {0xc000, 0x4000, sm.size - 0x4000}}
	case switchWhole:
		return []window{{0x8000, 0x8000, off}}

	default:
		windows := []window{{0x8000, sm.size, 0}}
		if sm.size == 0x4000 {
			windows = append(windows, window{0xc000, sm.size, 0})
		}
		return windows
	}
}

func (sm simpleModel) Write(state Snapshot, address uint16, value byte) (Snapshot, bool) {
	if sm.mode == fixed || address < 0x8000 {
		return state, false
	}
	if sm.mode == switchWhole {
		value &= 7
	}
	return sm.Seed(int(value) % sm.SeedCount()), true
}
