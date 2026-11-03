package quality

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

// testBank returns a bank at $8000. Each item is an instruction or, with a
// nil type, one data byte.
func testBank(items ...testItem) *program.PRGBank {
	size := 0
	for _, item := range items {
		size += len(item.data)
	}
	bank := program.NewPRGBank(size)
	bank.BaseAddress = 0x8000
	offset := 0
	for _, item := range items {
		if item.typ == 0 {
			for _, b := range item.data {
				bank.Offsets[offset].Data = []byte{b}
				bank.Offsets[offset].SetType(program.DataOffset)
				offset++
			}
			continue
		}
		bank.Offsets[offset].Data = item.data
		bank.Offsets[offset].SetType(item.typ)
		for i := 1; i < len(item.data); i++ {
			bank.Offsets[offset+i].SetType(program.CodeOffset)
		}
		offset += len(item.data)
	}
	return bank
}

type testItem struct {
	data []byte
	typ  program.OffsetType
}

func code(bytes ...byte) testItem {
	return testItem{
		data: bytes,
		typ:  program.CodeOffset,
	}
}

func data(bytes ...byte) testItem {
	return testItem{
		data: bytes,
	}
}

// testProgram returns a program without mapper registers.
func testProgram(banks ...*program.PRGBank) *program.Program {
	return &program.Program{
		PRG: banks,
	}
}

// aliasBank returns a copy of the bank at another CPU address. The copy has
// its own output records. The physical ROM range does not change.
func aliasBank(bank *program.PRGBank, address uint16) *program.PRGBank {
	alias := *bank
	alias.BaseAddress = address
	alias.Offsets = slices.Clone(bank.Offsets)
	return &alias
}

func TestAnalyzeCleanCode(t *testing.T) {
	bank := testBank(
		code(0xa9, 0x00),       // $8000 lda #$00
		code(0x8d, 0x00, 0x20), // $8002 sta $2000
		code(0xad, 0x02, 0x20), // $8005 lda $2002
		code(0x10, 0xfb),       // $8008 bpl $8005
		code(0x2c, 0x00, 0x00), // $800A bit $0000
		code(0x60),             // $800D rts
	)
	bank.Offsets[0x0a].SetType(program.CodeAsData)
	report := Analyze(testProgram(bank))

	assert.Equal(t, 6, report.Instructions)
	assert.Equal(t, 14, report.CodeBytes)
	assert.Equal(t, 0, report.Suspicious)
	assert.Len(t, report.Examples, 0)
}

func TestAnalyzeSuspiciousCode(t *testing.T) {
	bank := testBank(
		code(0x00),             // $8000 brk
		code(0x02),             // $8001 kil
		code(0xf8),             // $8002 sed
		code(0xad, 0x00, 0x30), // $8003 lda $3000, a write-only PPU mirror.
		code(0xad, 0x00, 0x20), // $8006 lda $2000
		code(0x8d, 0x02, 0x20), // $8009 sta $2002
		code(0x8d, 0x00, 0x80), // $800C sta $8000
		code(0x10, 0xf1),       // $800F bpl $8002, a valid start
		code(0x10, 0xf0),       // $8011 bpl $8003, a valid start
		code(0x10, 0xf0),       // $8013 bpl $8005, an operand
		code(0x4c, 0x00, 0x90), // $8015 jmp $9000, outside of the bank
		code(0xa9, 0x00),       // $8018 lda #$00, written as data
		data(0x14, 0x14),       // $801A nop $14,X, written as data
	)
	bank.Offsets[0x18].SetType(program.CodeAsData)
	bank.Offsets[0x1a].Data = []byte{0x14, 0x14}
	bank.Offsets[0x1a].SetType(program.CodeAsData)
	bank.Offsets[0x1b].Data = nil
	bank.Offsets[0x1b].SetType(program.CodeAsData)
	report := Analyze(testProgram(bank))

	assert.Equal(t, 13, report.Instructions)
	assert.Equal(t, 0, report.DataBytes)
	assert.Equal(t, 10, report.Suspicious)
	assert.Equal(t, 1, report.Reasons[Brk])
	assert.Equal(t, 2, report.Reasons[Unofficial])
	assert.Equal(t, 1, report.Reasons[Decimal])
	assert.Equal(t, 0, report.Reasons[UnusedAddress])
	assert.Equal(t, 2, report.Reasons[WriteOnlyRead])
	assert.Equal(t, 1, report.Reasons[ReadOnlyWrite])
	assert.Equal(t, 1, report.Reasons[ROMWrite])
	assert.Equal(t, 1, report.Reasons[BadTarget])
	assert.Equal(t, 1, report.Reasons[CodeAsData])
	assert.Equal(t, uint16(0x8013), report.Examples[7].Address)

	// A mapper cartridge has registers in the ROM area.
	report = Analyze(&program.Program{
		PRG:    []*program.PRGBank{bank},
		Mapper: 1,
	})
	assert.Equal(t, 0, report.Reasons[ROMWrite])
}

