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

#### `-cdl string`
Code/Data Log file (.cdl) from emulators like FCEUX or Mesen.
```bash
retrodisasm -cdl game.cdl -o output.asm input.nes
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
Omit file offsets in assembly comments.

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

## System Requirements

retrodisasm uses a modern software stack with minimal system dependencies:

* **Linux:** 2.6.32+
* **Windows:** 10+
* **macOS:** 10.15 Catalina+

### Optional Dependencies

* **ca65** - For reassembling ca65 format output and verification
* **asm6f** - For reassembling asm6 format output and verification (v1.6 modifications v03+ required)
* **nesasm** - For reassembling nesasm format output and verification
* **retroasm** - For reassembling retroasm format output (NES and CHIP-8, verification not supported)
