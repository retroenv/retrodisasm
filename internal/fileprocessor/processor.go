// Package fileprocessor handles file loading and processing operations.
//
// This package provides the main file processing workflow for the retrodisasm
// disassembler, including file loading, system detection, cartridge parsing,
// and output generation. It supports multiple target systems (NES, CHIP-8)
// and assembler formats (ca65, asm6, nesasm, retroasm).
//
// The main entry point is ProcessFile, which orchestrates the complete
// disassembly workflow from input file to assembled output.
package fileprocessor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/pipeline"
	"github.com/retroenv/retrogolib/log"
)

// ProcessFile handles the complete file processing workflow.
func ProcessFile(ctx context.Context, logger *log.Logger, opts options.Program, disasmOptions options.Disassembler) error {
	writer, err := createWriter(opts)
	if err != nil {
		return fmt.Errorf("creating writer: %w", err)
	}
	defer closeWriter(writer)

	pipe := pipeline.New(logger)
	_, err = pipe.Execute(ctx, opts, disasmOptions, writer)
	if err != nil {
		return fmt.Errorf("executing pipeline: %w", err)
	}

	return nil
}

// GetFilesToProcess returns list of files to process based on options.
// It supports batch processing via glob patterns.
func GetFilesToProcess(opts *options.Program) ([]string, error) {
	if opts.Batch != "" {
		matches, err := filepath.Glob(opts.Batch)
		if err != nil {
			return nil, fmt.Errorf("globbing batch pattern: %w", err)
		}
		return matches, nil
	}
	return []string{opts.Input}, nil
}

// GenerateOutputFilename generates output filename for a given input file.
// It replaces the input file extension with .asm.
func GenerateOutputFilename(inputFile string) string {
	ext := filepath.Ext(inputFile)
	return inputFile[:len(inputFile)-len(ext)] + ".asm"
}

// PrintBanner prints application version information.
func PrintBanner(logger *log.Logger, opts options.Program, version, commit, date string) {
	if opts.Quiet {
		return
	}

	versionString := version
	if commit != "" {
		if len(commit) > 7 {
			commit = commit[:7]
		}
		versionString += fmt.Sprintf(" (%s)", commit)
	}

	logger.Info("Retrodisasm", log.String("version", versionString))

	if date != "" && !strings.Contains(date, "unknown") {
		logger.Info("Build", log.String("date", date))
	}
}

// createWriter creates the output writer based on options.
func createWriter(opts options.Program) (io.Writer, error) {
	if opts.Output == "" || opts.Output == options.OutputStdout {
		return os.Stdout, nil
	}

	file, err := os.Create(opts.Output)
	if err != nil {
		return nil, fmt.Errorf("creating output file %s: %w", opts.Output, err)
	}
	return file, nil
}

// closeWriter safely closes the writer if it implements io.Closer.
func closeWriter(writer io.Writer) {
	if closer, ok := writer.(io.Closer); ok {
		_ = closer.Close()
	}
}