func TestAnalyzeMissedCode(t *testing.T) {
	bank := testBank(
		code(0x60), // $8000 rts
		data(
			0xa9, 0x80, // lda #$80
			0x8d, 0x00, 0x20, // sta $2000
			0x20, 0x00, 0x80, // jsr $8000
			0xa2, 0x00, // ldx #$00
			0x60, // rts
		),
		code(0x60), // $800C rts
		data(0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff),
		code(0x60), // $8016 rts
		data(0xa9, 0x01, 0xa9, 0x02, 0xa9, 0x03, 0xa9, 0x04, 0x60),
	)
	report := Analyze(testProgram(bank))

	// The second data run has no plausible code. The third run has valid
	// instructions, but no hardware access or call.
	assert.Equal(t, 1, report.MissedCodeRuns)
	assert.Equal(t, 11, report.MissedCodeBytes)
	assert.Equal(t, 29, report.DataBytes)
}

func TestReportWrite(t *testing.T) {
	report := Report{
		Instructions: 200,

		Reasons: map[Reason]int{
			BadTarget: 1,
			Brk:       2,
		},
		Suspicious: 3,

		Examples: []Example{
			{
				Address:   0x8123,
				Bank:      2,
				ROMOffset: 0x2123,

				Reasons: []Reason{Brk, BadTarget},
			},
		},
	}
	var b strings.Builder
	assert.NoError(t, report.Write(&b))

	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	assert.Len(t, lines, 4)
	assert.True(t, strings.Contains(lines[0], "suspicious 3 (1.50%)"))
	assert.True(t, strings.Contains(lines[1], "brk"))
	assert.True(t, strings.Contains(lines[2], "bad-target"))
	assert.Equal(t, "quality:   bank 2 $8123 ROM $002123: brk, bad-target", lines[3])
}

func TestAnalyzePPUMirrors(t *testing.T) {
	bank := testBank(
		code(0xad, 0x0a, 0x20), // Read the PPU status mirror.
		code(0x8d, 0x08, 0x20), // Write the PPU control mirror.
		code(0x8d, 0xfa, 0x3f), // Write a read-only status mirror.
		code(0xad, 0xf8, 0x3f), // Read a write-only control mirror.
	)
	report := Analyze(testProgram(bank))
	assert.Equal(t, 0, report.Reasons[UnusedAddress])
	assert.Equal(t, 1, report.Reasons[ReadOnlyWrite])
	assert.Equal(t, 1, report.Reasons[WriteOnlyRead])
}

func TestAnalyzePackedDataRun(t *testing.T) {
	encoded := []byte{0xa9, 0x80, 0x8d, 0, 0x20, 0x20, 0, 0x80, 0xa2, 0, 0x60}
	bank := testBank(data(encoded...))
	bank.Offsets[0].Data = encoded
	for i := 1; i < len(bank.Offsets); i++ {
		bank.Offsets[i].Data = nil
	}
	report := Analyze(testProgram(bank))
	assert.Len(t, encoded, report.DataBytes)
	assert.Len(t, encoded, report.MissedCodeBytes)
	assert.Equal(t, 1, report.MissedCodeRuns)
}

