package disasm

import (
	"github.com/retroenv/retrodisasm/internal/program"
)

// ChangeAddressRangeToCodeAsData sets a range of code address to code as
// data types. It combines all data bytes that are not split by a label.
func (dis *Disasm) ChangeAddressRangeToCodeAsData(address uint16, data []byte) {
	for i := 0; i < len(data); i++ {
		offsetInfo := dis.mapper.OffsetInfo(address + uint16(i))

		noLabelOffsets := 1
		for j := i + 1; j < len(data); j++ {
			offsetInfoNext := dis.mapper.OffsetInfo(address + uint16(j))
			if offsetInfoNext.Label == "" {
				offsetInfoNext.Data = nil
				offsetInfoNext.SetType(program.CodeAsData | program.DataOffset)
				noLabelOffsets++

				skipAddressToParse := address + uint16(j)
				dis.offsetsParsed.Add(skipAddressToParse)
				continue
			}
			break
		}

		offsetInfo.Data = data[i : i+noLabelOffsets]
		offsetInfo.ClearType(program.CodeOffset | program.FunctionReference | program.JumpTable)
		offsetInfo.SetType(program.CodeAsData | program.DataOffset)
		i += noLabelOffsets - 1
	}
}
