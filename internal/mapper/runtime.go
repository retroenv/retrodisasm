package mapper

type mmc1Runtime struct {
	shiftCount    byte
	shiftRegister byte

	control  byte
	chrBank0 int
	chrBank1 int
	prgBank  int
}

type mmc5Runtime struct {
	prgMode byte
	prgRegs [5]byte // $5113-$5117
}

type runtimeSnapshot struct {
	MappingSignature uint64

	MMC1ShiftCount    byte
	MMC1ShiftRegister byte
	MMC1Control       byte
	MMC1CHRBank0      int
	MMC1CHRBank1      int
	MMC1PRGBank       int

	MMC5PRGMode byte
	MMC5PRGRegs [5]byte
}

// SnapshotRuntimeState returns an opaque snapshot of mapper runtime state.
func (m *Mapper) SnapshotRuntimeState() any {
	return runtimeSnapshot{
		MappingSignature: m.MappingSignature(),

		MMC1ShiftCount:    m.mmc1.shiftCount,
		MMC1ShiftRegister: m.mmc1.shiftRegister,
		MMC1Control:       m.mmc1.control,
		MMC1CHRBank0:      m.mmc1.chrBank0,
		MMC1CHRBank1:      m.mmc1.chrBank1,
		MMC1PRGBank:       m.mmc1.prgBank,

		MMC5PRGMode: m.mmc5.prgMode,
		MMC5PRGRegs: m.mmc5.prgRegs,
	}
}

