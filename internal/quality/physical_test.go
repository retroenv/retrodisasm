package quality

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestExamplesAreBounded(t *testing.T) {
	bank := program.NewPRGBank(maxExamples + 2)
	bank.BaseAddress = 0x8000
	for i := range bank.Offsets {
		bank.Offsets[i] = program.Offset{
			Data: []byte{0},
			Type: program.CodeOffset,
		}
	}
	report := Analyze(testProgram(bank))
	assert.Equal(t, maxExamples+2, report.Reasons[Brk])
	assert.Len(t, report.Examples, maxExamples)
}

func TestReasonsUsePhysicalLocations(t *testing.T) {
	bank := testBank(code(0x00))
	bank.ROMOffset = 0x4000
	alias := aliasBank(bank, 0xa000)
	other := *bank
	other.ROMOffset = 0x6000
	report := Analyze(testProgram(&other, bank, alias))
	assert.Equal(t, 2, report.Suspicious)
	assert.Equal(t, 2, report.Reasons[Brk])
	assert.Len(t, report.Examples, 2)
}

func TestAnalyzeCountsPhysicalCodeOnce(t *testing.T) {
	bank := testBank(code(0x00), code(0xa9, 1), data(0x60, 0xea))
	bank.ROMOffset = 0x4000
	alias := aliasBank(bank, 0xa000)
	alias.ID = 1
	alias.Offsets[1].SetType(program.CodeAsData)
	alias.Offsets[3] = program.Offset{
		Data: []byte{0x60},
		Code: "rts",
		Type: program.CodeOffset,
	}
	app := testProgram(alias, bank)
	report := Analyze(app)
	assert.Equal(t, 3, report.Instructions)
	assert.Equal(t, 4, report.CodeBytes)
	assert.Equal(t, 1, report.DataBytes)
	assert.Equal(t, 1, report.Suspicious)
	assert.Equal(t, 1, report.Reasons[Brk])
	assert.Equal(t, 0, report.Reasons[CodeAsData])
	assert.Len(t, report.Examples, 1)
	assert.Equal(t, 0, report.Examples[0].Bank)
	assert.Equal(t, uint16(0x8000), report.Examples[0].Address)
}

func TestAnalyzeAliasCanResolveTarget(t *testing.T) {
	bank := testBank(code(0x4c, 3, 0x80), data(0x60))
	alias := *bank
	alias.Offsets = append([]program.Offset(nil), bank.Offsets...)
	alias.Offsets[3] = program.Offset{
		Data: []byte{0x60},
		Code: "rts",
		Type: program.CodeOffset,
	}
	for _, banks := range [][]*program.PRGBank{{bank, &alias}, {&alias, bank}} {
		report := Analyze(testProgram(banks...))
		assert.Equal(t, 0, report.Reasons[BadTarget])
		assert.Equal(t, 2, report.Instructions)
	}
}

func TestAnalyzeCountsOverlappingPhysicalBytesOnce(t *testing.T) {
	bank := testBank(code(0xa9, 0xea), data(0x60))
	bank.Offsets[1] = program.Offset{
		Data: []byte{0xea},
		Code: "nop",
		Type: program.CodeOffset,
	}
	report := Analyze(testProgram(bank))
	assert.Equal(t, 2, report.Instructions)
	assert.Equal(t, 2, report.CodeBytes)
	assert.Equal(t, 1, report.DataBytes)
	assert.Equal(t, 0, report.Suspicious)
}

func TestAnalyzeMissedCodeUsesPhysicalLocations(t *testing.T) {
	encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xa2, 0, 0x60}
	bank := testBank(data(encoded...))
	bank.ROMOffset = 0x4000
	alias := aliasBank(bank, 0xa000)
	alias.ID = 1
	other := *bank
	other.ROMOffset = 0x6000
	for _, banks := range [][]*program.PRGBank{{alias, bank, &other}, {&other, bank, alias}} {
		report := Analyze(testProgram(banks...))
		assert.Equal(t, 2, report.MissedCodeRuns)
		assert.Equal(t, 16, report.MissedCodeBytes)
		assert.Equal(t, 16, report.DataBytes)
		assert.Len(t, report.MissedExamples, 2)
		assert.Equal(t, uint16(0x8000), report.MissedExamples[0].Address)
	}

	// Code in one output view must not also count as missed code.
	codeView := testBank(code(0xa9, 1), code(0x8d, 0, 0x20), code(0xa2, 0), code(0x60))
	codeView.ROMOffset = bank.ROMOffset
	report := Analyze(testProgram(bank, codeView))
	assert.Equal(t, 0, report.MissedCodeRuns)
	assert.Equal(t, 0, report.MissedCodeBytes)
	assert.Equal(t, 8, report.CodeBytes)
	assert.Equal(t, 0, report.DataBytes)
}
