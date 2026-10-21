package mapper

// Mapping identifies the physical bytes visible in the eight 8 KiB CPU windows.
// Unmapped windows contain -1. Register latches are deliberately excluded so
// traces with identical address translation can share decoded instructions.
type Mapping [8]int

// Mapping returns the address-translation identity independently of register latches.
func (m *Mapper) Mapping(state Snapshot) Mapping {
	var key Mapping
	for i := range key {
		key[i] = -1
	}
	if m.layout == nil {
		return key
	}
	for _, win := range m.layout.Windows(state) {
		for i := 0; i < win.size; i += 0x2000 {
			key[(int(win.address)+i)/0x2000] = win.romOffset + i
		}
	}
	return key
}
