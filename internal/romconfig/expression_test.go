package romconfig

import (
	"strings"
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestEvaluate(t *testing.T) {
	symbols := map[string]uint16{"Table": 0x80ff, "Base": 0x8000}
	for _, test := range []struct {
		expression string
		value      uint16
	}{
		{"Table-Base", 255}, {"<Table+1", 0}, {">Table+1", 0x81},
		{"%1010", 10}, {"0xFF", 255}, {"0010", 10}, {"Table + 2 - 1", 0x8100},
	} {
		value, err := Evaluate(test.expression, symbols)
		assert.NoError(t, err)
		assert.Equal(t, test.value, value)
	}
	for _, expression := range []string{"", "Unknown", "Table+", "-1", "$FFFF+1", "0-1", "Table*2", "Table\n.byte 0"} {
		_, err := Evaluate(expression, symbols)
		assert.Error(t, err)
	}
}

func TestParseExpressionAnnotations(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`[symbols]
Table = $8010
Alias = $8010
[operands]
$8000 = <Table
[bytes]
$8010 = @16
[words]
$8020 = Table, Table+2
`))
	assert.NoError(t, err)
	assert.Equal(t, uint16(0x8010), cfg.Symbols["Alias"])
	assert.Equal(t, "<Table", cfg.Operands[0x8000])
	assert.Equal(t, uint16(16), cfg.Bytes[0x8010].Count)
	assert.Equal(t, []string{"Table", "Table+2"}, cfg.Words[0x8020].Expressions)
	for _, input := range []string{
		"[bytes]\n$8000=@0", "[bytes]\n$8000=@65536", "[words]\n$8000=Table,",
		"[bytes]\n$8000=@1\n0x8000=@2", "[symbols]\nbad name=1",
		"[symbols]\nAlias=1\n[variables]\nAlias=2",
	} {
		_, err := Parse(strings.NewReader(input))
		assert.Error(t, err)
	}
}
