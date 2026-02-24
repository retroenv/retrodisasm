package m6502emu

import "github.com/retroenv/retrogolib/arch/system/nes/cartridge"

const (
	ramSize          = 0x0800
	prgRAMSize       = 0x2000
	ppuRegisterStart = 0x2000
	ppuRegisterMask  = 0x0007
	ppuStatusVBlank  = 0x80
)

type nesBus struct {
	cart   *cartridge.Cartridge
	mapper Mapper

	ram    [ramSize]byte
	prgRAM [prgRAMSize]byte

	ppuStatusReadCount uint64

	onMapperWrite func(address uint16, value byte)
}

type busSnapshot struct {
	ram    [ramSize]byte
	prgRAM [prgRAMSize]byte

	ppuStatusReadCount uint64
}

func newNesBus(cart *cartridge.Cartridge, mapper Mapper) *nesBus {
	return &nesBus{
		cart:   cart,
		mapper: mapper,
	}
}

func (b *nesBus) snapshot() busSnapshot {
	return busSnapshot{
		ram:                b.ram,
		prgRAM:             b.prgRAM,
		ppuStatusReadCount: b.ppuStatusReadCount,
	}
}

func (b *nesBus) restore(state busSnapshot) {
	b.ram = state.ram
	b.prgRAM = state.prgRAM
	b.ppuStatusReadCount = state.ppuStatusReadCount
}

func (b *nesBus) Read(address uint16) uint8 {
	switch {
	case address <= 0x1FFF:
		return b.ram[address&0x07FF]

	case address <= 0x3FFF:
		register := ppuRegisterStart + (address & ppuRegisterMask)
		if register == 0x2002 {
			return b.readPPUStatus()
		}
		return 0x00

	case address <= 0x401F:
		switch address {
		case 0x4016, 0x4017:
			return 0x00 // controller input stub
		default:
			return 0x00 // APU/IO stubs
		}

	case address <= 0x5FFF:
		return 0x00 // open bus / expansion area

	case address <= 0x7FFF:
		return b.prgRAM[address-0x6000]

	default:
		return b.mapper.ReadMemory(address)
	}
}

func (b *nesBus) readPPUStatus() byte {
	// Alternate clear/set vblank to emulate phase transitions and avoid hard-wiring one state.
	// This keeps startup wait loops progressing in advisory mode while remaining deterministic.
	value := byte(0x00)
	if b.ppuStatusReadCount%2 == 1 {
		value = ppuStatusVBlank
	}
	b.ppuStatusReadCount++
	return value
}

func (b *nesBus) Write(address uint16, value uint8) {
	switch {
	case address <= 0x1FFF:
		b.ram[address&0x07FF] = value

	case address <= 0x3FFF:
		// PPU register writes are ignored in advisory mode.
		return

	case address <= 0x401F:
		// APU/IO writes are ignored in advisory mode.
		return

	case address <= 0x5FFF:
		// Expansion area writes ignored.
		return

	case address <= 0x7FFF:
		b.prgRAM[address-0x6000] = value

	default:
		if b.onMapperWrite != nil {
			b.onMapperWrite(address, value)
		}
	}
}
