package quality

import "github.com/retroenv/retrogolib/arch/cpu/cpu6502"

// inconsistentFlowPrefix returns the bytes through the last transfer that
// targets an operand in the candidate. The caller can check the suffix.
// Targets outside the candidate can use other banks or RAM. They are unknown.
func inconsistentFlowPrefix(data []byte, start uint16) int {
	starts := make([]bool, len(data))
	for i := 0; i < len(data); {
		starts[i] = true
		i += cpu6502.Opcodes[data[i]].Size()
	}

	invalidEnd := 0
	for i := 0; i < len(data); {
		opcode := cpu6502.Opcodes[data[i]]
		size := opcode.Size()
		ins := instruction{
			address: start + uint16(i),
			data:    data[i : i+size],
			opcode:  opcode,
		}
		if target, known := transferTarget(ins); known {
			offset := int(target - start)
			if offset < len(starts) && !starts[offset] {
				invalidEnd = i + size
			}
		}

		i += size
	}

	return invalidEnd
}
