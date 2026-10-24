package program

// CHR defines CHR data.
type CHR []byte

// LastNonZeroByte searches for the last byte in CHR that is not zero.
func (chr CHR) LastNonZeroByte() int {
	for i := len(chr) - 1; i >= 0; i-- {
		b := chr[i]
		if b == 0 {
			continue
		}
		return i + 1
	}

	return 0
}
