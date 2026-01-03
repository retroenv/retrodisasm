# NES Disassembly Guide

This guide covers NES-specific features and usage of retrodisasm.

## NES Features

### Core Capabilities
* **Bit-Perfect Reassembly** - Generated assembly reassembles to produce the exact same ROM binary
* **Execution Flow Tracing** - Differentiates between code and data through program flow analysis
* **Multiple Assembler Outputs** - Generates assembly compatible with asm6, ca65, nesasm, and retroasm
* **Smart Output** - Does not output trailing zero bytes by default

### NES-Specific Features
* Outputs [asm6](https://github.com/freem/asm6f)*/[ca65](https://github.com/cc65/cc65)/[nesasm](https://github.com/ClusterM/nesasm)/[retroasm](https://github.com/retroenv/retroasm) compatible assembly files
* Translates known RAM addresses to aliases
* Supports undocumented 6502 CPU opcodes
* Handles branching into opcode parts of instructions
* Experimental support for mappers with banking

\* `asm6f v1.6 (modifications v03)` or later is required

## Usage

### Basic Disassembly

Auto-detection from `.nes` extension:
```bash
retrodisasm -o output.asm input.nes
```

Explicit system specification:
```bash
retrodisasm -s nes -o game.asm game.bin
```

### Assembler Selection

Choose your preferred assembler with the `-a` flag:

```bash
retrodisasm -a asm6 -o output.asm input.nes
retrodisasm -a ca65 -o output.asm input.nes    # default
retrodisasm -a nesasm -o output.asm input.nes
retrodisasm -a retroasm -o output.asm input.nes
```

### Example Output

```asm
Reset:
  sei                            ; $8000 78
  cld                            ; $8001 D8
  lda #$10                       ; $8002 A9 10
  sta PPU_CTRL                   ; $8004 8D 00 20
...
```

### Reassembly

After disassembly, you can reassemble to verify bit-perfect output:

**ca65:**
```bash
ca65 output.asm -o output.o && ld65 output.o -t nes -o output.nes
```

**asm6:**
```bash
asm6f output.asm output.nes
```

**nesasm:**
```bash
nesasm output.asm -o output.nes
```

**retroasm:**
```bash
retroasm output.asm -o output.nes
```

## NES-Specific Options

### Code/Data Log (CDL) Support

Use CDL files from emulators to guide disassembly:
```bash
retrodisasm -cdl game.cdl -o output.asm input.nes
```

### ca65 Linker Configuration

Specify a custom linker config:
```bash
retrodisasm -c custom.cfg -o output.asm input.nes
```

### Verification

Automatically reassemble and verify bit-perfect output (requires asm6, ca65, or nesasm):
```bash
retrodisasm -verify -o output.asm input.nes
```

The verification uses the assembler specified with `-a` flag (default: ca65).

### Undocumented Opcodes

Output mnemonics for unofficial 6502 opcodes:
```bash
retrodisasm -output-unofficial -o output.asm input.nes
```

Stop tracing at unofficial opcodes unless branched to:
```bash
retrodisasm -stop-at-unofficial -o output.asm input.nes
```

### Binary Mode

Disassemble raw binary without NES header:
```bash
retrodisasm -binary -base 8000 -o output.asm prg.bin
```

## Batch Processing

Process multiple NES ROMs at once:
```bash
retrodisasm -batch "*.nes"
```

This generates `<romname>.asm` for each ROM file.

## Output Customization

### Remove Comments

Omit hex opcode bytes in comments:
```bash
retrodisasm -nohexcomments -o output.asm input.nes
```

Omit file offsets in comments:
```bash
retrodisasm -nooffsets -o output.asm input.nes
```

### Include Trailing Zeros

Include trailing zero bytes in banks (default is to omit):
```bash
retrodisasm -z -o output.asm input.nes
```

## Mapper Support

retrodisasm has experimental support for NES mappers with banking. The tool attempts to trace execution flow across bank switches and generate appropriate assembly output.

## Known Limitations

* Mapper support is experimental and may not handle all banking scenarios
* Some complex self-modifying code patterns may not disassemble perfectly
* Verification requires the assembler (asm6, ca65, or nesasm) to be installed in your PATH
