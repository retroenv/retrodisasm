# retrodisasm

[![CI](https://github.com/retroenv/retrodisasm/actions/workflows/go.yaml/badge.svg?branch=main)](https://github.com/retroenv/retrodisasm/actions/workflows/go.yaml)
[![Codecov](https://codecov.io/gh/retroenv/retrodisasm/graph/badge.svg)](https://codecov.io/gh/retroenv/retrodisasm)
[![Release](https://img.shields.io/github/v/release/retroenv/retrodisasm)](https://github.com/retroenv/retrodisasm/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/retroenv/retrodisasm.svg)](https://pkg.go.dev/github.com/retroenv/retrodisasm)
[![License](https://img.shields.io/github/license/retroenv/retrodisasm)](LICENSE)
![LLM assisted: human reviewed](https://img.shields.io/badge/LLM%20assisted-human%20reviewed-6f42c1)

A tracing disassembler for retro systems that generates bit-perfect,
reassemblable assembly source.

## Features

* **Execution-flow tracing** - Differentiates code from data through program-flow analysis
* **Bit-perfect reassembly** - Generated assembly reassembles to the exact same binary
* **Readable output** - Omits trailing zero bytes and replaces known RAM addresses with descriptive aliases
* **Multiple systems** - Supports NES and CHIP-8 input with automatic detection
* **Multiple assemblers** - Generates source for different assembler toolchains
* **Batch verification** - Processes and verifies multiple ROMs in one command
* **Banked output** - Writes PRG banks inline or as separate assembly files
* **CHR-ROM export** - Extracts graphics data into an included `.chr` file

## Supported Systems

| System | Architecture | Guide |
|--------|-------------|-------|
| **CHIP-8** | CHIP-8 VM | [CHIP-8 guide](docs/chip8.md) |
| **NES** | 6502 | [NES guide](docs/nes.md) |

## Quick Start

### Installation

Download a binary for Linux, macOS, or Windows from
[Releases](https://github.com/retroenv/retrodisasm/releases), or install from
source with Go 1.22 or newer:

```bash
go install github.com/retroenv/retrodisasm@latest
```

### Basic Usage

The tool auto-detects the system from file extensions (`.nes`, `.ch8`, `.rom`):

```bash
retrodisasm -o output.asm input.nes      # NES ROM
retrodisasm -o output.asm input.ch8      # CHIP-8 ROM
```

Reassemble and compare NES output with the original ROM:

```bash
retrodisasm -verify input.nes
```

Process and verify a collection of ROMs:

```bash
retrodisasm -verify -batch "roms/*.nes"
```

Export CHR-ROM to a separate file referenced by the generated assembly:

```bash
retrodisasm -chr -o output.asm input.nes
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

See the [command-line reference](docs/options.md) for all options. Reassembly
and automatic verification require the toolchain for the selected output
format; see the [assembler setup guide](docs/assemblers.md) for compatibility
and installation instructions.