func TestAnalyzeCodeAfterDataPadding(t *testing.T) {
	bank := testBank(data(0xff, 0xff, 0xff, 0xa9, 0x80, 0x8d, 0, 0x20, 0x20, 0, 0x80, 0xa2, 0, 0x60))
	report := Analyze(testProgram(bank))
	assert.Equal(t, 11, report.MissedCodeBytes)
	assert.Equal(t, 1, report.MissedCodeRuns)
}

func TestAnalyzeCodeEndingInIndirectJump(t *testing.T) {
	bank := testBank(data(0xa9, 0x80, 0x8d, 0, 0x20, 0xa2, 0, 0xa0, 0, 0x6c, 0x10, 0))
	report := Analyze(testProgram(bank))
	assert.Equal(t, 12, report.MissedCodeBytes)
	assert.Equal(t, 1, report.MissedCodeRuns)
}

func TestAnalyzeLongCodeCandidate(t *testing.T) {
	encoded := append([]byte{0xa9, 0, 0x8d, 0, 0x20}, bytes.Repeat([]byte{0xea}, 128)...)
	encoded = append(encoded, 0x60)
	report := Analyze(testProgram(testBank(data(encoded...))))
	assert.Len(t, encoded, report.MissedCodeBytes)
	assert.Equal(t, 1, report.MissedCodeRuns)
}

func TestMissedCodeRequiresInstructionTargets(t *testing.T) {
	for _, branch := range []byte{0xf8, 0xff, 1} {
		// The branch targets an operand of LDA, BNE, or STA.
		encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xa2, 0, 0xd0, branch, 0x8d, 0, 0x20, 0x60}
		report := Analyze(testProgram(testBank(data(encoded...))))
		assert.Equal(t, 0, report.MissedCodeRuns)
	}
}

func TestMissedCodeKeepsSuffixAfterInvalidTarget(t *testing.T) {
	encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xd0, 0xff,
		0xa9, 2, 0x8d, 0, 0x20, 0xa2, 0, 0x60}
	report := Analyze(testProgram(testBank(data(encoded...))))
	assert.Equal(t, 8, report.MissedCodeBytes)
	assert.Equal(t, 1, report.MissedCodeRuns)
	assert.Equal(t, []MissedExample{
		{
			Address:   0x8007,
			ROMOffset: 7,

			Bytes:        8,
			Instructions: 4,
		},
	}, report.MissedExamples)
}

func TestMissedCodeAcceptsInstructionTargets(t *testing.T) {
	for _, branch := range []byte{0xf7, 0xf9, 0, 3, 0x40} {
		encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xa2, 0, 0xd0, branch, 0x8d, 0, 0x20, 0x60}
		report := Analyze(testProgram(testBank(data(encoded...))))
		assert.Len(t, encoded, report.MissedCodeBytes)
		assert.Equal(t, 1, report.MissedCodeRuns)
	}
}

func TestMissedCodeRequiresMinimumCandidateSize(t *testing.T) {
	encoded := []byte{0xff, 0xff, 0x8d, 0, 0x20, 0xea, 0xea, 0x60, 0xff, 0xff}
	report := Analyze(testProgram(testBank(data(encoded...))))
	assert.Equal(t, 0, report.MissedCodeRuns)
}

func TestMissedCodeRejectsCallAndJumpIntoOperand(t *testing.T) {
	for _, opcode := range []byte{0x20, 0x4c} {
		encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xa2, 0, opcode, 1, 0x80, 0x60}
		report := Analyze(testProgram(testBank(data(encoded...))))
		assert.Equal(t, 0, report.MissedCodeRuns)
	}
}

func TestMissedCodeReportsBoundedLocations(t *testing.T) {
	encoded := []byte{0xa9, 1, 0x8d, 0, 0x20, 0xa2, 0, 0x60}
	bank := testBank(data(bytes.Repeat(encoded, maxExamples+1)...))
	bank.ID = 7
	bank.ROMOffset = 0x20000
	report := Analyze(testProgram(bank))
	assert.Equal(t, maxExamples+1, report.MissedCodeRuns)
	assert.Len(t, report.MissedExamples, maxExamples)
	assert.Equal(t, MissedExample{
		Address:      0x8000,
		Bank:         7,
		Bytes:        8,
		Instructions: 4,
		ROMOffset:    0x20000,
	}, report.MissedExamples[0])
	var out strings.Builder
	assert.NoError(t, report.Write(&out))
	assert.Contains(t, out.String(), "possible missed code bank 7 $8000 ROM $020000: 8 bytes, 4 instructions")
}

