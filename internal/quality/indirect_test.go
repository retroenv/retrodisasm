package quality

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestAnalyzeIndirectPointerAccess(t *testing.T) {
	tests := []struct {
		mapper  uint16
		name    string
		pointer uint16
		reason  Reason
	}{
		{
			name:    "unused expansion",
			pointer: 0x428e,
			reason:  UnusedAddress,
		},
		{
			name:    "unused high byte",
			pointer: 0x4017,
			reason:  UnusedAddress,
		},
		{
			mapper:  5,
			name:    "cpu test",
			pointer: 0x4018,
			reason:  UnusedAddress,
		},
		{
			name:    "ppu write only",
			pointer: 0x2000,
			reason:  WriteOnlyRead,
		},
		{
			name:    "ppu high byte write only",
			pointer: 0x2002,
			reason:  WriteOnlyRead,
		},
		{
			name:    "ppu mirror",
			pointer: 0x3000,
			reason:  WriteOnlyRead,
		},
		{
			name:    "apu write only",
			pointer: 0x4013,
			reason:  WriteOnlyRead,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := analyzeIndirectJump(tt.pointer, tt.mapper)
			assert.Equal(t, map[Reason]int{tt.reason: 1}, report.Reasons)
		})
	}
}

func TestAnalyzeIndirectPointerAccessWithoutWarning(t *testing.T) {
	tests := []struct {
		mapper  uint16
		name    string
		pointer uint16
	}{
		{
			name:    "ram",
			pointer: 0x0010,
		},
		{
			name:    "page wrap",
			pointer: 0x1fff,
		},
		{
			name:    "rom wrap",
			pointer: 0xffff,
		},
		{
			name:    "cartridge ram",
			pointer: 0x6038,
		},
		{
			mapper:  5,
			name:    "mmc5 registers",
			pointer: 0x5205,
		},
		{
			mapper:  19,
			name:    "namco registers",
			pointer: 0x4800,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, analyzeIndirectJump(tt.pointer, tt.mapper).Reasons)
		})
	}
}

func TestMissedCodeRejectsUnusedIndirectPointer(t *testing.T) {
	bank := testBank(data(0xa9, 0x80, 0x8d, 0, 0x20, 0xa2, 0, 0x6c, 0x8e, 0x42))
	report := Analyze(testProgram(bank))
	assert.Equal(t, 0, report.MissedCodeBytes)
	assert.Equal(t, 0, report.MissedCodeRuns)
}

func TestIndirectPointerChecksBothReads(t *testing.T) {
	bank := testBank(code(0x6c, 0xff, 0x40))
	report := Analyze(testProgram(bank))
	assert.Equal(t, map[Reason]int{
		UnusedAddress: 1,
		WriteOnlyRead: 1,
	}, report.Reasons)
	assert.Equal(t, 1, report.Suspicious)
}

func TestIndirectPointerRequiresCompleteOperand(t *testing.T) {
	for _, bytes := range [][]byte{{0x6c}, {0x6c, 0x42}} {
		bank := testBank(code(bytes...))
		report := Analyze(testProgram(bank))
		assert.Empty(t, report.Reasons)
	}
}

// analyzeIndirectJump returns the report of one indirect jump through the
// pointer.
func analyzeIndirectJump(pointer, mapperID uint16) Report {
	bank := testBank(code(0x6c, byte(pointer), byte(pointer>>8)))
	return Analyze(&program.Program{
		PRG:    []*program.PRGBank{bank},
		Mapper: mapperID,
	})
}
