// Package options contains the program options.
package options

import (
	"io"
	"strings"

	"github.com/retroenv/retrogolib/arch"
)

// OutputStdout is the special output value that indicates stdout.
const OutputStdout = "-"

// Positional contains positional arguments.
type Positional struct {
	File string `arg:"positional" usage:"file to disassemble"`
}

// Parameters contains file path options.
type Parameters struct {
	Input       string `flag:"i" usage:"input ROM file"`
	Output      string `flag:"o" usage:"output .asm file (default: <input>.asm, use - for stdout)"`
	CHRFilename string `flag:"chr" usage:"export CHR-ROM (default: <output>.chr; custom: -chr=<file>)"`
	Config      string `flag:"c" usage:"ca65 linker config file"`
	CodeDataLog string `flag:"cdl" usage:"Code/Data log file (.cdl)"`
	Batch       string `flag:"batch" usage:"batch process files matching pattern (e.g. *.nes)"`
}

// Flags contains behavior options.
type Flags struct {
	Assembler    string `flag:"a" usage:"assembler format: asm6, ca65, nesasm, retroasm" default:"ca65"`
	System       string `flag:"s" usage:"target system: nes, chip8 (default: auto-detect)"`
	Binary       bool   `flag:"binary" usage:"treat input as raw binary without header"`
	BaseAddress  string `flag:"base" usage:"base address for -binary mode in hex (e.g. 0200, 8000)"`
	AssembleTest bool   `flag:"verify" usage:"verify output by reassembling and comparing to input"`
	Debug        bool   `flag:"debug" usage:"enable debug logging"`
	Quiet        bool   `flag:"q" usage:"quiet mode"`
}

// OutputFlags contains output formatting options.
type OutputFlags struct {
	NoHexComments    bool `flag:"nohexcomments" usage:"omit hex opcode bytes in comments"`
	NoOffsets        bool `flag:"nooffsets" usage:"omit CPU and PPU addresses in comments"`
	OutputUnofficial bool `flag:"output-unofficial" usage:"use mnemonics for unofficial opcodes (incompatible with -verify)"`
	SplitBanks       bool `flag:"split-banks" usage:"write each PRG bank as a separate .asm file"`
	StopAtUnofficial bool `flag:"stop-at-unofficial" usage:"stop tracing at unofficial opcodes unless branched to"`
	ZeroBytes        bool `flag:"z" usage:"include trailing zero bytes in banks"`
}

// Program options of the disassembler.
type Program struct {
	Parameters
	Flags
	OutputFlags

	ExportCHR bool
}

// Disassembler defines options to control the disassembler.
type Disassembler struct {
	Assembler   string        // what assembler to use
	CodeDataLog io.ReadCloser // Code/Data log file to parse
	System      arch.System   // system type (e.g., nes, chip8)

	AssemblerSupportsUnofficial bool   // assembler can output unofficial opcodes (false for nesasm)
	BaseAddress                 uint16 // base address for code (0 = auto/default)
	Binary                      bool
	CodeOnly                    bool
	HexComments                 bool
	OffsetComments              bool
	OutputUnofficialAsMnemonics bool // output unofficial opcodes as mnemonics instead of .byte
	SplitBanks                  bool // write each PRG bank as a separate .asm file
	StopAtUnofficial            bool // stop tracing at unofficial opcodes unless explicitly branched to
	ZeroBytes                   bool
	CHRFilename                 string // CHR-ROM include path
	OutputFilename              string // output filename for bank file generation
}

// NewDisassembler returns a new options instance with default options.
func NewDisassembler(assemblerName, system string) Disassembler {
	return Disassembler{
		Assembler: strings.ToLower(assemblerName),
		System:    arch.System(system),

		HexComments:    true,
		OffsetComments: true,
	}
}
