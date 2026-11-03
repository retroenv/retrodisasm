package mapper

// Control ports of the boards that decode the CPU expansion area.
const (
	nina003ControlMask = 0xe100
	nina003ControlBase = 0x4100

	gtromControlMask = 0xd000
	gtromControlBase = 0x5000
)

// HasExpansionRegister reports a known control port in the CPU expansion area.
func HasExpansionRegister(mapperID, address uint16) bool {
	if address < 0x4020 || address >= 0x6000 {
		return false
	}
	switch mapperID {
	case 79:
		return address&nina003ControlMask == nina003ControlBase
	case 111:
		return address&gtromControlMask == gtromControlBase
	default:
		return false
	}
}
