package m6502emu

import "github.com/retroenv/retrogolib/arch/system/nes/cartridge"

const (
	ramSize          = 0x0800
	prgRAMSize       = 0x2000
	ppuRegisterStart = 0x2000
	ppuRegisterMask  = 0x0007
	ppuCtrlRegister  = 0x2000
	ppuStatusVBlank  = 0x80

	joypad1Address = 0x4016
	joypad2Address = 0x4017

	joypadButtonCount = 8
	joypadOpenBusBit6 = 0x40
	joypadButtonAMask = 0x01
	joypadButtonData  = 0x01
)

type nesBus struct {
	cart   *cartridge.Cartridge
	mapper Mapper
	cfg    Config

	ram    [ramSize]byte
	prgRAM [prgRAMSize]byte

	ppuCtrl            byte
	ppuStatusReadCount uint64

	joypadStrobe   bool
	joypadLatched  [2]byte
	joypadShift    [2]byte
	joypadBitsLeft [2]uint8
	joypadSeqIndex [2]uint32

	onMapperWrite func(address uint16, value byte)
}

type busSnapshot struct {
	ram    [ramSize]byte
	prgRAM [prgRAMSize]byte

	ppuCtrl            byte
	ppuStatusReadCount uint64
	joypadStrobe       bool
	joypadLatched      [2]byte
	joypadShift        [2]byte
	joypadBitsLeft     [2]uint8
	joypadSeqIndex     [2]uint32
}

func newNesBus(cart *cartridge.Cartridge, mapper Mapper, cfg Config) *nesBus {
	return &nesBus{
		cart:   cart,
		mapper: mapper,
		cfg:    cfg,
	}
}

func (b *nesBus) snapshot() busSnapshot {
	return busSnapshot{
		ram:                b.ram,
		prgRAM:             b.prgRAM,
		ppuCtrl:            b.ppuCtrl,
		ppuStatusReadCount: b.ppuStatusReadCount,
		joypadStrobe:       b.joypadStrobe,
		joypadLatched:      b.joypadLatched,
		joypadShift:        b.joypadShift,
		joypadBitsLeft:     b.joypadBitsLeft,
		joypadSeqIndex:     b.joypadSeqIndex,
	}
}

func (b *nesBus) restore(state busSnapshot) {
	b.ram = state.ram
	b.prgRAM = state.prgRAM
	b.ppuCtrl = state.ppuCtrl
	b.ppuStatusReadCount = state.ppuStatusReadCount
	b.joypadStrobe = state.joypadStrobe
	b.joypadLatched = state.joypadLatched
	b.joypadShift = state.joypadShift
	b.joypadBitsLeft = state.joypadBitsLeft
	b.joypadSeqIndex = state.joypadSeqIndex
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
		case joypad1Address:
			return b.readJoypad(0)
		case joypad2Address:
			return b.readJoypad(1)
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
		register := ppuRegisterStart + (address & ppuRegisterMask)
		if register == ppuCtrlRegister {
			b.ppuCtrl = value
		}
		// Other PPU register writes are ignored in advisory mode.
		return

	case address <= 0x401F:
		if address == joypad1Address {
			b.writeJoypadStrobe(value)
			return
		}
		// APU/IO writes (besides controller strobe) are ignored in advisory mode.
		return

	case address <= 0x5FFF:
		// Mapper-controlled expansion registers (e.g., MMC5 $5100-$5117) live here.
		// Forward writes so runtime PRG mapping can react.
		if b.onMapperWrite != nil && address >= 0x5000 {
			b.onMapperWrite(address, value)
		}
		return

	case address <= 0x7FFF:
		b.prgRAM[address-0x6000] = value

	default:
		if b.onMapperWrite != nil {
			b.onMapperWrite(address, value)
		}
	}
}

func (b *nesBus) ppuNMIEnabled() bool {
	return b.ppuCtrl&ppuStatusVBlank != 0
}

func (b *nesBus) writeJoypadStrobe(value byte) {
	newStrobe := value&joypadButtonData != 0
	if newStrobe {
		// While strobe is high, read A directly from current state, but do not
		// advance scripted timeline until the poll cycle is latched on high->low.
		b.latchJoypads(false)
	} else if b.joypadStrobe {
		// Latch on high->low transition, matching CPU polling behavior.
		b.latchJoypads(true)
	}
	b.joypadStrobe = newStrobe
}

func (b *nesBus) latchJoypads(advanceSequence bool) {
	for controller := 0; controller < len(b.joypadLatched); controller++ {
		state := b.sampleJoypadState(controller)
		b.joypadLatched[controller] = state
		b.joypadShift[controller] = state
		b.joypadBitsLeft[controller] = joypadButtonCount
		if advanceSequence {
			b.advanceJoypadSequence(controller)
		}
	}
}

func (b *nesBus) sampleJoypadState(controller int) byte {
	// State bit order: A, B, Select, Start, Up, Down, Left, Right.
	if controller < 0 || controller > 1 {
		return 0
	}
	sequence := b.joypadSequence(controller)
	if len(sequence) > 0 {
		index := int(b.joypadSeqIndex[controller])
		if index >= len(sequence) {
			index = len(sequence) - 1
		}
		return sequence[index]
	}
	return b.defaultJoypadState(controller)
}

func (b *nesBus) defaultJoypadState(controller int) byte {
	if controller == 0 {
		return b.cfg.Joypad1State
	}
	return b.cfg.Joypad2State
}

func (b *nesBus) joypadSequence(controller int) []byte {
	if controller == 0 {
		return b.cfg.Joypad1Sequence
	}
	return b.cfg.Joypad2Sequence
}

func (b *nesBus) advanceJoypadSequence(controller int) {
	sequence := b.joypadSequence(controller)
	if len(sequence) == 0 {
		return
	}
	if int(b.joypadSeqIndex[controller]) < len(sequence)-1 {
		b.joypadSeqIndex[controller]++
	}
}

func (b *nesBus) readJoypad(controller int) byte {
	if controller < 0 || controller > 1 {
		return joypadOpenBusBit6
	}

	if b.joypadStrobe {
		return joypadOpenBusBit6 | (b.joypadLatched[controller] & joypadButtonAMask)
	}

	var bit byte = 1
	if b.joypadBitsLeft[controller] > 0 {
		bit = b.joypadShift[controller] & joypadButtonAMask
		b.joypadShift[controller] >>= 1
		b.joypadBitsLeft[controller]--
	}

	return joypadOpenBusBit6 | bit
}
