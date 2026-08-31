package nesasm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestExternalCommandUsesSourceDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a-long-batch-path", "with-several", "nested-directories")
	asmFile := filepath.Join(dir, "output.asm")
	outputFile := filepath.Join(t.TempDir(), "output.nes")

	cmd, err := externalCommand(context.Background(), asmFile, outputFile)

	assert.NoError(t, err)
	assert.Equal(t, dir, cmd.Dir)
	assert.Equal(t, []string{assemblerName, "-z", "-o", outputFile, "output.asm"}, cmd.Args)
}
