package nesasm

import (
	"fmt"
	"io"

	"github.com/retroenv/retrodisasm/internal/program"
)

const bankSize = 0x2000

// addPrgBankSelectors adds PRG bank selectors to every 0x2000 byte offsets, as required
// by nesasm to avoid the error "Bank overflow, offset > $1FFF".
func addPrgBankSelectors(codeBaseAddress int, prg []*program.PRGBank) int {
	counter := 0
	bankNumber := 0
	bankAddress := codeBaseAddress
	bankSwitch := true

	for _, bank := range prg {
		index := 0

		for {
			if bankSwitch { // if switch was carried over after last bank was filled
				setPrgBankSelector(bank.Offsets, index, &bankAddress, &bankNumber)
				bankSwitch = false
			}

			bankSpaceLeft := counter % bankSize
			if bankSpaceLeft == 0 {
				bankSpaceLeft = bankSize
			}

			bankBytesLeft := len(bank.Offsets[index:])
			if bankSpaceLeft > bankBytesLeft {
				counter += bankBytesLeft
				break
			}
			if bankSpaceLeft == bankBytesLeft {
				counter += bankBytesLeft
				bankSwitch = true
				break
			}

			setPrgBankSelector(bank.Offsets, index+bankSpaceLeft, &bankAddress, &bankNumber)

			index += bankSpaceLeft
			counter += bankSpaceLeft
		}
	}

	return bankNumber
}

// chrBanks adds CHR bank selectors to every 0x2000 byte offsets, as required
// by nesasm to avoid the error "Bank overflow, offset > $1FFF".
func chrBanks(nextBank int, chr program.CHR) []program.CHR {
	banks := make([]program.CHR, 0, len(chr)/bankSize)
	remaining := len(chr)

	for index := 0; remaining > 0; nextBank++ {
		toWrite := min(remaining, bankSize)

		bank := chr[index : index+toWrite]
		//WriteCallback: writeBankSelector(nextBank, -1),
		banks = append(banks, bank)

		index += toWrite
		remaining -= toWrite
	}

	return banks
}

func setPrgBankSelector(prg []program.Offset, index int, bankAddress, bankNumber *int) {
	offsetInfo := &prg[index]

	// NESASM's 8 KiB bank boundary may split an instruction or function-reference
	// .word. Convert it to bytes so the shared writer reaches the bank callback.
	if offsetInfo.IsType(program.CodeOffset|program.FunctionReference) && len(offsetInfo.Data) == 0 {
		// look backwards for instruction start
		instructionStartIndex := index - 1
		for offsetInfo = &prg[instructionStartIndex]; len(offsetInfo.Data) == 0; {
			instructionStartIndex--
			offsetInfo = &prg[instructionStartIndex]
		}

		offsetInfo.Comment = fmt.Sprintf("bank switch in instruction detected: %s %s",
			offsetInfo.Comment, offsetInfo.Code)
		data := offsetInfo.Data

		for i := range data {
			offsetInfo = &prg[instructionStartIndex+i]
			offsetInfo.Data = data[i : i+1]
			// Structured-reference types take precedence over DataOffset in the shared writer.
			offsetInfo.ClearType(program.CodeOffset | program.FunctionReference | program.JumpTable)
			offsetInfo.SetType(program.CodeAsData | program.DataOffset)
		}
		offsetInfo = &prg[index]
	}

	offsetInfo.WriteCallback = writeBankSelector(*bankNumber, *bankAddress)

	*bankAddress += bankSize
	// Wrap address back to $8000 after reaching $10000 (end of $8000-$FFFF range)
	// nesasm uses 8KB banks but NES code runs at $8000-$FFFF
	if *bankAddress >= 0x10000 {
		*bankAddress = 0x8000
	}
	*bankNumber++
}

func writeBankSelector(bankNumber, bankAddress int) func(writer io.Writer) error {
	return func(writer io.Writer) error {
		if _, err := fmt.Fprintf(writer, "\n .bank %d\n", bankNumber); err != nil {
			return fmt.Errorf("writing bank switch: %w", err)
		}

		if bankAddress >= 0 {
			if _, err := fmt.Fprintf(writer, " .org $%04x\n\n", bankAddress); err != nil {
				return fmt.Errorf("writing segment: %w", err)
			}
		}
		return nil
	}
}
