# Game annotations

Use `-annotations game.ini` to give a ROM meaningful function and variable names, comments,
and explicit code/data ranges. The same INI works with every supported output
assembler. The existing `-c` flag remains the ca65 linker configuration path.

```sh
retrodisasm -annotations game.ini -o game.asm game.nes
```

## Readability

Without hints, output has one blank line before labels (including named functions
and tables), and between code and data blocks. These rules share one gap rather
than adding gaps together. Standalone comments also start a new block.
There is no guessed function-end rule: a return or jump does not always end a
function, so spacing follows the next label or code/data boundary instead.

```ini
[blank_lines]
$8082 = 2
$8182 = 0

[comments_before]
$8082 = Frame update\nDisplay work followed by game logic.

[symbol_groups]
Video registers = PPU_CTRL_REG1, PPU_CTRL_REG2, PPU_STATUS
```

Explicit counts replace automatic gaps before the comment/label at that address.
Hints inside raw data split the output at that byte; hints inside instructions
require byte output, just like inline comments. Explicit byte/word records may
not span a presentation hint: split the record in the INI first.

Symbol groups reference names defined in `[symbols]`, `[constants]`, or
`[variables]`. Unknown names and repeated group members are errors. Only
definitions actually emitted by the assembler appear; groups do not create new
symbols or move inline ROM labels. Ungrouped definitions retain alphabetical
ordering. An assembler may emit constants and variables separately, so a group
spanning those categories is presented separately in each category.

## Format

All sections are optional. Section and key names are case-sensitive. Addresses
accept `$8000`, `0x8000`, or decimal `32768`; unprefixed values are decimal even
with leading zeros. Use hexadecimal prefixes to avoid ambiguity.

```ini
[rom]
prg_crc32 = 5cf548d3
chr_crc32 = 867b51ad

[vectors]
nmi = NonMaskableInterrupt
reset = Start
irq = $FFF0

[constants]
VideoControl = $2000

[variables]
FrameCounter = $09

[labels]
$8000 = Start
$805A = VideoPointers

[comments]
$8000 = Initialize the game; punctuation such as # is preserved.

[code]
$8000 = $8059

[data]
$805A = $8081
```

| Section | Meaning |
| --- | --- |
| `rom` | Optional CRC32/IEEE of PRG and CHR bytes separately, excluding the iNES header and trainer. Values are hexadecimal. A mismatch is an error. |
| `vectors` | NES handler names for `nmi`, `reset`, `irq`. A numeric value asserts the stored vector address; it never patches the ROM. |
| `constants` | `Name = address`, overriding the default hardware alias for reads and writes. |
| `variables` | `Name = address`, naming RAM references even when used just once. Unused variables are not emitted. Use labels for ROM addresses. |
| `labels` | `address = Name`, naming either code or data without forcing it to be decoded. |
| `comments` | `address = text`, attaching a comment to the exact byte. Comments inside instructions cause byte output so their location is preserved. |
| `comments_before` | `address = text`, standalone comment lines before the label/instruction/data. Use literal `\n` between lines. |
| `blank_lines` | `address = count` (0–8), replacing automatic spacing before that address. Zero suppresses the automatic gap. |
| `symbol_groups` | `heading = Name1, Name2, ...`, ordering related definitions under a comment heading. Group and member order follow the INI. |
| `code` | `start = end`, inclusive ranges to decode, including code after returns or unreachable from interrupt vectors. Normal flow tracing may continue past the range. |
| `data` | `start = end`, inclusive ranges that must remain bytes and stop execution tracing. |
| `symbols` | `Name = value`, additional definitions, including unused constants and aliases sharing a value. Aliases of labeled ROM addresses are emitted as labels so relocatable assemblers can subtract them correctly. |
| `operands` | `instruction address = expression`, selecting the exact symbol or expression for that instruction. The expression must evaluate to the operand encoded in the ROM. Currently supported for NES/6502. |
| `bytes` | `address = expressions`, a comma-separated list of symbolic byte values, or `address = @count` to emit that many literal ROM bytes on a data line. |
| `words` | Like `bytes`, but with little-endian 16-bit elements; `@count` counts words. |

For example:

```ini
[symbols]
BufferAlias = $0300

[operands]
$8000 = BufferAlias+1

[bytes]
$805A = <BufferAlias, >BufferAlias
$8060 = @8

[words]
$8070 = Start, Start+2
```

Expressions accept defined names, `$`/`0x` hexadecimal, `%` binary, decimal,
addition/subtraction, and a leading `<` or `>` selector for the low/high byte of
the result. Omit addressing punctuation such as `#`, parentheses and `,X` from
operand hints: it is reconstructed from the decoded instruction. Values outside
the operand's range, unknown symbols, and expressions that disagree with the ROM
are errors. Byte/word records must cover data, not instructions; use `[data]` to
prevent automatic tracing of their contents. Records cannot overlap one another
or conceal a label/comment inside them.

Use `@count` for literal tables to keep their bytes in the ROM rather than the
INI. The writer preserves symbolic table expressions and record boundaries;
NESASM may split lines or emit bytes where a record crosses its bank boundary.
Its `LOW()`/`HIGH()` syntax is selected automatically. Symbol definitions remain
available even if unused, while redundant inferred names are omitted when
operand hints replace their references.

Names use ASCII letters, digits and underscores, with a letter or underscore
first. Use unique names, including across sections. The address-keyed sections
allow one entry per address; `[symbols]` can supply additional names for a shared
address. Shared interrupt vectors must use the same name.
Duplicate entries, unknown sections, malformed addresses, overlapping ranges and
out-of-ROM annotations are errors. Data ranges must not cut through an instruction
being decoded; adjust the boundary to the actual instruction start.

Blank lines and lines beginning with `;` or `#` are ignored. These characters
also start inline comments except in `[comments]` and `[comments_before]`, where all text after the first
`=` is retained. Comments are single-line text; there are no escape sequences,
includes, or executable directives.

INI code/data hints take precedence over automatic table detection and tracing
requests from CDL. A label alone does not create a code entry point; add a `[code]`
range when needed. NES interrupt-vector bytes themselves are reserved; use
`[vectors]` to name their handlers. `[vectors]` is unavailable for raw binaries or
CHIP-8. Other sections use the selected system's mapped program address space.

Currently, INI addresses refer to the disassembler's default CPU bank mapping.
For mirrored ROMs, use addresses in the emitted PRG range rather than its mirror.
There is no bank-qualified address syntax, so a single file cannot distinguish
different switchable banks at the same CPU address. Use annotations for the
mapped banks only.