// RestoreRuntimeState restores a mapper runtime snapshot previously returned by SnapshotRuntimeState.
func (m *Mapper) RestoreRuntimeState(snapshot any) bool {
	state, ok := snapshot.(runtimeSnapshot)
	if !ok {
		return false
	}

	if !m.RestoreMappingSignature(state.MappingSignature) {
		return false
	}

	m.mmc1.shiftCount = state.MMC1ShiftCount
	m.mmc1.shiftRegister = state.MMC1ShiftRegister
	m.mmc1.control = state.MMC1Control
	m.mmc1.chrBank0 = state.MMC1CHRBank0
	m.mmc1.chrBank1 = state.MMC1CHRBank1
	m.mmc1.prgBank = state.MMC1PRGBank

	m.mmc5.prgMode = state.MMC5PRGMode
	m.mmc5.prgRegs = state.MMC5PRGRegs
	return true
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
	if m.bankWindowSize != 0x2000 || len(m.banksMapped) == 0 {
		return false
	}
	if m.mapperNumber != 5 && address < 0x8000 {
		return false
	}

	before := m.MappingSignature()

	switch m.mapperNumber {
	case 1:
		m.applyMMC1Write(address, value)
	case 2:
		m.applyUxROMWrite(value)
	case 5:
		m.applyMMC5Write(address, value)
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
	if m.mapperNumber == 5 {
		m.mmc5.prgMode = 3
		m.seedMMC5PRGRegistersFromCurrentMapping()
	}
}

func (m *Mapper) seedMMC5PRGRegistersFromCurrentMapping() {
	if len(m.banksMapped) == 0 || len(m.mapped) == 0 {
		return
	}

	idx8000 := m.mappedBank8KIndex(0x8000)
	idxA000 := m.mappedBank8KIndex(0xA000)
	idxC000 := m.mappedBank8KIndex(0xC000)
	idxE000 := m.mappedBank8KIndex(0xE000)

	// Register layout: $5113-$5117 => prgRegs[0..4]
	// Keep ROM bank mappings stable on first $5100 mode writes.
	m.mmc5.prgRegs[1] = 0x80 | byte(modInt(idx8000, len(m.banksMapped)))
	m.mmc5.prgRegs[2] = 0x80 | byte(modInt(idxA000, len(m.banksMapped)))
	m.mmc5.prgRegs[3] = 0x80 | byte(modInt(idxC000, len(m.banksMapped)))
	m.mmc5.prgRegs[4] = 0x80 | byte(modInt(idxE000, len(m.banksMapped)))
}

func (m *Mapper) mappedBank8KIndex(address uint16) int {
	if m.bankWindowSize != 0x2000 || len(m.mapped) == 0 {
		return 0
	}
	bankWindow := int(address >> m.addressShifts)
	if bankWindow < 0 || bankWindow >= len(m.mapped) {
		return 0
	}
	mapped := m.mapped[bankWindow]

	for idx, entry := range m.banksMapped {
		if entry.id != mapped.id {
			continue
		}
		if entry.dataStart == mapped.dataStart {
			return idx
		}
	}
	for idx, entry := range m.banksMapped {
		if entry.id == mapped.id {
			return idx
		}
	}
	return 0
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

func (m *Mapper) applyMMC5Write(address uint16, value byte) {
	switch address {
	case 0x5100:
		m.mmc5.prgMode = value & 0x03
		m.applyMMC5PRGMapping()
	case 0x5113:
		m.mmc5.prgRegs[0] = value
	case 0x5114:
		m.mmc5.prgRegs[1] = value
		m.applyMMC5PRGMapping()
	case 0x5115:
		m.mmc5.prgRegs[2] = value
		m.applyMMC5PRGMapping()
	case 0x5116:
		m.mmc5.prgRegs[3] = value
		m.applyMMC5PRGMapping()
	case 0x5117:
		m.mmc5.prgRegs[4] = value
		m.applyMMC5PRGMapping()
	}
}

func (m *Mapper) applyMMC5PRGMapping() {
	if len(m.banksMapped) == 0 {
		return
	}

	map8k := func(window uint16, reg byte, forceROM bool) {
		bank8k, ok := m.mmc5ROMBankIndex(reg, forceROM)
		if !ok {
			return
		}
		m.setMappedBank(window, m.banksMapped[bank8k])
	}

	map16k := func(window uint16, reg byte, forceROM bool) {
		bank8k, ok := m.mmc5ROMBankIndex(reg, forceROM)
		if !ok {
			return
		}
		base := bank8k &^ 1
		m.setMappedBank(window, m.banksMapped[base])
		m.setMappedBank(window+0x2000, m.banksMapped[modInt(base+1, len(m.banksMapped))])
	}

	switch m.mmc5.prgMode & 0x03 {
	case 0:
		bank8k, ok := m.mmc5ROMBankIndex(m.mmc5.prgRegs[4], true)
		if !ok {
			return
		}
		base := bank8k &^ 3
		m.setMappedBank(0x8000, m.banksMapped[base])
		m.setMappedBank(0xA000, m.banksMapped[modInt(base+1, len(m.banksMapped))])
		m.setMappedBank(0xC000, m.banksMapped[modInt(base+2, len(m.banksMapped))])
		m.setMappedBank(0xE000, m.banksMapped[modInt(base+3, len(m.banksMapped))])
	case 1:
		map16k(0x8000, m.mmc5.prgRegs[2], false)
		map16k(0xC000, m.mmc5.prgRegs[4], true)
	case 2:
		map16k(0x8000, m.mmc5.prgRegs[2], false)
		map8k(0xC000, m.mmc5.prgRegs[3], false)
		map8k(0xE000, m.mmc5.prgRegs[4], true)
	case 3:
		map8k(0x8000, m.mmc5.prgRegs[1], false)
		map8k(0xA000, m.mmc5.prgRegs[2], false)
		map8k(0xC000, m.mmc5.prgRegs[3], false)
		map8k(0xE000, m.mmc5.prgRegs[4], true)
	}
}

func (m *Mapper) mmc5ROMBankIndex(reg byte, forceROM bool) (int, bool) {
	if !forceROM && reg&0x80 == 0 {
		return 0, false
	}
	bank := modInt(int(reg&0x7F), len(m.banksMapped))
	return bank, true
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
