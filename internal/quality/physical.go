package quality

import (
	"cmp"
	"maps"
	"slices"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/set"
)

const (
	physicalData byte = iota + 1
	physicalCode
)

type physicalAnalysis struct {
	bytes        []byte
	instructions map[int]Example
}

func (a *physicalAnalysis) addBank(bank *program.PRGBank, mapperID uint16) {
	starts := set.New[uint16]()
	var instructions []instruction
	for i, info := range bank.Offsets {
		if len(info.Data) == 0 {
			continue
		}
		kind := physicalData
		if info.IsType(tracedCode) {
			kind = physicalCode
		}

		for j := i; j < min(len(bank.Offsets), i+len(info.Data)); j++ {
			a.bytes[bank.ROMOffset+j] = max(a.bytes[bank.ROMOffset+j], kind)
		}

		if kind != physicalCode {
			continue
		}
		address := bank.BaseAddress + uint16(i)
		starts.Add(address)
		instructions = append(instructions, instruction{
			address:    address,
			codeAsData: info.IsType(program.CodeAsData),
			data:       info.Data,
			opcode:     cpu6502.Opcodes[info.Data[0]],
		})
	}

	for _, ins := range instructions {
		reasons := instructionReasons(ins, mapperID)
		if target, ok := transferTarget(ins); ok && int(target) >= int(bank.BaseAddress) &&
			int(target) < int(bank.BaseAddress)+len(bank.Offsets) && !starts.Contains(target) {

			reasons = append(reasons, BadTarget)
		}

		a.addInstruction(Example{
			Address:   ins.address,
			Bank:      bank.ID,
			ROMOffset: bank.ROMOffset + int(ins.address-bank.BaseAddress),

			Reasons: reasons,
		})
	}
}

func (a *physicalAnalysis) addInstruction(example Example) {
	previous, found := a.instructions[example.ROMOffset]
	if found {
		// A physical warning must apply to each output view of the instruction.
		var common []Reason
		for _, reason := range previous.Reasons {
			if slices.Contains(example.Reasons, reason) {
				common = append(common, reason)
			}
		}

		if compareExamples(previous, example) < 0 {
			example = previous
		}

		example.Reasons = common
	}

	a.instructions[example.ROMOffset] = example
}

func (a *physicalAnalysis) report() Report {
	report := Report{
		Instructions: len(a.instructions),
		Reasons:      make(map[Reason]int),
	}
	for _, kind := range a.bytes {
		switch kind {
		case physicalCode:
			report.CodeBytes++
		case physicalData:
			report.DataBytes++
		}
	}

	for _, example := range slices.SortedFunc(maps.Values(a.instructions), compareExamples) {
		if len(example.Reasons) == 0 {
			continue
		}
		report.Suspicious++
		for _, reason := range example.Reasons {
			report.Reasons[reason]++
		}

		if len(report.Examples) < maxExamples {
			report.Examples = append(report.Examples, example)
		}
	}

	return report
}

func inspectPhysical(app *program.Program) physicalAnalysis {
	size := 0
	for _, bank := range app.PRG {
		size = max(size, bank.ROMOffset+len(bank.Offsets))
	}

	analysis := physicalAnalysis{
		bytes:        make([]byte, size),
		instructions: make(map[int]Example),
	}
	for _, bank := range app.PRG {
		analysis.addBank(bank, app.Mapper)
	}

	return analysis
}

func compareExamples(first, second Example) int {
	return cmp.Or(cmp.Compare(first.Bank, second.Bank), cmp.Compare(first.Address, second.Address),
		cmp.Compare(first.ROMOffset, second.ROMOffset))
}
