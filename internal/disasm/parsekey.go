package disasm

// ParseKey uniquely identifies a parse location by CPU address and active mapping state.
type ParseKey struct {
	PC        uint16
	MappingID uint64
}

func (dis *Disasm) currentParseKey(address uint16) ParseKey {
	return ParseKey{
		PC:        address,
		MappingID: dis.mapper.MappingSignature(),
	}
}
