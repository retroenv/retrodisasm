# retrodisasm - a tracing disassembler for retro systems

[![Build status](https://github.com/retroenv/retrodisasm/actions/workflows/go.yaml/badge.svg?branch=main)](https://github.com/retroenv/retrodisasm/actions)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/retroenv/retrodisasm)
[![codecov](https://codecov.io/gh/retroenv/retrodisasm/branch/main/graph/badge.svg?token=NS5UY28V3A)](https://codecov.io/gh/retroenv/retrodisasm)

> **Note:** This project was renamed from `nesgodisasm` to `retrodisasm` to reflect its expanded support for multiple retro systems beyond just NES.

retrodisasm is a tracing disassembler for retro console and computer systems that generates bit-perfect reassemblable assembly code.

## Features

* **Bit-Perfect Reassembly** - Generated assembly reassembles to the exact same binary
* **Execution Flow Tracing** - Differentiates code from data through program flow analysis
* **Multi-Architecture** - Modular design supporting multiple retro systems
* **Multiple Assemblers** - Output compatible with various assemblers
* **CHR-ROM Export** - Optionally extract graphics data into an included `.chr` file
* **Batch Processing** - Process multiple ROMs at once
* **Smart Output** - Omits trailing zeros, translates RAM addresses to aliases

## Supported Systems

| System | Architecture | Assemblers | Documentation |
|--------|-------------|------------|---------------|
| **NES** | 6502 | asm6, ca65, nesasm, retroasm | [docs/nes.md](docs/nes.md) |
| **CHIP-8** | CHIP-8 VM | retroasm | [docs/chip8.md](docs/chip8.md) |

## Quick Start

### Installation

**Option 1:** Download a binary from [Releases](https://github.com/retroenv/retrodisasm/releases)

**Option 2:** Install from source:
```bash
go install github.com/retroenv/retrodisasm@latest
```

### Basic Usage

The tool auto-detects the system from file extensions (`.nes`, `.ch8`, `.rom`):

```bash
retrodisasm -o output.asm input.nes      # NES ROM
retrodisasm -o output.asm input.ch8      # CHIP-8 ROM
```

Example output (NES):
```asm
Reset:
  sei                            ; $8000 78
  cld                            ; $8001 D8
  lda #$10                       ; $8002 A9 10
  sta PPU_CTRL                   ; $8004 8D 00 20
...
```

Reassemble with ca65:
```bash
ca65 output.asm -o output.o && ld65 output.o -t nes -o output.nes
```

## Documentation

* **[Command-Line Options Reference](docs/options.md)** - Complete CLI documentation
* **[NES Disassembly Guide](docs/nes.md)** - NES-specific features and usage
* **[CHIP-8 Disassembly Guide](docs/chip8.md)** - CHIP-8-specific features and usage

## System Requirements

* **Linux:** 2.6.32+
* **Windows:** 10+
* **macOS:** 10.15 Catalina+

Optional: assembler tools for reassembly (ca65, asm6f, nesasm, retroasm) and verification (ca65, asm6f, nesasm).
