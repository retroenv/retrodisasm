package writer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestOutputAliasMap_SkipsDuplicateReEmission(t *testing.T) {
	app := &program.Program{}
	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	aliases := map[string]uint16{
		"PPU_CTRL": 0x2000,
		"PPU_MASK": 0x2001,
	}

	assert.NoError(t, w.OutputAliasMap(aliases))
	assert.NoError(t, w.OutputAliasMap(aliases))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "PPU_CTRL = $2000"))
	assert.Equal(t, 1, strings.Count(out, "PPU_MASK = $2001"))
}

func TestOutputAliasMap_EmitsWhenAddressDiffers(t *testing.T) {
	app := &program.Program{}
	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"FOO": 0x0010}))
	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"FOO": 0x0020}))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "FOO = $0010"))
	assert.Equal(t, 1, strings.Count(out, "FOO = $0020"))
}

func TestForOutputSharesAliasEmissionState(t *testing.T) {
	app := &program.Program{}
	var first, second bytes.Buffer
	w := New(app, &first, Options{})

	assert.NoError(t, w.OutputAliasMap(map[string]uint16{"PPU_CTRL": 0x2000}))
	child := w.ForOutput(&second, Options{})
	assert.NoError(t, child.OutputAliasMap(map[string]uint16{"PPU_CTRL": 0x2000}))

	assert.Contains(t, first.String(), "PPU_CTRL = $2000")
	assert.NotContains(t, second.String(), "PPU_CTRL = $2000")
}

func TestBundleAddressedDataWrites(t *testing.T) {
	data := make([]byte, 18)
	for i := range data {
		data[i] = byte(i)
	}

	tests := []struct {
		name           string
		offsetComments bool
		want           string
	}{
		{
			name:           "address comments",
			offsetComments: true,
			want: ".byte $00, $01, $02, $03, $04, $05, $06, $07, $08, $09, $0a, $0b, $0c, $0d, $0e, $0f ; $1000\n" +
				".byte $10, $11                   ; $1010\n",
		},
		{
			name: "comments disabled",
			want: ".byte $00, $01, $02, $03, $04, $05, $06, $07, $08, $09, $0a, $0b, $0c, $0d, $0e, $0f\n" +
				".byte $10, $11\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := New(&program.Program{}, &buf, Options{OffsetComments: tt.offsetComments})

			assert.NoError(t, w.BundleAddressedDataWrites(data, 0x1000))
			assert.Equal(t, tt.want, buf.String())
		})
	}
}

func TestProcessPRGWritesCrossSegmentBranchAsBytes(t *testing.T) {
	var buf bytes.Buffer
	bank := program.NewPRGBank(2)
	bank.Offsets[0] = program.Offset{
		Data: []byte{0xD0, 0x2E},
		Type: program.CodeOffset,
		Code: "bne _label_c010",
	}
	w := New(&program.Program{}, &buf, Options{LiteralCrossSegmentBranches: true})

	assert.NoError(t, w.ProcessPRG(bank, 2))

	assert.Equal(t, "  .byte $d0, $2e\n", buf.String())
}

func TestProcessPRGWritesLiteralBranchOutsideSegmentAsBytes(t *testing.T) {
	var buf bytes.Buffer
	bank := program.NewPRGBank(0x8000)
	bank.BaseAddress = 0x8000
	bank.Offsets[0] = program.Offset{
		Address: 0x8000,
		Data:    []byte{0x30, 0x80},
		Type:    program.CodeOffset,
		Code:    "bmi $7F82",
	}
	w := New(&program.Program{}, &buf, Options{LiteralCrossSegmentBranches: true})

	assert.NoError(t, w.ProcessPRG(bank, 2))

	assert.Equal(t, "  .byte $30, $80\n", buf.String())
}

func TestGetPrgDataStopsAtComment(t *testing.T) {
	// Bundling through an annotated byte used to discard its comment.
	bank := program.NewPRGBank(3)
	for i := range bank.Offsets {
		bank.Offsets[i] = program.Offset{Data: []byte{byte(i)}, Type: program.DataOffset}
	}
	bank.Offsets[1].Comment = "Middle byte"
	assert.Equal(t, []byte{0}, getPrgData(bank, 0, 3))
	assert.Equal(t, []byte{1, 2}, getPrgData(bank, 1, 3))
}

func TestPresentationSpacing(t *testing.T) {
	for _, count := range []int{-1, 0, 2} {
		bank := program.NewPRGBank(2)
		bank.Offsets[0] = program.Offset{Data: []byte{0x60}, Code: "rts", Type: program.CodeOffset}
		bank.Offsets[1] = program.Offset{Data: []byte{1}, Type: program.DataOffset, Label: "Table", CommentBefore: `Heading\nDetails`}
		want := 1
		if count >= 0 {
			bank.Offsets[1].BlankLines = &count
			want = count
		}
		var buf bytes.Buffer
		wr := New(&program.Program{}, &buf, Options{})
		assert.NoError(t, wr.ProcessPRG(bank, 2))
		assert.True(t, strings.Contains(buf.String(), "rts\n"+strings.Repeat("\n", want)+"; Heading\n; Details\nTable:\n"))
	}
}

func TestGetPrgDataStopsAtPresentation(t *testing.T) {
	for _, withComment := range []bool{false, true} {
		bank := program.NewPRGBank(2)
		bank.Offsets[0] = program.Offset{Data: []byte{1}, Type: program.DataOffset}
		bank.Offsets[1] = program.Offset{Data: []byte{2}, Type: program.DataOffset}
		if withComment {
			bank.Offsets[1].CommentBefore = "Table"
		} else {
			count := 0
			bank.Offsets[1].BlankLines = &count
		}
		assert.Equal(t, []byte{1}, getPrgData(bank, 0, 2))
	}
}

func TestExpressionLines(t *testing.T) {
	// NESASM truncates long input lines, so split only between complete expressions.
	lines, err := expressionLines(".byte HIGH(TableA), HIGH(TableB), HIGH(TableC)", 34)
	assert.NoError(t, err)
	assert.Equal(t, []string{".byte HIGH(TableA), HIGH(TableB)", ".byte HIGH(TableC)"}, lines)
	_, err = expressionLines(".byte HIGH(TableA)", 10)
	assert.Error(t, err)
}

func TestGetPrgDataStopsAtBankEnd(t *testing.T) {
	bank := program.NewPRGBank(3)
	bank.Offsets[0] = program.Offset{
		Data: []byte{0xa9, 0x01, 0x02},
		Type: program.DataOffset,
	}

	data := getPrgData(bank, 0, 2)

	assert.Equal(t, []byte{0xa9, 0x01}, data)
}
