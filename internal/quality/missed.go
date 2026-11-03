package quality

import (
	"cmp"
	"maps"
	"slices"
)

type missedCode struct {
	bytes      []bool
	candidates map[int]MissedExample
}

func newMissedCode(size int) missedCode {
	return missedCode{
		bytes:      make([]bool, size),
		candidates: make(map[int]MissedExample),
	}
}

func (m *missedCode) add(example MissedExample) {
	for i := example.ROMOffset; i < example.ROMOffset+example.Bytes; i++ {
		m.bytes[i] = true
	}

	previous, found := m.candidates[example.ROMOffset]
	if !found || example.Bytes > previous.Bytes ||
		(example.Bytes == previous.Bytes && compareMissedExamples(example, previous) < 0) {

		m.candidates[example.ROMOffset] = example
	}
}

func (m *missedCode) report(report *Report) {
	report.MissedCodeRuns = len(m.candidates)
	for _, candidate := range m.bytes {
		if candidate {
			report.MissedCodeBytes++
		}
	}

	examples := slices.SortedFunc(maps.Values(m.candidates), compareMissedExamples)
	report.MissedExamples = firstExamples(examples)
}

func compareMissedExamples(first, second MissedExample) int {
	return cmp.Or(cmp.Compare(first.Bank, second.Bank), cmp.Compare(first.Address, second.Address),
		cmp.Compare(first.ROMOffset, second.ROMOffset))
}
