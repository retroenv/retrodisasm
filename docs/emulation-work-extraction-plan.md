# Extraction Plan: `emulation_work` Branch

This document maps out how to extract the `emulation_work` branch (~12,400 additions
across 50 files) into clean, reviewable PRs. Changes are organized into dependency tiers
so each PR can be reviewed and merged independently once its prerequisites land.

**Branch stats:** 50 files changed, 12,410 insertions, 284 deletions.

---

## Tier 1 - Independent Bug Fixes

No dependencies on each other or on later tiers. Can land in any order.

### PR 1: Guard nil opcode in jump engine context scan

**Summary:** Prevent nil-pointer panic when `GetContextDataReferences` encounters
a nil opcode entry.

**Files:**
- `internal/jumpengine/jumpengine.go`

**Size:** ~2 lines changed

**What changed:** Added `opcode == nil` guard before calling `opcode.Instruction()`,
preventing a nil dereference when iterating DisasmOffset entries that have no opcode set.

---

### PR 2: Non-zero exit code on multi-file disassembly errors

**Summary:** Exit with code 1 when any file fails disassembly, so scripts and CI
detect failures.

**Files:**
- `main.go`

**Size:** ~6 lines added

**What changed:** Introduced a `hadErrors` flag that is set when any file's disassembly
fails. After the loop, `os.Exit(1)` is called if errors occurred. Previously, errors were
logged but the process exited 0.

---

### PR 3: Fix `LastNonZeroByte` return value + add `BaseAddress` field

**Summary:** Return 0 (not `endIndex`) when no non-zero byte is found, and add
`BaseAddress` field to `PRGBank` for multi-bank address tracking.

**Files:**
- `internal/program/prg.go`

**Size:** ~5 lines changed

**What changed:**
- `LastNonZeroByte` returned `endIndex` on failure, making callers believe the entire
  bank contained valid data. Now returns 0 to signal "bank is empty."
- Added `BaseAddress uint16` field to `PRGBank`. This field is consumed by Tier 4
  output PRs, but the struct change is small and self-contained.

---

### PR 4: Mark indexed-addressing targets as data

**Summary:** Prevent data tables referenced by indexed loads (`LDA table,X`) from
being misidentified as code by the execution tracer.

**Files:**
- `internal/arch/m6502/parser.go`

**Size:** ~23 lines added

**What changed:** In `replaceParamByAlias`, when the instruction uses indexed
addressing and the reference is in the code region, the new `markIndexedReferenceAsData`
helper sets the offset type to `DataOffset` and seeds `offsetInfo.Data` with the actual
byte. This prevents `followExecutionFlow` from falling through into table data and
decoding it as 6502 instructions.

---

### PR 5: Replace `WriteString(Sprintf(...))` with `Fprintf`

**Summary:** Lint cleanup -- eliminate unnecessary intermediate string allocation in
CHIP-8 retroasm writer.

**Files:**
- `internal/assembler/retroasm/chip8.go`

**Size:** ~4 lines changed (2 call sites)

**What changed:** `buf.WriteString(fmt.Sprintf(...))` replaced with `fmt.Fprintf(&buf, ...)`
for direct writes to the `strings.Builder`. No behavioral change.

---

## Tier 2 - Foundational Infrastructure

These PRs introduce the building blocks that Tiers 3-5 depend on.

### PR 6: ParseKey -- mapping-aware parse deduplication

**Summary:** Key the parse-visited and branch-destination sets by `(PC, MappingID)`
instead of plain `uint16`, so the same CPU address under different bank mappings is
not falsely deduplicated.

**Dependencies:** None (but consumed by everything in Tiers 3-5)

**Files:**
- `internal/disasm/parsekey.go` (new, 14 lines)
- `internal/disasm/parsekey_test.go` (new, 105 lines)
- `internal/disasm/parser.go` (refactor: sets/slices keyed by `ParseKey`)
- `internal/disasm/disasm.go` (field type changes: `branchDestinations`, `offsetsToParse`,
  `offsetsToParseAdded`, `offsetsParsed`, `functionReturnsToParse`,
  `functionReturnsToParseAdded` all become `ParseKey`-keyed)
- `internal/disasm/data.go` (use `currentParseKey` for `offsetsParsed` lookups)

**Size:** ~120 lines new, ~70 lines refactored

