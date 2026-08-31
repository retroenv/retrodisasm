// Package cli handles command line interface logic.
package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/cli"
)

var validAssemblers = []string{"asm6", "ca65", "nesasm", "retroasm"}

// UsageError represents an error that should show usage information.
type UsageError struct {
	flagSet *cli.FlagSet
	msg     string
}

func (e *UsageError) Error() string {
	return e.msg
}

// ShowUsage prints the usage message.
func (e *UsageError) ShowUsage() {
	e.flagSet.ShowUsage()
}

// ParseFlags parses command line flags and returns program and disassembler options.
func ParseFlags() (options.Program, options.Disassembler, error) {
	var opts options.Program
	var pos options.Positional

	fs := cli.NewFlagSet("retrodisasm")
	fs.AddSection("Parameters", &opts.Parameters)
	fs.AddSection("Options", &opts.Flags)
	fs.AddSection("Output options", &opts.OutputFlags)
	fs.AddPositional(&pos)

	args, exportCHR := normalizeCHRArguments(os.Args[1:])
	opts.ExportCHR = exportCHR

	_, err := fs.Parse(args)
	if err != nil {
		return opts, options.Disassembler{}, &UsageError{flagSet: fs}
	}

	// Use positional file argument if -i flag not provided
	if opts.Input == "" {
		opts.Input = pos.File
	}

	if opts.Input == "" && opts.Batch == "" {
		return opts, options.Disassembler{}, &UsageError{flagSet: fs}
	}

	if err := normalizeOptions(&opts); err != nil {
		return opts, options.Disassembler{}, err
	}

	disasmOptions := createDisasmOptions(opts)

	if err := validateOptionCombinations(opts, disasmOptions); err != nil {
		return opts, options.Disassembler{}, err
	}

	return opts, disasmOptions, nil
}

// normalizeCHRArguments allows -chr to act as a boolean flag while retaining
// the standard Go flag syntax -chr=<file> for its optional filename. Moving it
// before positional arguments also lets the flag work after the input filename.
func normalizeCHRArguments(args []string) ([]string, bool) {
	normalized := make([]string, 0, len(args))
	chrArguments := make([]string, 0, 1)
	var exportCHR bool
	parseFlags := true

	for _, arg := range args {
		if arg == "--" {
			parseFlags = false
			normalized = append(normalized, arg)
			continue
		}
		if !parseFlags {
			normalized = append(normalized, arg)
			continue
		}

		switch {
		case arg == "-chr" || arg == "--chr":
			chrArguments = append(chrArguments, arg+"=")
			exportCHR = true
		case strings.HasPrefix(arg, "-chr=") || strings.HasPrefix(arg, "--chr="):
			chrArguments = append(chrArguments, arg)
			exportCHR = true
		default:
			normalized = append(normalized, arg)
		}
	}

	return append(chrArguments, normalized...), exportCHR
}

// validateOptionCombinations checks for incompatible option combinations.
func validateOptionCombinations(opts options.Program, disasmOptions options.Disassembler) error {
	if opts.AssembleTest && disasmOptions.OutputUnofficialAsMnemonics {
		return errors.New("-output-unofficial and -verify cannot be used together: unofficial mnemonics may assemble to different bytes")
	}
	if opts.ExportCHR && opts.CHRFilename != "" && opts.Batch != "" {
		return errors.New("a custom -chr filename cannot be used with -batch; use bare -chr to derive a filename for each ROM")
	}
	return nil
}

// normalizeOptions normalizes and validates option values.
func normalizeOptions(opts *options.Program) error {
	opts.Assembler = strings.ToLower(opts.Assembler)
	if opts.Assembler == "asm6f" {
		opts.Assembler = "asm6"
	}

	if !slices.Contains(validAssemblers, opts.Assembler) {
		return fmt.Errorf("unsupported assembler: %s. Valid options: %s",
			opts.Assembler, strings.Join(validAssemblers, ", "))
	}

	return nil
}

// createDisasmOptions creates disassembler options based on program options.
func createDisasmOptions(opts options.Program) options.Disassembler {
	disasmOptions := options.NewDisassembler(opts.Assembler, opts.System)

	// nesasm doesn't support unofficial instructions in output
	if opts.Assembler == assembler.Nesasm {
		disasmOptions.AssemblerSupportsUnofficial = false
	}

	// Parse base address if provided
	if opts.BaseAddress != "" {
		var baseAddr uint64
		var err error
		// Try parsing with 0x prefix
		if strings.HasPrefix(opts.BaseAddress, "0x") || strings.HasPrefix(opts.BaseAddress, "0X") {
			baseAddr, err = strconv.ParseUint(opts.BaseAddress[2:], 16, 16)
		} else {
			// Parse as hex without prefix
			baseAddr, err = strconv.ParseUint(opts.BaseAddress, 16, 16)
		}
		if err == nil && baseAddr <= 0xFFFF {
			disasmOptions.BaseAddress = uint16(baseAddr)
		}
	}

	// Apply output flag settings
	disasmOptions.Binary = opts.Binary
	disasmOptions.HexComments = !opts.NoHexComments
	disasmOptions.OffsetComments = !opts.NoOffsets
	disasmOptions.OutputUnofficialAsMnemonics = opts.OutputUnofficial
	disasmOptions.SplitBanks = opts.SplitBanks
	disasmOptions.StopAtUnofficial = opts.StopAtUnofficial
	disasmOptions.ZeroBytes = opts.ZeroBytes

	return disasmOptions
}