func TestAnalyzeHardwareAccessRanges(t *testing.T) {
	tests := []struct {
		data   []byte
		mapper uint16
		name   string
		reason Reason
	}{
		{
			data: []byte{0xbd, 0, 0x20},
			name: "indexed PPU read can select status",
		},
		{
			data: []byte{0x99, 2, 0x20},
			name: "indexed PPU write can select control",
		},
		{
			data: []byte{0xb9, 0, 0x40},
			name: "indexed APU read can select status",
		},
		{
			data: []byte{0xbd, 0x80, 0x5f},
			name: "indexed expansion read can reach RAM",
		},
		{
			data: []byte{0x9d, 0xff, 0xff},
			name: "indexed ROM write can wrap to RAM",
		},
		{
			data:   []byte{0xbd, 0, 0x50},
			name:   "indexed expansion read stays unused",
			reason: UnusedAddress,
		},
		{
			data:   []byte{0x9d, 0, 0x80},
			name:   "indexed ROM write stays in ROM",
			reason: ROMWrite,
		},
		{
			data:   []byte{0x9d, 0, 0x80},
			mapper: 2,
			name:   "indexed mapper write is valid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &program.Program{
				PRG:    []*program.PRGBank{testBank(code(tt.data...))},
				Mapper: tt.mapper,
			}
			report := Analyze(app)
			if tt.reason == "" {
				assert.Equal(t, 0, report.Suspicious)
			} else {
				assert.Equal(t, 1, report.Suspicious)
				assert.Equal(t, 1, report.Reasons[tt.reason])
			}
		})
	}
}

func TestAnalyzeReadInstructionsDoNotWrite(t *testing.T) {
	// These absolute instructions are adc, eor, ora, and sbc.
	for _, opcode := range []byte{0x6d, 0x4d, 0x0d, 0xed} {
		// The PPU status register is readable.
		report := Analyze(testProgram(testBank(code(opcode, 2, 0x20))))
		assert.Equal(t, 0, report.Suspicious, "opcode $%02X", opcode)

		// A read of ROM is valid on a cartridge with mapper 0.
		report = Analyze(testProgram(testBank(code(opcode, 0, 0x80))))
		assert.Equal(t, 0, report.Suspicious, "opcode $%02X", opcode)

		report = Analyze(testProgram(testBank(code(opcode, 0, 0x20))))
		assert.Equal(t, 1, report.Reasons[WriteOnlyRead], "opcode $%02X", opcode)
	}
}

func TestAnalyzeUnofficialMemoryAccess(t *testing.T) {
	// The unofficial sax instruction writes to ROM.
	report := Analyze(testProgram(testBank(code(0x8f, 0, 0x80))))
	assert.Equal(t, 1, report.Reasons[ROMWrite])
	assert.Equal(t, 1, report.Reasons[Unofficial])

	// The unofficial lax instruction reads a write-only register.
	report = Analyze(testProgram(testBank(code(0xaf, 0, 0x20))))
	assert.Equal(t, 1, report.Reasons[WriteOnlyRead])
	assert.Equal(t, 1, report.Suspicious)
}

func TestAnalyzeModifyHardwareAccess(t *testing.T) {
	for _, opcode := range []byte{0xee, 0xce, 0x0e, 0x4e, 0x2e, 0x6e} {
		for _, address := range []byte{0, 2} {
			bank := testBank(code(opcode, address, 0x20))
			report := Analyze(testProgram(bank))
			reason := WriteOnlyRead
			if address == 2 {
				reason = ReadOnlyWrite
			}
			assert.Equal(t, 1, report.Reasons[reason], opcode, address)
			assert.Equal(t, 1, report.Suspicious, opcode, address)
		}
	}
	// An unknown index can select the readable and writable PPU data register.
	bank := testBank(code(0x7e, 0, 0x20))
	report := Analyze(testProgram(bank))
	assert.Equal(t, 0, report.Suspicious)
}