**What changed:** `ParseKey` is a `(PC uint16, MappingID uint64)` pair. `MappingID` comes
from `mapper.MappingSignature()`. All parse-queue and visited-set data structures
throughout the disassembler are re-keyed from `uint16` to `ParseKey`. The `data.go`
fix for `ChangeAddressRangeToCodeAsData` is included because it changes the same
`offsetsParsed` key type.

---

### PR 7: Mapper API extensions

**Summary:** Add bank introspection, mapping snapshots, and bank-switch simulation to
the mapper, enabling multi-bank traversal and emulation-driven tracing.

**Dependencies:** None (but consumed by Tiers 3-5)

**Files:**
- `internal/mapper/mapper.go` (~210 lines added)
- `internal/mapper/mapper_test.go` (new, 369 lines)
- `internal/mapper/bank.go` (new, 16 lines -- `setBankVectorsFromSlice` helper)

**Size:** ~595 lines new/changed

**What changed:** New methods on `Mapper`:
- `BankCount()`, `BankVectors(bankIndex)` -- bank introspection
- `MapBank(bankIndex)`, `RestoreDefaultMapping()`, `RestoreFixedUpperMapping()` -- bank remapping
- `MappingSignature()` -- FNV-1 hash of current mapping state (used by `ParseKey`)
- `RestoreMappingSignature(sig)` -- restore a previously seen mapping from snapshot cache
- `ResolveAddress(address)` -- CPU address to `(bankID, physicalOffset)` translation
- `EmittedAddressOfOffset(offsetInfo)` -- reverse-map offset to assembly output address
- `MapperRegisterDescription(address)` -- human-readable mapper register names
- `rememberCurrentMapping()` -- records mapping snapshots for later restoration

New fields: `mapperNumber`, `mappingSnapshots map[uint64][]mappedBank`,
`mmc1`/`mmc5` runtime structs, offset address cache.

Package-level: `prgWindowAddresses = []uint16{0x8000, 0xA000, 0xC000, 0xE000}`.

---

### PR 8: Trace statistics infrastructure

**Summary:** Add detailed counters for every tracing decision point, with structured
debug logging at end of trace.

**Dependencies:** None

**Files:**
- `internal/disasm/stats.go` (new, 114 lines)

**Size:** ~114 lines

**What changed:** `traceStats` struct with 50+ counters covering queue lifecycle,
branch destinations, additional bank processing, cross-bank call seeding,
split-pointer-table seeding (11 distinct rejection buckets), parsing execution,
and code-as-data reclassification. `resetTraceStats()` and
`logTraceStats(start, completed)` methods. Stats are reset in `Process` and logged
on defer.

---

### PR 9: CLI trace-mode flags and split-banks option

**Summary:** Add `-trace-mode`, `-trace-max-instr`, `-trace-max-visits-per-state`,
`-trace-max-branch-states`, `-trace-joypad1`, `-trace-joypad2`,
`-trace-joypad1-seq`, `-trace-joypad2-seq`, and `-split-banks` flags.

**Dependencies:** None (flag plumbing only, actual consumers come later)

**Files:**
- `internal/options/options.go` (~35 lines)
- `internal/cli/cli.go` (~19 lines -- validation, normalization, copy to disasm opts)
- `internal/cli/cli_test.go` (~94 lines -- refactored test structure, new trace flags
  test case, invalid trace-mode test)

**Size:** ~148 lines

**What changed:** `TraceMode` (static/emu/hybrid), budget limits, joypad bitmasks
and sequences, and `SplitBanks` bool added to `Flags` and `Disassembler` option
structs. CLI validates `TraceMode` against `validTraceModes` list. Defaults to
`"static"` preserving existing behavior.

---

## Tier 3 - Multi-Bank Features

### PR 10: Multi-bank PRG processing engine

**Summary:** Process non-last PRG banks by temporarily remapping the CPU window,
seeding entry points from bank vectors, cross-bank call targets, and pointer tables,
then running `followExecutionFlow` per bank.

**Dependencies:** PR 6 (ParseKey), PR 7 (Mapper API), PR 8 (stats)

**Files:**
- `internal/disasm/banks.go` (new, 1195 lines)
- `internal/arch/m6502/vectors.go` (new, 67 lines -- `InitializeBankVectors`)
- `internal/arch/m6502/vectors_bank_test.go` (new, 169 lines)
- `internal/arch/chip8/chip8.go` (~5 lines -- `InitializeBankVectors` no-op stub)
- `internal/disasm/disasm.go` (add `InitializeBankVectors` to `architecture` interface,
  add `processingAdditionalBanks` field, call `processAdditionalBanks` from `Process`)

