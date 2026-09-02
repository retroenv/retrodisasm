# CHIP-8 Disassembly Guide

This guide covers CHIP-8-specific features and usage of retrodisasm.

## CHIP-8 Features

* **Execution Flow Tracing** - Differentiates between code and data through program flow analysis
* **Complete Instruction Set** - Handles all standard CHIP-8 instructions (35 opcodes)
* **retroasm Output** - Generates [retroasm](https://github.com/retroenv/retroasm) compatible assembly

## Usage

### Basic Disassembly

Auto-detection from `.ch8` or `.rom` extensions:
```bash
retrodisasm -o output.asm program.ch8
retrodisasm -o output.asm game.rom
```

Explicit system specification:
```bash
retrodisasm -s chip8 -o output.asm program.bin
```

### Assembler

CHIP-8 disassembly only supports the retroasm format:
```bash
retrodisasm -a retroasm -o output.asm program.ch8
```

See [Assembler Setup](assemblers.md) for installation instructions.

## Binary Mode

Disassemble raw CHIP-8 binary:
```bash
retrodisasm -s chip8 -binary -base 0200 -o output.asm program.bin
```

CHIP-8 programs typically start at address `0x0200`.

## Batch Processing

Process multiple CHIP-8 ROMs:
```bash
retrodisasm -s chip8 -batch "*.ch8"
retrodisasm -s chip8 -batch "*.rom"
```

## Output Customization

### Remove Comments

Omit hex opcode bytes in comments:
```bash
retrodisasm -s chip8 -nohexcomments -o output.asm program.ch8
```

Omit file offsets in comments:
```bash
retrodisasm -s chip8 -nooffsets -o output.asm program.ch8
```

## Reassembly

After disassembly, reassemble with retroasm:
```bash
retroasm output.asm -o output.ch8
```

## CHIP-8 Instruction Set

retrodisasm supports all 35 standard CHIP-8 instructions:

* Display: `CLS`, `DRW`
* Flow: `JP`, `CALL`, `RET`, `SE`, `SNE`
* Arithmetic: `ADD`, `SUB`, `SUBN`, `OR`, `AND`, `XOR`, `SHR`, `SHL`
* Memory: `LD` (various addressing modes)
* Random: `RND`
* Timers: `LD DT`, `LD ST`
* Keyboard: `SKP`, `SKNP`
* BCD: `LD B`
* Register operations: `LD [I]`, `LD F`

## Notes

* CHIP-8 is a virtual machine architecture, not a physical system
* The standard memory layout starts programs at address `0x0200`
* retrodisasm traces execution flow to identify code vs data
* Verification (reassembly comparison) is not yet implemented for CHIP-8
