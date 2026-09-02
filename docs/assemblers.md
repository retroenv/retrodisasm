# Assembler Setup

retrodisasm generates source for external assemblers. Install the toolchain for
the selected output format and ensure its executable is available on `PATH`.

## Compatibility

| Format | Systems | Executables | Reassembly | `-verify` |
|--------|---------|-------------|------------|-----------|
| asm6 | NES | `asm6f` | Yes | Yes |
| ca65 | NES | `ca65`, `ld65` | Yes | Yes |
| nesasm | NES | `nesasm` | Yes | Yes |
| retroasm | CHIP-8, NES | `retroasm` | Yes | No |

Automatic verification invokes the selected assembler, rebuilds the ROM, and
compares it with the input. Retroasm output can be reassembled manually but is
not supported by `-verify`.

## asm6f

retrodisasm requires [asm6f](https://github.com/freem/asm6f) v1.6
modifications v03 or newer. Download the current archive from the
[asm6f releases](https://github.com/freem/asm6f/releases), or build it from
source on Linux, macOS, or Windows with a C compiler:

```bash
git clone https://github.com/freem/asm6f.git
cd asm6f
make
```

Copy the resulting `asm6f` or `asm6f.exe` executable to a directory on `PATH`.

## ca65

ca65 and ld65 are part of the [cc65](https://github.com/cc65/cc65) toolchain.
On Debian and Ubuntu:

```bash
sudo apt install cc65
```

To build the current version from source:

```bash
git clone https://github.com/cc65/cc65.git
cd cc65
make
sudo make avail
```

Windows packages and additional installation details are available in the
[cc65 getting-started guide](https://cc65.github.io/getting-started.html).

## nesasm

retrodisasm targets [nesasm CE](https://github.com/ClusterM/nesasm). Build it
on Linux with:

```bash
git clone https://github.com/ClusterM/nesasm.git
cd nesasm/source
make all
```

Copy the resulting `nesasm` executable to a directory on `PATH`. The upstream
README also documents the MSYS2 dependencies for Windows and the
`argp-standalone` dependency for macOS.

## retroasm

Install [retroasm](https://github.com/retroenv/retroasm) with Go 1.24 or newer:

```bash
go install github.com/retroenv/retroasm/cmd/retroasm@latest
```

Ensure the Go binary directory, normally `$HOME/go/bin`, is on `PATH`.

## Check the Installation

On Linux and macOS, confirm that the required executables are discoverable:

```bash
command -v asm6f
command -v ca65
command -v ld65
command -v nesasm
command -v retroasm
```

Only the executables for the formats you use need to be installed.