**Size:** ~1,436 lines new, ~20 lines changed

**What changed:**
- `processAdditionalBanks` iterates non-last banks with up to 4 seeding passes
  (`seedLikelyEntryPoints`, `seedLikelyCrossBankCallTargets`,
  `seedLikelyPointerTables`, `seedLikelySplitPointerTables`)
- `validateCodeSequence` / `validateCodeSequenceStrict` reject data bytes (BRK, unofficial
  opcodes) masquerading as code
- `shouldRejectBranchTarget` extracted to keep cyclomatic complexity within lint limits
- `InitializeBankVectors` reads NMI/Reset/IRQ from each bank's trailing 6 bytes,
  validates with `isValidVectorAddress` + `isValidOpcodeAt`, and queues as parse targets

---

### PR 11: CDL multi-bank support

**Summary:** Extend Code/Data Log ingestion to handle multi-bank ROMs by computing
bank index from flat CDL offset and remapping before seeding.

**Dependencies:** PR 7 (Mapper API -- `MapBank`, `RestoreDefaultMapping`)

**Files:**
- `internal/mapper/cdl.go` (~48 lines changed)
- `internal/mapper/cdl_test.go` (~37 lines changed)

**Size:** ~85 lines

**What changed:**
- `ApplyCodeDataLog` dispatches to `applyCodeDataLogSingleBank` or
  `applyCodeDataLogMultiBank` based on `bankWindowSize`
- Multi-bank variant computes `bankIndex = index / 0x8000`, calls `MapBank(bankIndex)`,
  and seeds at `$8000 + bankOffset`
- Fixed off-by-one: `index > len` became `index >= len` in single-bank path
- Tests updated for bounds fix; new `TestApplyCodeDataLog_MultiBank` for Mapper 7

---

### PR 12: Data-as-code false positive detection and label resolution

**Summary:** Detect suspect call destinations (BRK/unofficial opcodes at target),
deduplicate labels across bank mappings, and handle cross-bank caller rewriting.

**Dependencies:** PR 6 (ParseKey), PR 7 (Mapper API)

**Files:**
- `internal/disasm/code.go` (~186 lines changed)
- `internal/disasm/code_test.go` (new, 23 lines)

**Size:** ~209 lines

**What changed:**
- `processJumpDestinations` refactored from `set.Set[uint16]` to `set.Set[ParseKey]`
  with companion `branchDestinationInfo map[ParseKey]*offset.DisasmOffset`
- New helpers: `sortedBranchDestinations`, `resolveDestinationLabel` (calls
  `isSuspectCallDestination` to downgrade false-positive `_func_` labels to `_label_`),
  `uniqueLabelName` (appends `_m<mappingID>` for cross-mapping collisions),
  `applyDestinationLabel` (skips cross-bank callers unless rewritable),
  `canRewriteCallersToBranchLabel`, `isStableEmittedLabelAddress`,
  `defaultMappingSignature`
- Test for `uniqueLabelName` collision resolution

---

## Tier 4 - Output Features

### PR 13: 32KB bank splitting in processor

**Summary:** Split 32KB internal banks into two 16KB output `PRGBank` entries, and
resolve missing symbol aliases across bank boundaries.

**Dependencies:** PR 3 (`BaseAddress` field), PR 7 (Mapper API), PR 10 (multi-bank)

**Files:**
- `internal/mapper/processor.go` (new, 261 lines)
- `internal/mapper/processor_test.go` (~227 lines changed)

**Size:** ~488 lines

**What changed:**
- `SetProgramBanks` detects 32KB banks and calls `setProgramBanksSplit`
- `create16KOutputBank` splits offsets at the 0x4000 boundary, sets `BaseAddress`
  to `$8000` / `$C000`, calls `setBankName` and `setBankVectorsFromSlice`
- `addMissingSymbolAliases` post-pass: scans offsets for unresolved references,
  rewrites unresolved relative branches as `.byte` literals
  (`rewriteRelativeBranchAsBytes`), injects missing symbols as constants into
  `PRG[0]` and `app.Constants`
- Tests: split expectations, small-bank no-split, alias injection, relative branch
  rewrite, mapping-suffix symbol resolution

---

### PR 14: Split-banks output pipeline and assembler writers

