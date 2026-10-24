package mapper

const (
	mmc1Control = iota
	mmc1PRG
	mmc1Shift
	mmc1Count
)

// mmc1 models standard MMC1 PRG wiring up to 256 KiB. Larger boards reuse CHR
// register bits for PRG address lines and require separate board models.
type mmc1 struct {
	size int
}

func (mm mmc1) BankSize() int { return 0x4000 }

func (mm mmc1) BusConflicts() bool { return false }

func (mm mmc1) Origin(bank int) uint16 {
	if bank == mm.size/0x4000-1 {
		return 0xc000
	}
	return 0x8000
}

func (mm mmc1) Seed(index int) Snapshot {
	return Snapshot{registers: [8]byte{0x0c, byte(index)}}
}

func (mm mmc1) SeedCount() int { return mm.size / 0x4000 }

func (mm mmc1) Windows(state Snapshot) []window {
	bank := int(state.registers[mmc1PRG]&0x0f) % (mm.size / 0x4000)
	switch (state.registers[mmc1Control] >> 2) & 3 {
	case 0, 1:
		return []window{{0x8000, 0x8000, (bank &^ 1) * 0x4000}}
	case 2:
		return []window{{0x8000, 0x4000, 0}, {0xc000, 0x4000, bank * 0x4000}}
	default:
		return []window{{0x8000, 0x4000, bank * 0x4000}, {0xc000, 0x4000, mm.size - 0x4000}}
	}
}

func (mm mmc1) Write(state Snapshot, address uint16, value byte) (Snapshot, bool) {
	if address < 0x8000 {
		return state, false
	}
	if value&0x80 != 0 {
		state.registers[mmc1Shift] = 0
		state.registers[mmc1Count] = 0
		state.registers[mmc1Control] |= 0x0c

		return state, true
	}
	state.registers[mmc1Shift] |= (value & 1) << state.registers[mmc1Count]
	state.registers[mmc1Count]++
	if state.registers[mmc1Count] == 5 {
		switch address & 0xe000 {
		case 0x8000:
			state.registers[mmc1Control] = state.registers[mmc1Shift] & 0x0c
		case 0xe000:
			state.registers[mmc1PRG] = state.registers[mmc1Shift] & byte(mm.size/0x4000-1)
		}

		state.registers[mmc1Shift] = 0
		state.registers[mmc1Count] = 0
	}

	return state, true
}
