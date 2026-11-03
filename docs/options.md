# Command-Line Options Reference

Complete reference for all retrodisasm command-line options.

## Usage

```
retrodisasm [options] [file]
```

## Parameters

### Input/Output

#### `-i string`
Input ROM file path.
```bash
retrodisasm -i game.nes -o game.asm
```

#### `-o string`
Output assembly file path. Defaults to `<input>.asm`. Use `-` for stdout.
```bash
retrodisasm -o output.asm input.nes
retrodisasm -o - input.nes > output.asm
```

#### `file` (positional)
Alternative way to specify input file.
```bash
retrodisasm input.nes -o output.asm
```

### System & Format

#### `-s string`
Target system: `nes`, `chip8`. Auto-detects from file extension if not specified.
```bash
retrodisasm -s nes game.bin
retrodisasm -s chip8 program.rom
```

Auto-detection rules:
* `.nes` → NES
* `.ch8` → CHIP-8
* `.rom` → CHIP-8

#### `-a string`
Assembler format. Default: `ca65`

Supported formats by system:
* **NES:** `asm6`, `ca65`, `nesasm`, `retroasm`
* **CHIP-8:** `retroasm`

```bash
retrodisasm -a asm6 -o output.asm input.nes
retrodisasm -a retroasm -o output.asm input.ch8
```

#### `-binary`
Treat input as raw binary without system-specific header.
```bash
retrodisasm -binary -base 8000 -o output.asm prg.bin
```

#### `-base string`
Base address for `-binary` mode in hexadecimal (e.g., `0200`, `8000`).
```bash
retrodisasm -binary -base C000 -o output.asm code.bin
```

### NES-Specific Options

#### `-chr[=file]`

Export CHR-ROM and reference it from the generated assembly with an `_chr_0000` label. Without a filename, the output uses the assembly filename with a `.chr` extension. Use the equals form to provide a custom filename.

```bash
retrodisasm -chr -o build/game.asm game.nes
# Writes build/game.chr and includes "game.chr" from build/game.asm

retrodisasm -chr=assets/tiles.chr -o build/game.asm game.nes
# Writes assets/tiles.chr and includes "../assets/tiles.chr"
```

This option requires an NES cartridge with CHR-ROM and is not available in `-binary` mode. With `-batch`, use bare `-chr` so each ROM receives its own derived filename.

#### `-c string`
ca65 linker configuration file path.
```bash
retrodisasm -c custom.cfg -o output.asm input.nes
```

#### `-annotations string`

Load ROM names, comments, code/data ranges and optional checksum validation from
an annotations file. See the [annotation guide](annotations.md).

```bash
retrodisasm -annotations game.ini -o output.asm input.nes
```

#### `-cdl string`

Code/Data Log file (.cdl) from emulators like FCEUX or Mesen.
```bash
retrodisasm -cdl game.cdl -o output.asm input.nes
```

#### `-split-banks`

Write each PRG bank to a separate assembly file and include those files from
the main output. Bank filenames are derived from the output filename.

```bash
retrodisasm -split-banks -o build/game.asm game.nes
# Writes build/game.asm and files such as build/game_000.asm
```

### Processing Options

#### `-batch string`
Batch process files matching glob pattern. Generates `<filename>.asm` for each match.
```bash
retrodisasm -batch "*.nes"
retrodisasm -batch "roms/*.ch8"
```

#### `-verify`
Verify output by reassembling and comparing to input. Requires the assembler (asm6, ca65, or nesasm) to be installed.
```bash
retrodisasm -verify -o output.asm input.nes
```

Verification uses the assembler specified with `-a` flag (default: ca65).

Note: Not compatible with `-output-unofficial`. Retroasm is not supported for verification.

#### `-quality`

Write NES quality signals to stderr, also in quiet mode. Heuristic signals
do not prove an error.

The report counts physical PRG ROM bytes and instruction starts. Aliases of
one ROM location do not add duplicate bytes, instructions, or warnings. A byte
that is code in one output view does not also count as data. A warning must
apply to each code view of its physical location. For example, a target that
one view resolves does not cause a `bad-target` warning for another view.
The report counts the suspicious instructions, which often are data that
the analysis decoded as code:

* `brk`, `decimal` (`sed`), and `unofficial` instructions.
* `unused-address`: an access to the unused CPU test or expansion area.
  PPU register mirrors use the access rules of their base register.
* `write-only-read` and `read-only-write`: a read of a write-only register,
  or a write to the PPU status register. Memory instructions that read and
  then write are checked for both access types.