**Summary:** Write each PRG bank to a separate `.asm` file with `.include` directives
in the main file. All four assembler backends updated.

**Dependencies:** PR 13 (bank splitting), PR 9 (`SplitBanks` flag)

**Files:**
- `internal/pipeline/pipeline.go` (~27 lines changed)
- `internal/assembler/asm6/file.go` (~131 lines changed)
- `internal/assembler/ca65/file.go` (~136 lines changed)
- `internal/assembler/ca65/config.go` (~6 lines -- use `bank.BaseAddress`)
- `internal/assembler/nesasm/file.go` (~91 lines changed)
- `internal/assembler/retroasm/nes.go` (~60 lines changed)
- `internal/writer/writer.go` (~31 lines changed)
- `internal/writer/writer_test.go` (new, 41 lines)
- `internal/disasm/disasm_test.go` (~14 lines -- `nopWriteCloser` for multi-bank tests)

**Size:** ~510 lines changed, ~41 lines new tests

**What changed:**
- `pipeline.go`: `runDisassembly` accepts `opts` + `outputPath`; `newBankWriter`
  creates per-bank files (`output_bank_0.asm`) when `SplitBanks` is true;
  `generateBankFilename` converts bank names to file names
- All assemblers: `writeBank`/`writePRGBank` uses `bankWriteCloser` from
  `newBankWriter`; emits `.include "..."` to main writer; vectors written per-bank
  using `bank.BaseAddress`; `.base`/`.org` uses `bank.BaseAddress`; added
  `assembleComment` helpers
- `ca65/config.go`: memory/segment templates use `bank.BaseAddress` instead of
  global `CodeBaseAddress`
- `writer.go`: `emittedAliases` map prevents duplicate constant definitions across
  bank files sharing a `Writer`
- `disasm_test.go`: `nopWriteCloser` replaces nil writer in test harness

---

## Tier 5 - Emulation Trace

### PR 15: Mapper runtime state (snapshot/restore + ApplyMapperWrite)

**Summary:** Implement mapper register write simulation and full runtime state
serialization for emulator backtracking.

**Dependencies:** PR 7 (Mapper API)

**Files:**
- `internal/mapper/runtime.go` (new, 379 lines)

**Size:** ~379 lines

**What changed:**
- `mmc1Runtime` (shift register, control, PRG/CHR bank registers) and
  `mmc5Runtime` (PRG mode + 5 register bytes) state structs
- `runtimeSnapshot` flat struct for serialization
- `SnapshotRuntimeState()` / `RestoreRuntimeState(snapshot)` -- capture and restore
  complete mapper state including calling `RestoreMappingSignature`
- `ApplyMapperWrite(address, value)` -- dispatches writes to mapper register ranges:
  UxROM ($8000-$FFFF single write), AxROM (same), MMC1 (serial shift register at
  $8000-$FFFF), MMC5 (mode 3 PRG registers). Returns true if mapping changed.

---

### PR 16: M6502 advisory emulator core

**Summary:** 6502 CPU emulator with NES bus simulation, branch-state exploration,
joypad replay, and synthetic NMI generation.

**Dependencies:** PR 15 (mapper runtime), PR 7 (Mapper API)

**Files:**
- `internal/trace/m6502emu/bus.go` (new, 258 lines)
- `internal/trace/m6502emu/trace.go` (new, 1,034 lines)
- `internal/trace/m6502emu/trace_test.go` (new, 815 lines)

**Size:** ~2,107 lines

**What changed:**
- `bus.go`: NES memory bus -- RAM ($0000-$07FF, mirrored 4x), PPU register stubs
  ($2002 toggles vblank flag), joypad shift register ($4016/$4017 strobe + sequential
  reads from configurable bitmask or frame sequence), PRG RAM ($6000-$7FFF),
  cartridge ROM via `mapper.ReadMemory`. Full `snapshot`/`restore` for backtracking.
- `trace.go`: Advisory emulator engine. Runs up to `MaxInstructions` with per-PC
  visit limits (`visitLimit=2048`, `ppuLoopVisitLimit=8192`). Records `TraceStep`
  entries (PC, opcode, operands, MappingSignature, bankID, physicalOffset). Branch-state
  exploration: at each conditional branch, saves CPU+bus+mapper snapshots for the
  not-taken path when `MaxBranchStates > 0`. Synthetic NMI injection at configurable
  intervals. Builds `Result` with all steps, branch alternates, bank-switch writes,
  and hotspot summaries.
