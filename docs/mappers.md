# NES Mapper Support

retrodisasm analyzes ROMs by PRG layout model. It rejects a ROM whose mapper
has no model.

| Model | PRG layout | Analysis |
|-------|------------|----------|
| Fixed | One fixed mapping of 16 or 32 KiB | Single trace; CHR switching does not change PRG addresses |
| Fixed last bank | Switchable 16 KiB at `$8000`, last bank fixed at `$C000` | Each bank is traced with the fixed bank |
| Switchable 32 KiB | Switchable 32 KiB at `$8000-$FFFF` | Each bank is traced with its own vectors |
| Serial-register banking | 16 KiB switching with either end fixed, or paired 32 KiB | Follows the serial control and PRG register writes; standard boards with 32-256 KiB PRG |

For banked mappers, retrodisasm follows direct control flow and known register
values at `STA`, `STX`, and `STY` writes to find the bank mappings to trace.
Each mapping is traced independently, so identical vector addresses in different
banks do not hide code. The exploration is limited to 100,000 control-flow
locations and 64 additional mappings. When a limit is reached, a warning is
logged and tracing continues in the mappings found so far.

Output contains each physical bank once, also with `-split-banks`. Labels in
banked ROMs get a `_bankN_atXXXX` suffix. If mappings disagree about a
byte range, the output keeps the original bytes and adds a comment.

CDL data and configured code ranges also provide entry points. Annotation
addresses apply to every mapping; bank-specific annotations are not supported.

This is static analysis, not CPU emulation. RAM values, indirect stores,
read-modify-write mapper operations, and computed jumps are not modeled.
Untraced bytes remain data. Boards that reuse CHR register bits to select more
than 256 KiB of PRG need additional board models.
