package romconfig

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestParse(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`; annotations
[rom]
prg_crc32 = 00000000
[vectors]
nmi = VBlank
irq = $FFF0
[constants]
VideoControl = $2000 ; hardware
[variables]
Score = 0xC4
[labels]
$8000 = Start
[comments]
$8000 = Initialize; retain # and = in comments
[code]
$8000 = $8002
[data]
$8003 = $8005
`))
	assert.NoError(t, err)
	assert.Equal(t, uint32(0), *cfg.PRGCRC32)
	assert.Equal(t, "VBlank", cfg.Vectors[NMI].Name)
	assert.Equal(t, uint16(0xfff0), *cfg.Vectors[IRQ].Address)
	assert.Equal(t, "VideoControl", cfg.Constants[0x2000])
	assert.Equal(t, "Score", cfg.Variables[0xc4])
	assert.Equal(t, "Start", cfg.Labels[0x8000])
	assert.Equal(t, "Initialize; retain # and = in comments", cfg.Comments[0x8000])
	assert.Equal(t, []Range{{Start: 0x8000, End: 0x8002}}, cfg.Code)
	assert.Equal(t, []Range{{Start: 0x8003, End: 0x8005}}, cfg.Data)
}

func TestPresentation(t *testing.T) {
	cfg, err := Parse(strings.NewReader("[blank_lines]\n$8000=0\n$8001=8\n" +
		"[comments_before]\n$8000=Heading; # retained\\nNext\n" +
		"[symbol_groups]\nRegisters=Video, Audio\n[symbols]\nVideo=$2000\nAudio=$4000"))
	assert.NoError(t, err)
	assert.Equal(t, 0, cfg.BlankLines[0x8000])
	assert.Equal(t, 8, cfg.BlankLines[0x8001])
	assert.Equal(t, `Heading; # retained\nNext`, cfg.CommentsBefore[0x8000])
	assert.Equal(t, []SymbolGroup{{Heading: "Registers", Names: []string{"Video", "Audio"}}}, cfg.SymbolGroups)
	for _, input := range []string{
		"[blank_lines]\n$8000=-1", "[blank_lines]\n$8000=9", "[blank_lines]\n$8000=one",
		"[blank_lines]\n$8000=1\n0x8000=2", "[comments_before]\n$8000=a\n0x8000=b",
		"[symbol_groups]\nBad=Missing", "[symbols]\nA=1\n[symbol_groups]\nBad=A,A",
		"[symbol_groups]\nBad=Not a symbol", "[symbols]\nA=1\n[symbol_groups]\nBad=a",
	} {
		_, err := Parse(strings.NewReader(input))
		assert.Error(t, err)
	}
}

func TestParseInvalid(t *testing.T) {
	// Malformed entries and structured-section errors must never be silently ignored.
	for _, input := range []string{
		"name=value", "[unknown]", "[labels", "[labels]\nno equals",
		"[labels]\n$8000=", "[labels]\n$8000=bad name", "[labels]\n$10000=Start",
		"[labels]\n$8000=Start\n0x8000=Other", "[labels]\n$8000=Start\n$8001=Start",
		"[rom]\nprg_crc32=invalid", "[rom]\nprg_crc32=100000000", "[rom]\ncrc=0",
		"[vectors]\nother=Start", "[vectors]\nnmi=$10000", "[vectors]\nnmi=bad name",
		"[data]\n$8002=$8000", "[data]\n$8000=$8003\n[code]\n$8003=$8006",
		"[variables]\nScore=$C4\nOther=$C4", "[labels]\n$8000=Start\n$8000=Start",
	} {
		t.Run(input, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(input))
			assert.ErrorContains(t, err, "line ")
			assert.Nil(t, cfg)
		})
	}
}

func TestParseAddress(t *testing.T) {
	for _, value := range []string{"$2000", "0x2000", "8192", "08192", "  $2000  "} {
		address, err := parseAddress(value)
		assert.NoError(t, err)
		assert.Equal(t, uint16(8192), address)
	}
	for _, value := range []string{"", "-1", "$10000", "0x", "invalid"} {
		_, err := parseAddress(value)
		assert.Error(t, err)
	}
}

func TestValidateROM(t *testing.T) {
	prg, chr := []byte{1, 2, 3}, []byte{4, 5}
	input := fmt.Sprintf("[rom]\nprg_crc32=%08X\nchr_crc32=%08X", crc32.ChecksumIEEE(prg), crc32.ChecksumIEEE(chr))
	cfg, err := Parse(strings.NewReader(input))
	assert.NoError(t, err)
	assert.NoError(t, cfg.ValidateROM(prg, chr))
	assert.ErrorContains(t, cfg.ValidateROM(nil, chr), "prg_crc32 mismatch")
	assert.ErrorContains(t, cfg.ValidateROM(prg, nil), "chr_crc32 mismatch")
}

func TestParseReadError(t *testing.T) {
	_, err := Parse(io.MultiReader(strings.NewReader("[labels]\n"), failedReader{}))
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestLibraryParserCompatibility(t *testing.T) {
	cfg, err := Parse(strings.NewReader("\uFEFF; profile\r\n[symbols] ; constants\r\n" +
		"SpriteTable=$8123\n[comments_before]\n$8000=Keep; # and \\n\n" +
		"[symbols]\nFrameCounter=$09\n[symbol_groups]\nZ group=SpriteTable\nA group=FrameCounter"))
	assert.NoError(t, err)
	assert.Equal(t, uint16(0x8123), cfg.Symbols["SpriteTable"])
	assert.Equal(t, uint16(9), cfg.Symbols["FrameCounter"])
	assert.Equal(t, `Keep; # and \n`, cfg.CommentsBefore[0x8000])
	assert.Equal(t, "Z group", cfg.SymbolGroups[0].Heading)
	assert.Equal(t, "A group", cfg.SymbolGroups[1].Heading)
	for _, input := range []string{
		"[Symbols]", "[rom]\nPRG_CRC32=00000000",
		"[symbols]\nName=1\n[symbols]\nName=2",
	} {
		_, err := Parse(strings.NewReader(input))
		assert.ErrorContains(t, err, "line ")
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir() + "/missing.ini")
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

type failedReader struct{}

func (failedReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