- `trace_test.go`: RAM mirroring, PPU status, PPU NMI enable, joypad strobe/read,
  snapshot/restore, branch alternates, mapper write recording, visit limits,
  hotspot aggregation, main `Run` with minimal NOP program.

---

### PR 17: Advisory emulator integration and hybrid dispatch

**Summary:** Wire the emulator into the disassembly pipeline -- run the emulator,
seed discovered code addresses into the static tracer, and support hybrid mode.

**Dependencies:** PR 16 (emulator), PR 6 (ParseKey), PR 8 (stats), PR 9 (CLI flags)

**Files:**
- `internal/disasm/emutrace.go` (new, 458 lines)
- `internal/disasm/emutrace_test.go` (new, 54 lines)
- `internal/disasm/disasm.go` (~45 lines changed -- hybrid dispatch in `Process`)

**Size:** ~557 lines

**What changed:**
- `emutrace.go`: `runAdvisoryEmuTrace` builds config from options + env var overrides
  (`RETRODISASM_EMU_TRACE_*`), runs `m6502emu.Run`, returns `Result`.
  `seedFromAdvisoryEmuTrace` ingests steps by restoring mapping signatures and calling
  `AddAddressToParse`. `logEmuTraceResult` logs step count and hotspots.
  `annotateBankSwitchWrites` marks mapper-write offsets.
  `isHybridTraceMode` checks trace mode + multi-bank presence.
  `tunedBranchStateBudgetForMapper` caps MMC1 branch budget to prevent combinatorial
  explosion from serial shift register.
- `disasm.go` `Process` changes: resets stats, starts timer, runs advisory emu trace,
  runs `followExecutionFlow` first in hybrid mode, seeds from emu result,
  runs `followExecutionFlow` again, calls `processAdditionalBanks`,
  logs comparison, annotates bank-switch writes.
- Tests: `parseJoypadSequence`, `parseJoypadSequenceSetting`, budget tuning for Mapper 1.

---

## Scripts and Makefile

Scripts are developer research/triage tools. Ship them with the PR whose feature
they exercise, or bundle as a final tooling PR.

| Script | Size | Purpose | Ships with |
|--------|------|---------|------------|
| `scripts/verify_rom_city_rampage.sh` | 44 lines | Smoke-test hybrid trace on a real multi-bank ROM | PR 14 or PR 17 |
| `Makefile` (`test-rom-city-rampage` target) | 3 lines | Make target for the above | Same as above |
| `scripts/benchmark_mapper_corpus.sh` | 354 lines | Baseline pass/fail survey across ROM corpus by mapper | PR 17 or standalone tooling PR |
| `scripts/benchmark_trace_sweep.sh` | 483 lines | Cartesian sweep of trace budget parameters | PR 17 or standalone tooling PR |
| `scripts/cluster_failure_artifacts.sh` | 571 lines | Post-process failure artifacts from benchmark scripts | Same as `benchmark_mapper_corpus.sh` |
| `scripts/sweep_mapper_joypad_timeline.sh` | 482 lines | Sweep joypad input presets across mapper-filtered ROMs | PR 17 or standalone tooling PR |
| `scripts/sweep_rom_city_rampage_joypad_timeline.sh` | 220 lines | Per-ROM joypad sweep for Rom City Rampage | Same as above |

---

## Documentation

| File | Size | Ships with |
|------|------|------------|
| `docs/nes-emulation-trace-plan.md` | 3,609 lines | PR 16 or PR 17 (design doc for the emulation feature) |

---

## Dependency Graph

```
Tier 1 (independent):
  PR 1  Jump engine nil guard
  PR 2  Exit code handling
  PR 3  LastNonZeroByte fix + BaseAddress field
  PR 4  Indexed load data protection
  PR 5  fmt.Fprintf cleanup

Tier 2 (foundations):
  PR 6  ParseKey deduplication
  PR 7  Mapper API extensions
  PR 8  Trace statistics
  PR 9  CLI trace-mode flags

Tier 3 (multi-bank):
  PR 10 Multi-bank PRG processing -----> depends on PR 6, PR 7, PR 8
  PR 11 CDL multi-bank support --------> depends on PR 7
  PR 12 Data-as-code detection --------> depends on PR 6, PR 7

Tier 4 (output):
  PR 13 32KB bank splitting -----------> depends on PR 3, PR 7, PR 10
  PR 14 Split-banks output pipeline ---> depends on PR 9, PR 13

Tier 5 (emulation):
  PR 15 Mapper runtime state ----------> depends on PR 7
  PR 16 M6502 emulator core -----------> depends on PR 15, PR 7
  PR 17 Emulator integration ----------> depends on PR 16, PR 6, PR 8, PR 9
```

