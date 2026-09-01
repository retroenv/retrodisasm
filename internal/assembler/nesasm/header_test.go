package nesasm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

func TestWriteROMHeaderIncludesBattery(t *testing.T) {
	var output bytes.Buffer
	fileWriter := FileWriter{
		app:        &program.Program{Battery: 1},
		mainWriter: &output,
	}

	assert.NoError(t, fileWriter.writeROMHeader())
	assert.True(t, strings.Contains(output.String(), ".inesbat 1"))
}
