# Changelog for retrodisasm

All notable changes to this project will be documented in this file.

## [v0.4.0] - 2026-09-01

Added:

* add `-split-banks` option to write each PRG bank to a separate assembly file
* add iNES 2.0 mapper support
* add `-chr` option with an optional filename to export CHR-ROM data
* add CHR-ROM addresses to inline data comments
* add ALR, ANC, ARR, AXS, and KIL unofficial 6502 opcodes

Changed:

* generate reassemblable banked NES output across supported assemblers
* split system-specific documentation into NES, CHIP-8, and command-line guides

Fixed:

* fix branches, function references, aliases, and interrupt vectors across PRG banks
* fix missing or duplicated data at PRG bank boundaries
* fix indexed operands misidentifying referenced data as code
* fix data tables misidentified as code by indexed loads
* fix CHIP-8 branch operands losing instruction-specific formatting
* fix BRK handling at the end of raw binaries
* fix multi-file disassembly returning success when one or more files fail
* fix crashes caused by invalid offsets, vectors, and jump-table opcodes

## [v0.3.1] - 2026-01-03

Added:

* add `-base` flag for custom base address in binary mode
* add `-o -` flag for stdout output
* add `-output-unofficial` flag to output unofficial opcodes as mnemonics
* add `-stop-at-unofficial` flag to stop tracing at unofficial opcodes
* add multi-bank ROM support with per-bank vectors

Fixed:

* fix binary mode incorrectly including NES segments
* fix tracing continuing after BRK instruction
* fix unconditional branch detection for complementary branch patterns

## [v0.3.0] - 2025-11-08

Added:

* add CHIP-8 architecture support with retroasm assembler
* add system auto-detection from file extensions (.nes, .ch8, .rom)
* add `-s` flag for manual system specification (nes, chip8)
* add NES ROM information in output file header comments

Changed:

* rename project from nesgodisasm to retrodisasm to reflect multi-system support

Fixed:

* fix instruction overlap detection for unofficial instructions
* fix jump engine detection and processing
* fix mapper validation for empty banks
* fix assembler binary detection
* fix CLI flag registration timing issues

## [v0.2.3] - 2024-11-05

Added:

* add -binary option to read raw binary files without header
* add asm6 multi bank support
* add option to write generated ca65 config to file

Changed:

* improve vectors start address calculation
* use ca65 as default assembler

Fixed:

* fix nmi handler crash
* fix scanning of last zero byte for prg segment


## [v0.2.2] - 2024-01-06

Added:

* add nesasm output compatibility support
* show warning of mapper is not nrom
* start of banking support
* support asm6f as a assembler parameter alias for asm6
* output data lines with address in comments

Fixed:

* fix assembler binary name for verification on windows
* fix crash when disassembling invalid irq handler addresses
* improve code base address handling for large PRG
* improve instruction parameter converter prefix and suffix handling


## [v0.2.1] - 2023-07-19

Added:

* added asm6 output compatibility support

Fixed:

* fix crashes when disassembling nesfab generated roms


## [v0.2.0] - 2023-01-05

Added:

* jump engine detection
* write CRC32 checksums of segments in the output header as comments
* batch processing of multiple input files
* new logger output with different verbosity levels

Changed:

* the project was moved into its own git repository

Fixed:

* data references into unofficial instruction opcodes
* data references before code base start
* instruction overlap with IRQ handler start address
* support different code base addresses
* variable detection for zero page access
* variable usage detection for indirect jumps


## [v0.1.1] - 2022-08-02

Added:

* add var aliases for zeropage accesses
* support code/data logs
* support more mappers
* unofficial instruction opcodes are bundled

Fixed:

* fix wrong address in comments for non standard rom base addresses
* support data references into instruction opcodes


## [v0.1.0] - 2022-06-26

First version of retrodisasm (formerly nesgodisasm) released.