* `rom-write`: a write to ROM on a cartridge with mapper 0.
* `bad-target`: a branch or jump in the bank to an address that is not the
  start of an instruction.
* `code-as-data`: an official instruction that the output writes as data
  bytes. `bit` instructions are not part of this count.

For an indexed absolute access, a hardware warning must apply to all 256
possible index values. The check includes PPU mirrors and address wrap.
The base address alone does not determine the effective address.

The report also counts possible missed code. The scan checks inside data
runs, including packed byte records and runs with initial padding. It caches
suffix results and decodes each byte once, without a candidate length limit.
A candidate must access hardware or call a subroutine, and end with a return
or jump. It must contain at least 8 bytes and 4 instructions. A direct branch,
jump, or call within the candidate must target an instruction start. If a
transfer targets an operand, the scan rejects the prefix through that transfer
and checks the suffix. Targets outside the candidate remain unknown; they can
use other banks or RAM. The report shows up to 20 candidate locations, with
their bank, CPU address, physical ROM offset, byte count, and instruction count.
These are heuristic candidates.
Aliases do not add duplicate candidate starts or bytes. A location that is
code in another output view is excluded from the missed-code scan.

Indirect jump checks include both pointer reads. They use the NES CPU page
wrap rule and the existing hardware access warnings. For example,
`JMP ($428E)` on a mapper without expansion registers has an `unused-address`
warning. Such a jump also excludes a possible missed-code run. These warnings
do not prove that an instruction is data.

```bash
retrodisasm -quality -o output.asm input.nes
# quality: code 22798 bytes, data 9970 bytes, instructions 10703, suspicious 7 (0.07%), possible missed code 0 bytes in 0 runs
# quality:   unofficial       6
# quality:   code-as-data     1
# quality:   bank 0 $F349 ROM $007349: code-as-data
# quality:   bank 0 $F34A ROM $00734A: unofficial
```

### Output Formatting

#### `-nohexcomments`
Omit hex opcode bytes in assembly comments.

Default output:
```asm
lda #$10    ; $8002 A9 10
```

With `-nohexcomments`:
```asm
lda #$10    ; $8002
```

#### `-nooffsets`
Omit CPU and PPU addresses in assembly comments, including addresses on inline
CHR-ROM data.

Default output:
```asm
lda #$10    ; $8002 A9 10
```

With `-nooffsets`:
```asm
lda #$10    ; A9 10
```

#### `-z`
Include trailing zero bytes in banks. By default, trailing zeros are omitted for cleaner output.
```bash
retrodisasm -z -o output.asm input.nes
```

### Undocumented Opcodes (NES)

#### `-output-unofficial`
Use mnemonics for unofficial/undocumented 6502 opcodes instead of `.byte` directives.
```bash
retrodisasm -output-unofficial -o output.asm input.nes
```

Note: Not compatible with `-verify` as unofficial mnemonics may not be supported by all assemblers.

#### `-stop-at-unofficial`
Stop tracing execution at unofficial opcodes unless they are branched to explicitly.
```bash
retrodisasm -stop-at-unofficial -o output.asm input.nes
```

### Logging & Debugging

#### `-debug`
Enable debug logging for troubleshooting.
```bash
retrodisasm -debug -o output.asm input.nes
```

#### `-q`
Quiet mode. Suppress non-error output.
```bash
retrodisasm -q -o output.asm input.nes
```

## Examples

### Basic Disassembly
```bash
retrodisasm game.nes
retrodisasm -o custom.asm game.nes
```

### With Verification
```bash
retrodisasm -verify game.nes
```

### Using CDL File
```bash
retrodisasm -cdl game.cdl game.nes
```

### Batch Processing
```bash
retrodisasm -batch "roms/*.nes"
```

### Clean Output
```bash
retrodisasm -nohexcomments -nooffsets -o clean.asm game.nes
```

### Binary Mode
```bash
retrodisasm -binary -base 8000 -o prg.asm prg.bin
```

### CHIP-8 Disassembly
```bash
retrodisasm -s chip8 program.ch8
retrodisasm -s chip8 -batch "*.ch8"
```

### To Stdout
```bash
retrodisasm -o - game.nes | less
```

## Installation Requirements

Prebuilt releases are available for Linux, macOS, and Windows. Building from
source requires Go 1.25 or newer.

### Optional Dependencies

See [Assembler Setup](assemblers.md) for the supported toolchains, verification
compatibility, and installation instructions.
