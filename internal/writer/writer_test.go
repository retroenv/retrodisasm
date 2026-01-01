package writer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestVectorLabel_AddressBeforeCodeBase(t *testing.T) {
	app := &program.Program{
		CodeBaseAddress: 0x8000,
	}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	bank := &program.PRGBank{}

	// Address below code base should return hex address
	label := w.VectorLabel(bank, 0x7FFF)
	assert.Equal(t, "$7FFF", label)

	label = w.VectorLabel(bank, 0x0000)
	assert.Equal(t, "$0000", label)
}

func TestVectorLabel_LabelInCurrentBank(t *testing.T) {
	app := &program.Program{
		CodeBaseAddress: 0x8000,
	}

	// Create bank with label at offset 0 (address 0x8000)
	bank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x8000),
	}
	bank.Offsets[0].Label = "Reset_Bank0"

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	// Should return label from current bank
	label := w.VectorLabel(bank, 0x8000)
	assert.Equal(t, "Reset_Bank0", label)
}

func TestVectorLabel_FallbackToLastBank(t *testing.T) {
	// Create last bank with label
	lastBank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x8000),
	}
	lastBank.Offsets[0].Label = "Reset"

	app := &program.Program{
		CodeBaseAddress: 0x8000,
		PRG:             []*program.PRGBank{lastBank},
	}

	// Create current bank without label at offset 0
	currentBank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x8000),
	}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	// Should fall back to last bank's label
	label := w.VectorLabel(currentBank, 0x8000)
	assert.Equal(t, "Reset", label)
}

func TestVectorLabel_NoLabelReturnsHex(t *testing.T) {
	// Create banks without labels
	lastBank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x8000),
	}

	app := &program.Program{
		CodeBaseAddress: 0x8000,
		PRG:             []*program.PRGBank{lastBank},
	}

	currentBank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x8000),
	}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	// No label anywhere should return hex
	label := w.VectorLabel(currentBank, 0x8000)
	assert.Equal(t, "$8000", label)
}

func TestVectorLabel_OutOfBoundsIndex(t *testing.T) {
	app := &program.Program{
		CodeBaseAddress: 0x8000,
	}

	// Create small bank
	bank := &program.PRGBank{
		Offsets: make([]program.Offset, 0x100),
	}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	// Address way past bank size should return hex
	label := w.VectorLabel(bank, 0xFFFF)
	assert.Equal(t, "$FFFF", label)
}

func TestOutputAliasMap_DeduplicatesAcrossCalls(t *testing.T) {
	app := &program.Program{}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	// First call with some aliases
	aliases1 := map[string]uint16{
		"PPU_CTRL": 0x2000,
		"PPU_MASK": 0x2001,
	}
	err := w.OutputAliasMap(aliases1)
	assert.NoError(t, err)

	output1 := buf.String()
	assert.Contains(t, output1, "PPU_CTRL = $2000")
	assert.Contains(t, output1, "PPU_MASK = $2001")

	// Second call with overlapping aliases
	aliases2 := map[string]uint16{
		"PPU_CTRL":   0x2000, // duplicate
		"PPU_STATUS": 0x2002, // new
	}
	err = w.OutputAliasMap(aliases2)
	assert.NoError(t, err)

	output2 := buf.String()
	// PPU_CTRL should only appear once (from first call)
	// PPU_STATUS should be in second call
	assert.Contains(t, output2, "PPU_STATUS = $2002")

	// Count occurrences of PPU_CTRL - should be exactly 1
	count := strings.Count(output2, "PPU_CTRL")
	assert.Equal(t, 1, count)
}

func TestOutputAliasMap_EmptyMap(t *testing.T) {
	app := &program.Program{}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	err := w.OutputAliasMap(map[string]uint16{})
	assert.NoError(t, err)
	assert.Equal(t, "", buf.String())
}

func TestOutputAliasMap_AllDuplicates(t *testing.T) {
	app := &program.Program{}

	var buf bytes.Buffer
	w := New(app, &buf, Options{})

	aliases := map[string]uint16{"TEST": 0x1234}
	err := w.OutputAliasMap(aliases)
	assert.NoError(t, err)

	initialLen := buf.Len()

	// Second call with same alias should add nothing
	err = w.OutputAliasMap(aliases)
	assert.NoError(t, err)

	// Buffer length should be unchanged (no new content added)
	assert.Equal(t, initialLen, buf.Len())
}
