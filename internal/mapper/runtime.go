package mapper

type mmc1Runtime struct {
	shiftCount    byte
	shiftRegister byte

	control  byte
	chrBank0 int
	chrBank1 int
	prgBank  int
}

// RestoreMappingSignature restores a previously seen mapping snapshot by signature.
func (m *Mapper) RestoreMappingSignature(signature uint64) bool {
	if len(m.mappingSnapshots) == 0 {
		return false
	}

	snapshot, ok := m.mappingSnapshots[signature]
	if !ok || len(snapshot) != len(m.mapped) {
		return false
	}

	copy(m.mapped, snapshot)
	return true
}

// ApplyMapperWrite applies mapper register-space writes that can change PRG mapping.
// It returns true if the active mapping changed.
func (m *Mapper) ApplyMapperWrite(address uint16, value byte) bool {
	if m.bankWindowSize != 0x2000 || address < 0x8000 || len(m.banksMapped) == 0 {
		return false
	}

	before := m.MappingSignature()

	switch m.mapperNumber {
	case 1:
		m.applyMMC1Write(address, value)
	case 2:
		m.applyUxROMWrite(value)
	case 7:
		m.applyAxROMWrite(value)
	default:
		return false
	}

	after := m.MappingSignature()
	if before != after {
		m.rememberCurrentMapping()
		return true
	}
	return false
}

func (m *Mapper) initializeRuntimeState() {
	if m.mapperNumber == 1 {
		m.mmc1.control = 0x0C
	}
}

func (m *Mapper) rememberCurrentMapping() {
	if m.mappingSnapshots == nil {
		return
	}

	sig := m.MappingSignature()
	snapshot := make([]mappedBank, len(m.mapped))
	copy(snapshot, m.mapped)
	m.mappingSnapshots[sig] = snapshot
}

func (m *Mapper) applyAxROMWrite(value byte) {
	if len(m.banks) == 0 {
		return
	}
	// AxROM: bits 0-2 select the 32KB PRG bank at $8000-$FFFF.
	bankIndex := int(value & 0b0000_0111)
	bankIndex = modInt(bankIndex, len(m.banks))
	m.mapFull32KBank(bankIndex)
}

func (m *Mapper) applyUxROMWrite(value byte) {
	count16 := m.prg16KBankCount()
	if count16 == 0 {
		return
	}

	// UxROM: switchable 16KB at $8000-$BFFF, fixed last 16KB at $C000-$FFFF.
	selected16 := modInt(int(value), count16)
	m.map16KWindow(0x8000, selected16)
	m.map16KWindow(0xC000, count16-1)
}

func (m *Mapper) applyMMC1Write(address uint16, value byte) {
	if value&0x80 != 0 {
		m.mmc1.shiftCount = 0
		m.mmc1.shiftRegister = 0
		m.mmc1.control |= 0x0C
		m.applyMMC1Control()
		return
	}

	// MMC1 writes one low bit at a time into a 5-bit shift register.
	bit := (value & 1) << m.mmc1.shiftCount
	m.mmc1.shiftRegister |= bit
	m.mmc1.shiftCount++
	if m.mmc1.shiftCount < 5 {
		return
	}

	switch {
	case address < 0xA000:
		m.mmc1.control = m.mmc1.shiftRegister
	case address < 0xC000:
		m.mmc1.chrBank0 = int(m.mmc1.shiftRegister)
	case address < 0xE000:
		m.mmc1.chrBank1 = int(m.mmc1.shiftRegister)
	default:
		m.mmc1.prgBank = int(m.mmc1.shiftRegister) & 0b0000_1111
	}

	m.mmc1.shiftCount = 0
	m.mmc1.shiftRegister = 0
	m.applyMMC1Control()
}

func (m *Mapper) applyMMC1Control() {
	count16 := m.prg16KBankCount()
	if count16 == 0 {
		return
	}

	prgMode := (m.mmc1.control >> 2) & 0b0000_0011
	switch prgMode {
	case 0, 1:
		// Switch 32KB at $8000, low bit ignored.
		base16 := m.mmc1.prgBank &^ 1
		base16 = modInt(base16, count16)
		m.map16KWindow(0x8000, base16)
		m.map16KWindow(0xC000, base16+1)
	case 2:
		// Fix first 16KB at $8000, switch 16KB at $C000.
		m.map16KWindow(0x8000, 0)
		m.map16KWindow(0xC000, m.mmc1.prgBank)
	case 3:
		// Switch 16KB at $8000, fix last 16KB at $C000.
		m.map16KWindow(0x8000, m.mmc1.prgBank)
		m.map16KWindow(0xC000, count16-1)
	}
}

func (m *Mapper) mapFull32KBank(bankIndex int) {
	entries := m.mappedEntriesForBank(bankIndex)
	if len(entries) == 0 {
		return
	}
	for i, window := range prgWindowAddresses {
		m.setMappedBank(window, entries[i%len(entries)])
	}
}

func (m *Mapper) map16KWindow(startAddress uint16, bank16 int) {
	count16 := m.prg16KBankCount()
	if count16 == 0 {
		return
	}

	bank16 = modInt(bank16, count16)
	first8K := bank16 * 2
	m.setMappedBank(startAddress, m.banksMapped[first8K])
	m.setMappedBank(startAddress+0x2000, m.banksMapped[first8K+1])
}

func (m *Mapper) prg16KBankCount() int {
	return len(m.banksMapped) / 2
}

func modInt(value, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	value %= divisor
	if value < 0 {
		value += divisor
	}
	return value
}
