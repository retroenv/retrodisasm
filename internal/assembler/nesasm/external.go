// Package nesasm provides helpers to create nesasm assembler compatible asm output.
package nesasm

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const assemblerName = "nesasm"

// AssembleUsingExternalApp calls the external assembler and linker to generate a .nes
// ROM from the given asm file.
func AssembleUsingExternalApp(ctx context.Context, asmFile, outputFile string) error {
	if _, err := exec.LookPath(assemblerName); err != nil {
		return fmt.Errorf("%s is not installed", assemblerName)
	}

	cmd, err := externalCommand(ctx, asmFile, outputFile)
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("assembling file: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

func externalCommand(ctx context.Context, asmFile, outputFile string) (*exec.Cmd, error) {
	outputPath, err := filepath.Abs(outputFile)
	if err != nil {
		return nil, fmt.Errorf("resolving output path: %w", err)
	}
	cmd := exec.CommandContext(ctx, assemblerName, "-z", "-o", outputPath, filepath.Base(asmFile))
	cmd.Dir = filepath.Dir(asmFile)
	return cmd, nil
}