---

## File-to-PR Assignment (complete)

Every changed file is assigned to exactly one PR.

| File | PR |
|------|-----|
| `internal/jumpengine/jumpengine.go` | 1 |
| `main.go` | 2 |
| `internal/program/prg.go` | 3 |
| `internal/arch/m6502/parser.go` | 4 |
| `internal/assembler/retroasm/chip8.go` | 5 |
| `internal/disasm/parsekey.go` | 6 |
| `internal/disasm/parsekey_test.go` | 6 |
| `internal/disasm/parser.go` | 6 |
| `internal/disasm/data.go` | 6 |
| `internal/mapper/mapper.go` | 7 |
| `internal/mapper/mapper_test.go` | 7 |
| `internal/mapper/bank.go` | 7 |
| `internal/disasm/stats.go` | 8 |
| `internal/options/options.go` | 9 |
| `internal/cli/cli.go` | 9 |
| `internal/cli/cli_test.go` | 9 |
| `internal/disasm/banks.go` | 10 |
| `internal/arch/m6502/vectors.go` | 10 |
| `internal/arch/m6502/vectors_bank_test.go` | 10 |
| `internal/arch/chip8/chip8.go` | 10 |
| `internal/mapper/cdl.go` | 11 |
| `internal/mapper/cdl_test.go` | 11 |
| `internal/disasm/code.go` | 12 |
| `internal/disasm/code_test.go` | 12 |
| `internal/mapper/processor.go` | 13 |
| `internal/mapper/processor_test.go` | 13 |
| `internal/pipeline/pipeline.go` | 14 |
| `internal/assembler/asm6/file.go` | 14 |
| `internal/assembler/ca65/file.go` | 14 |
| `internal/assembler/ca65/config.go` | 14 |
| `internal/assembler/nesasm/file.go` | 14 |
| `internal/assembler/retroasm/nes.go` | 14 |
| `internal/writer/writer.go` | 14 |
| `internal/writer/writer_test.go` | 14 |
| `internal/disasm/disasm_test.go` | 14 |
| `internal/mapper/runtime.go` | 15 |
| `internal/trace/m6502emu/bus.go` | 16 |
| `internal/trace/m6502emu/trace.go` | 16 |
| `internal/trace/m6502emu/trace_test.go` | 16 |
| `internal/disasm/emutrace.go` | 17 |
| `internal/disasm/emutrace_test.go` | 17 |
| `internal/disasm/disasm.go` | 6 + 10 + 17 (*) |
| `docs/nes-emulation-trace-plan.md` | 16 or 17 |
| `Makefile` | 17 |
| `scripts/verify_rom_city_rampage.sh` | 17 |
| `scripts/benchmark_mapper_corpus.sh` | 17 |
| `scripts/benchmark_trace_sweep.sh` | 17 |
| `scripts/cluster_failure_artifacts.sh` | 17 |
| `scripts/sweep_mapper_joypad_timeline.sh` | 17 |
| `scripts/sweep_rom_city_rampage_joypad_timeline.sh` | 17 |

(*) `disasm.go` has changes spanning multiple PRs:
- PR 6: field type changes (`branchDestinations`, parse queues become `ParseKey`-keyed)
- PR 10: `architecture` interface addition (`InitializeBankVectors`),
  `processingAdditionalBanks` field, call to `processAdditionalBanks`
- PR 17: `stats` field, hybrid dispatch in `Process`, emu trace calls

When extracting, cherry-pick or split the `disasm.go` hunks according to which PR
they belong to.

---

## Suggested Merge Order

1. PRs 1-5 (any order) -- quick wins, immediate value
2. PRs 6-9 (any order among themselves) -- foundations
3. PR 10 (multi-bank engine) -- biggest single PR, review carefully
4. PRs 11, 12 (either order) -- smaller multi-bank features
5. PR 13 (bank splitting) -- output infrastructure
6. PR 14 (split-banks writers) -- completes output pipeline
7. PR 15 (mapper runtime) -- emulation foundation
8. PR 16 (emulator core) -- largest code addition, standalone testable
9. PR 17 (integration + scripts) -- ties everything together
