# NES Emulator-Assisted Trace Plan

Date: 2026-02-23

## Goal

Improve NES disassembly accuracy by simulating CPU execution and mapper state transitions so code discovery works across bank switches, not only within the default static mapping.

## Prerequisites

- Go 1.22+ (required for range-over-int and `min` builtin)
- retrogolib CPU API surface: `m6502.New()`, `m6502.BasicMemory`, `Step()`, `TraceStep`, `WithTracing()`, `WithPreExecutionHook()`
- iNES 2.0 mapper support (commit `1b3d48d`) — emulator should use the mapper number from the cartridge header, supporting extended mapper numbers

## Why This Is Needed

Current tracing is primarily address-based and assumes one active mapping per CPU window during disassembly. That causes blind spots for mapper-driven code paths.

## Current-State Review (Code Findings)

1. Parse queue deduplicates by CPU address only.
   - `internal/disasm/disasm.go:81` shows a TODO for bank switch handling.
   - `internal/disasm/disasm.go:82` and `internal/disasm/disasm.go:84` keep parse state as `set.Set[uint16]`.
   - `internal/disasm/parser.go:43` and `internal/disasm/parser.go:108` dedupe/skip by plain `uint16` address.
   - Impact: same CPU address executed under different bank mappings is parsed only once.

2. Mapper mapping is static during disassembly.
   - `internal/mapper/mapper.go:95` default mapping is configured once.
   - `internal/mapper/mapper.go:110` has internal `setMappedBank`, but no runtime write path for mapper registers.
   - Impact: traced reads after runtime bank switch may resolve wrong physical PRG bytes.

3. CDL application is not multi-bank aware.
   - `internal/mapper/cdl.go:12` binds to `bank0` and `internal/mapper/cdl.go:14` exits when index exceeds that bank.
   - Impact: multi-bank coverage from code/data logs is truncated.

4. Labels are address-only, not bank-qualified.
   - `internal/disasm/code.go:11` uses templates like `_func_%04x`.
   - Impact: potential symbol collisions if multiple physical banks expose same CPU address labels.

5. User-facing warnings already acknowledge mapper limitations.
   - `internal/pipeline/pipeline.go:210` warns non-0/non-3 mapper support is experimental.

6. Good foundation exists for CPU stepping/tracing.
   - `retrogolib` 6502 CPU supports instruction stepping with trace info (`.../arch/cpu/m6502/step.go:17`).
   - Hook support exists (`.../arch/cpu/m6502/option.go:30`).
   - Custom bus is supported via `BasicMemory` (`.../arch/cpu/m6502/memory.go:21`).

7. Multi-bank vector tracing design exists but is NOT implemented.
   - `.claude/retrodisasm-project.md` lines 67-120 documents a complete design for per-bank vector tracing using `MapBank()`, `RestoreDefaultMapping()`, `BankCount()`, `BankVectors()`, `InitializeBankVectors()`.
   - These mapper methods and architecture interfaces are designed but not yet in code.

8. Mapper internal structure uses fixed 8KB windowing.
   - `internal/mapper/mapper.go:16` — `bankWindowSize` is `0x2000` (8KB) for NES multi-bank.
   - `internal/mapper/mapper.go:19` — `banksMapped` holds all possible 8KB bank mappings.
   - `internal/mapper/mapper.go:20` — `mapped` holds the currently active 8KB slot assignments (8 slots for 64KB address space).
   - Runtime mapper writes must update the correct number of 8KB slots per mapper type.

## Mapper Emulation Scope

### PRG Bank Switching Classification

Not all mappers switch PRG banks. Only mappers that remap PRG code at runtime need emulator support:

**Needs PRG runtime emulation:**
- **Mapper 1 (MMC1)**: 16KB or 32KB switchable via serial register
- **Mapper 2 (UxROM)**: 16KB switchable bank at $8000-$BFFF, $C000-$FFFF fixed to last bank
- **Mapper 5 (MMC5)**: Configurable PRG mode ($5100), 4 individual 8KB PRG bank select registers ($5114-$5117)
- **Mapper 7 (AxROM)**: 32KB switchable bank at $8000-$FFFF

**No PRG switching (mapper 0 equivalent for disassembly):**
- **Mapper 0 (NROM)**: Fixed PRG, no bank switching
- **Mapper 3 (CNROM)**: Switches CHR banks only; PRG is fixed — functionally identical to mapper 0 for disassembly

### Mapper Register Specifications

- **Mapper 0**: No writes (fixed)
- **Mapper 2**: $8000-$FFFF write → low bits select 16KB bank at $8000-$BFFF; $C000-$FFFF fixed to last bank
- **Mapper 3**: $8000-$FFFF write → CHR bank select only (ignore for PRG disassembly)
- **Mapper 7**: $8000-$FFFF write → bits 0-2 select 32KB PRG bank; bit 4 = VRAM mirror (irrelevant for disassembly)
- **Mapper 1 (MMC1)**: Serial 5-bit shift register at $8000-$FFFF; bit 7 resets; address bits 14-13 select target register (control, CHR0, CHR1, PRG)
- **Mapper 5 (MMC5)**: $5100 write → PRG mode (0-3); $5114-$5117 write → per-window 8KB PRG bank select (bit 7 = ROM flag)

### Internal 8KB Windowing

The `Mapper` struct uses a fixed `bankWindowSize` of `0x2000` (8KB) with 8 slots covering the full 64KB address space. Runtime mapper writes must update the correct number of 8KB slots:

- **Mapper 1** (variable): depends on PRG mode register (16KB or 32KB switching)
- **Mapper 2** (16KB at $8000-$BFFF): update 2 slots ($8000, $A000); $C000/$E000 fixed to last bank
- **Mapper 5** (variable): depends on PRG mode ($5100); mode 3 = 4 individual 8KB slots, mode 0 = single 32KB slot
- **Mapper 7** (32KB switch): update all 4 PRG slots ($8000, $A000, $C000, $E000)

### Working Corpus

- Working corpus: mapper `0` (41 ROMs), `3` (6 ROMs), `7` (1 ROM)
- Not-working corpus: mapper `1` (2 ROMs), `2` (7 ROMs)
- Special corpus: mapper `5` (Rom City Rampage — MMC5, verified with both `ca65` and `asm6`)

## Proposed Architecture

Use a hybrid model:

1. Emulator pass discovers reachable instruction stream with mapper transitions.
2. Disassembler pass consumes emulator trace states (not only plain addresses).
3. Existing static heuristics (jump engine, branch post-processing, variable/constant analysis) remain, but become bank-context aware.

### New Core Components

1. `internal/trace/m6502emu` (new package)
   - Runs `retrogolib` CPU with a custom NES memory bus.
   - Records per-instruction events:
     - `PC`
     - opcode bytes
     - current mapper snapshot ID
     - physical PRG location (bank ID + offset)
     - control-flow edge (`from`, `to`, branch/call/jump/return/fallthrough)
     - bank switch events (old/new mapping)

2. `MapperRuntime` extension in `internal/mapper`
   - Builds on existing `banksMapped` (all possible mappings) and `setMappedBank()` (private slot assignment).
   - "MapperRuntime" exposes public methods that reassign `mapped` slots from `banksMapped` entries.
   - A "snapshot" is a copy of the `mapped` slice (8 entries). No new data structure needed.
   - Supports mapping signature/hash for dedupe and parse-keying.
   - Provides physical PRG resolution for each CPU read/write.

3. `TraceDB` (new data structure)
   - Stores discovered states and edges.
   - Keyed by `(pc, mapping_signature)` for dedupe.
   - Exposes frontier for branch exploration and later disasm integration.

4. Bank-aware parse key in `internal/disasm`
   - Replace `uint16` parse keys with:
     - `type ParseKey struct { PC uint16; MappingID uint32 }`
   - Parse queue and parsed-sets use `ParseKey`.
   - Prevents incorrect collapsing of same CPU address across distinct bank mappings.

### NES Memory Map for Emulator Bus

The `BasicMemory` implementation needs this concrete address map:

| Range | Implementation |
|-------|---------------|
| $0000-$07FF | 2KB RAM array |
| $0800-$1FFF | Mirror → `address & 0x07FF` |
| $2000-$3FFF | PPU stubs (mirror via `0x2000 + address & 0x07`) |
| $4000-$401F | APU/IO stubs |
| $4020-$5FFF | Return 0 (open bus) |
| $6000-$7FFF | Optional 8KB PRG-RAM array |
| $8000-$FFFF | Delegate to mapper (bank-switched) |

### I/O Stub Strategy

- `$2002` (PPUSTATUS): deterministic alternating read (`0x00`, `0x80`, ...) to avoid hard-wiring one phase
- `$4016/$4017` (controllers): serial strobe/latch/shift emulation (neutral input state by default)
- All other PPU/APU: Return 0
- Rationale: explores "normal startup" path for most games

### retrogolib API Usage Example

```go
bus := &TraceBus{...}  // implements m6502.BasicMemory
mem, _ := m6502.NewMemory(bus)
cpu := m6502.New(mem, m6502.WithTracing(), m6502.WithPreExecutionHook(onPreExec))
for i := 0; i < maxInstructions; i++ {
    if err := cpu.Step(); err != nil { break }
    // record cpu.TraceStep
}
```

### Integration API (Phase 3)

How emulator trace feeds into disassembly:

1. Emulator produces `[]TraceResult` with `(PC, mapped slot assignments)` per instruction.
2. Before `followExecutionFlow()`, trace results are queued via `AddAddressToParse` with correct mapping state.
3. Disassembler restores mapping before reading each `ParseKey`-keyed address.
4. Integration point: `Process()` method in `disasm.go` (line ~130).

## Execution Strategy

### Deterministic Baseline (first)

1. Start CPU at Reset vector.
2. Step instructions with budget limits:
   - max instructions
   - max branch frontier size
   - max visits per `(pc, mapping_signature)`
3. Follow deterministic control flow.
4. Record dynamic bank-switch writes and resulting mappings.

### Halt and Termination Conditions

- **JMP-to-self**: retrogolib already handles (step.go:97-98); detect via PC unchanged after `Step()`
- **Invalid opcode**: `Step()` returns `ErrUnknownOpcode` — log and terminate that path
- **Unmapped reads**: return 0 from bus, log warning
- **Per-PC visit counter** (e.g., limit 8) to catch I/O-dependent infinite loops

### Controlled Path Expansion (second)

For conditional branches, allow bounded exploration:

1. Continue with actual CPU flags (primary path).
2. Optionally enqueue alternate path with cloned CPU+mapper state.
3. Budget gate prevents explosion.

This gives meaningful extra coverage without full symbolic execution.

### State Cloning Cost (for Phase 5)

- CPU state: ~7 bytes registers + 8 mapper slot entries (~256 bytes total)
- RAM: 2KB per cloned state
- 100 queued states ≈ 200KB — acceptable
- Bank PRG data is shared read-only, never cloned
- Simple value copy, no copy-on-write needed

## CLI and Config Plan

Add opt-in flags first, then evaluate defaulting:

1. `-trace-mode static|emu|hybrid` (default `static` initially)
2. `-trace-max-instr N`
3. `-trace-max-branch-states N`
4. `-trace-max-visits-per-state N`

This keeps rollout safe and benchmarkable.

Status update (2026-02-23): implemented in **Phase 6** with env fallback compatibility.

## Phased Implementation Plan

### Phase 0: Instrumentation and Baseline

Status: Completed (2026-02-23)

1. Add trace stats struct and debug output.
2. Add mapper corpus benchmark script (coverage/accuracy metrics).
3. Document baseline pass/fail by mapper and ROM set.

Acceptance:

1. Existing tests pass.
2. Baseline metrics are reproducible.

Implementation notes:

- Trace stats added to disassembly flow:
  - `internal/disasm/stats.go`
  - `internal/disasm/disasm.go`
  - `internal/disasm/parser.go`
  - `internal/disasm/code.go`
  - `internal/disasm/data.go`
- New reproducible baseline script:
  - `scripts/benchmark_mapper_corpus.sh`

Repro command used:

```bash
scripts/benchmark_mapper_corpus.sh -g all -a ca65 -o /tmp/mapper_baseline_ca65_all.csv
```

Baseline summary captured (ca65, group=all):

| Set | Mapper | Pass | Fail | Total |
|-----|--------|------|------|-------|
| notworking | 1 | 2 | 0 | 2 |
| notworking | 2 | 7 | 0 | 7 |
| working | 0 | 41 | 0 | 41 |
| working | 3 | 6 | 0 | 6 |
| working | 7 | 1 | 0 | 1 |

Notes:

- The `working` / `notworking` directory names are legacy corpus labels. Current baseline shows all ROMs in both sets passing verification with `ca65`.
- Script sets `GOCACHE` to a temp directory for sandbox compatibility.
- Trace stats are emitted at debug level at end of each `Process()` run under the log message `Trace stats`.

### Phase 0.5: Multi-Bank Vector Tracing

Status: Completed (2026-02-23)

Implement the multi-bank vector tracing design already documented in `.claude/retrodisasm-project.md` (lines 67-120). This is simpler than CPU emulation (no custom memory bus) and provides immediate multi-bank coverage.

1. Implement `MapBank(bankIndex int)` and `RestoreDefaultMapping()` on `Mapper`.
2. Implement `BankCount() int` and `BankVectors(bankIndex int) [3]uint16` on `Mapper`.
3. Add `InitializeBankVectors(bankIndex int)` to the architecture interface.
4. Add `processAdditionalBanks()` to disassembler flow: iterate non-last banks, map each, trace unique vectors, restore.
5. Validate vector addresses and opcodes before tracing (see vector validation in design doc).

Acceptance:

1. Multi-bank ROMs (mapper 7) trace vectors from non-last banks.
2. `setMappedBank()` works correctly for runtime remapping (validates foundation for emulator phases).
3. Existing tests pass, `-verify` passes for working ROMs.

Implementation notes:

- Mapper remapping/vector APIs added:
  - `internal/mapper/mapper.go`
  - `internal/mapper/mapper_test.go`
  - New methods: `BankCount()`, `BankVectors()`, `MapBank()`, `RestoreDefaultMapping()`
- Architecture interface extended with per-bank vector initialization:
  - `internal/disasm/disasm.go` (architecture interface)
  - `internal/arch/m6502/vectors.go` (`InitializeBankVectors`)
  - `internal/arch/chip8/chip8.go` (no-op `InitializeBankVectors`)
- Disassembler flow now traces additional mapped banks before post-processing:
  - `internal/disasm/banks.go`
  - `internal/disasm/disasm.go`

Validation:

- Unit/integration tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase05 go test ./internal/mapper ./internal/arch/m6502 ./internal/disasm ./internal/pipeline`
- Mapper 7 verification sample:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase05 go run . -verify -q -a ca65 -s nes -o /tmp/bt_phase05.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success (`EXIT:0`)

Notes:

- Current implementation maps each non-last 32KB PRG bank to `$8000-$FFFF`, queues only vectors that differ from the last bank, validates vector address and opcode, then traces that context.
- This phase improves multi-bank coverage while preserving the existing address-only parse key model. Full `(PC, MappingID)` dedupe remains in Phase 2.

### Phase 1: Emulator Trace Prototype (Advisory Only)

Status: Completed (2026-02-23)

1. Implement `internal/trace/m6502emu` with custom NES memory bus.
2. Implement NES address map (RAM, mirrors, PPU/APU stubs, mapper delegation).
3. Record executed `(pc, mapping_signature)` and bank-switch events.
4. Do not alter disassembly output yet; log comparison only.

Acceptance:

1. Emulator trace runs on mapper 0/7 fixtures (mapper 3 needs no PRG runtime handling).
2. No output regressions in current pipeline.

Implementation notes:

- New advisory emulator trace package:
  - `internal/trace/m6502emu/trace.go`
  - `internal/trace/m6502emu/bus.go`
  - `internal/trace/m6502emu/trace_test.go`
- Advisory integration in disassembly flow (no output mutation):
  - `internal/disasm/emutrace.go`
  - `internal/disasm/disasm.go`
- Mapper introspection helpers for trace metadata:
  - `internal/mapper/mapper.go`
  - `internal/mapper/mapper_test.go`
  - New methods: `MappingSignature()`, `ResolveAddress()`

Advisory mode controls:

- `RETRODISASM_EMU_TRACE=1` enables emulator trace.
- `RETRODISASM_EMU_TRACE_MAX_INSTR=<N>` sets instruction budget (default `100000`).
- `RETRODISASM_EMU_TRACE_MAX_VISITS=<N>` sets per-PC visit limit (default `8`).

Validation:

- Tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase1 go test ./internal/trace/m6502emu ./internal/mapper ./internal/disasm ./internal/pipeline`
- Mapper 0 advisory trace logs:
  - `RETRODISASM_EMU_TRACE=1 GOCACHE=/tmp/retrodisasm_gocache_phase1 go run . -debug -q -o /tmp/phase1_nestest.asm internal/testroms/commercial/working/nestest.nes`
  - Logs include `Advisory emu trace` and `Advisory emu trace comparison`.
- Mapper 7 advisory trace logs:
  - `RETRODISASM_EMU_TRACE=1 RETRODISASM_EMU_TRACE_MAX_INSTR=200000 RETRODISASM_EMU_TRACE_MAX_VISITS=32 GOCACHE=/tmp/retrodisasm_gocache_phase1 go run . -debug -q -o /tmp/phase1_bt_debug.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Prototype captured mapper-register-space writes (`mapper_writes > 0`) while mapping changes remain `0` in this phase.
- Output regression guard:
  - `RETRODISASM_EMU_TRACE=1 GOCACHE=/tmp/retrodisasm_gocache_phase1 go run . -verify -q -a ca65 -s nes -o /tmp/phase1_bt.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success (`EXIT:0`).

Notes:

- Phase 1 intentionally does not change disassembly classification or queueing decisions.
- Trace output is advisory/debug data only; ParseKey and mapping-aware dedupe remain Phase 2.

### Phase 2: Bank-Aware Parse Keys

Status: Completed (2026-02-23)

1. Introduce `ParseKey` and update parse queues/dedupe.
2. Thread mapping ID through parse API and branch bookkeeping.
3. Add tests for same `PC` parsed in multiple mapping contexts.

Acceptance:

1. New tests cover duplicate-CPU-address multi-bank scenarios.
2. Existing tests remain green.

Implementation notes:

- Added parse-key model and mapping-aware queueing in disassembler core:
  - `internal/disasm/parsekey.go`
  - `internal/disasm/disasm.go`
  - `internal/disasm/parser.go`
  - `internal/disasm/data.go`
  - `internal/disasm/code.go`
  - `internal/disasm/emutrace.go`
- `ParseKey` is now `(PC, MappingID)` where `MappingID` is sourced from `Mapper.MappingSignature()`.
- Updated dedupe/parsed bookkeeping to be key-based:
  - `offsetsToParse`
  - `offsetsToParseAdded`
  - `offsetsParsed`
  - `functionReturnsToParse`
  - `functionReturnsToParseAdded`
- Branch destination tracking is now mapping-aware (`set.Set[ParseKey]`) with per-key destination offset bookkeeping.
- Function-return invalidation (`DeleteFunctionReturnToParse`) now removes all queued mapping variants for a given CPU address.
- Added targeted phase tests:
  - `internal/disasm/parsekey_test.go`
  - `TestAddAddressToParse_AllowsSameAddressAcrossMappings`
  - `TestFollowExecutionFlow_ParsesSamePCAcrossMappings`

Validation:

- Full test suite:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase2 go test ./...`
  - Result: success.
- Focused integration set:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase2 go test ./internal/disasm ./internal/arch/m6502 ./internal/jumpengine ./internal/mapper ./internal/pipeline`
  - Result: success.

Known limitation observed during phase validation:

- Mapper 7 `-verify` run shows duplicate symbol definitions in generated `ca65` output when additional bank contexts are parsed (e.g. repeated PPU/OAM aliases).
- Repro:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase2 go run . -verify -q -a ca65 -s nes -o /tmp/phase2_bt.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: fails with duplicate-symbol assembler errors.
- This aligns with Phase 4 scope (**Symbol and Output Stabilization**) and is tracked there.

### Phase 3: Mapper Runtime Integration

Status: Completed (2026-02-23)

1. Implement runtime writes for mapper 7 first, then mapper 2, then mapper 1.
   - Mapper 3 is excluded: it only switches CHR banks, not PRG.
2. Feed emulator mapping snapshots into disasm reads (`OffsetInfo`/`ReadMemory` by snapshot).
3. Fix CDL multi-bank handling while touching mapper internals.

Acceptance:

1. Mapper 7 Battletoads path coverage improves across switched banks.
2. Mapper 2/1 not-working sample set shows measurable progress.

Implementation notes:

- Mapper runtime emulation added for PRG bank writes:
  - `internal/mapper/runtime.go`
  - `internal/mapper/mapper.go`
  - New APIs:
    - `ApplyMapperWrite(address uint16, value byte) bool`
    - `RestoreMappingSignature(signature uint64) bool`
  - Implemented mapper-specific PRG behavior:
    - Mapper `7` (AxROM): 32KB switch at `$8000-$FFFF`
    - Mapper `2` (UxROM): switchable 16KB at `$8000-$BFFF`, fixed last 16KB at `$C000-$FFFF`
    - Mapper `1` (MMC1): serial 5-bit writes with PRG mode handling (`0/1/2/3`)
- Mapping snapshot store (`mappingSnapshots`) now captures/restores window assignments by signature.
- Disassembly parse now restores mapper state per `ParseKey` before processing each queued item:
  - `internal/disasm/parser.go`
- Emulator trace now applies mapper writes (instead of just logging write addresses):
  - `internal/trace/m6502emu/trace.go`
  - `internal/trace/m6502emu/trace_test.go`
- Emulator trace integration now seeds parse queue from emu-discovered `(PC, MappingSignature)` states:
  - `internal/disasm/emutrace.go`
  - `internal/disasm/disasm.go`
- CDL handling is now multi-bank aware:
  - `internal/mapper/cdl.go`
  - `internal/mapper/cdl_test.go`
  - Applies flags across all PRG banks and queues code addresses in the correct mapped bank context.
- Added/updated mapper runtime tests:
  - `internal/mapper/mapper_test.go`
  - Coverage for signature restore and mapper `7/2/1` write semantics.

Validation:

- Unit/integration:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase3 go test ./...`
  - Result: success.
- Mapper 7 runtime-write proof (Battletoads debug trace):
  - `RETRODISASM_EMU_TRACE=1 RETRODISASM_EMU_TRACE_MAX_INSTR=200000 RETRODISASM_EMU_TRACE_MAX_VISITS=32 GOCACHE=/tmp/retrodisasm_gocache_phase3 go run . -debug -q -o /tmp/phase3_bt_debug.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Log highlights:
    - `unique_mapping=2`
    - `mapper_writes=1`
    - `mapping_changes=1`

Known limitation after Phase 3:

- `-verify` on mapper-heavy ROMs currently fails due duplicate symbol/alias definitions across multi-context output.
- Repro:
  - `RETRODISASM_EMU_TRACE=1 GOCACHE=/tmp/retrodisasm_gocache_phase3 go run . -verify -q -a ca65 -s nes -o /tmp/phase3_bt.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: fails with duplicate-symbol assembler errors.
- This is tracked in **Phase 4: Symbol and Output Stabilization**.

### Phase 4: Symbol and Output Stabilization

Status: Completed (2026-02-23)

1. Add bank-qualified symbol fallback for collisions.
2. Ensure assembler outputs (`ca65`, `asm6`, `nesasm`, `retroasm`) remain valid.
3. Add regression tests for duplicate logical addresses across banks.

Acceptance:

1. `-verify` passes for unchanged working ROMs.
2. No symbol collision regressions on multi-bank outputs.

Implementation notes:

- Label collision fallback added in jump-destination naming:
  - `internal/disasm/code.go`
  - `uniqueLabelName(...)` appends mapping-qualified suffixes (`_m%04x`) on name collisions.
- Cross-bank alias re-emission dedupe added in shared writer:
  - `internal/writer/writer.go`
  - `OutputAliasMap(...)` now suppresses duplicate alias output across banks for identical `(name, address)`.
- Missing-symbol fallback alias injection added and hardened:
  - `internal/mapper/processor.go`
  - Adds aliases for unresolved `_func_`, `_label_`, `_jump_engine_` references.
  - Definition detection now considers only labels that are actually emitted by the writer traversal.
  - Handles mapping-qualified symbol forms (e.g. `_func_ff79_m8d46`) when extracting target addresses.
- Regression tests added:
  - `internal/writer/writer_test.go`
    - `TestOutputAliasMap_SkipsDuplicateReEmission`
    - `TestOutputAliasMap_EmitsWhenAddressDiffers`
  - `internal/disasm/code_test.go`
    - `TestUniqueLabelName_CollisionUsesMappingSuffix`
  - `internal/mapper/processor_test.go`
    - `TestSetProgramBanks_AddsMissingSymbolAlias`
    - `TestSetProgramBanks_AddsAliasWhenLabelIsInsideInstruction`
    - `TestSymbolAddress_WithMappingSuffix`

Validation:

- Unit/integration:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase4 go test ./...`
  - Result: success.
- Mapper 7 repro verification:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase4 go run . -verify -q -a ca65 -s nes -o /tmp/phase4_bt.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success (previous `_func_ff79` undefined-symbol failure resolved).
- Working corpus verification benchmark:
  - `scripts/benchmark_mapper_corpus.sh -g working -a ca65 -o /tmp/mapper_phase4_working.csv`
  - Summary:
    - `working mapper 0: 41/41 pass`
    - `working mapper 3: 6/6 pass`
    - `working mapper 7: 1/1 pass`

Post-phase note:

- Not-working mapper 1/2 samples still show PRG mismatch verification failures (content mismatch, not symbol-collision errors). This remains outside Phase 4 scope and is addressed by upcoming trace/path improvements.

### Phase 5: Controlled Branch Expansion

Status: Completed (2026-02-23)

1. Add optional bounded alternate-branch exploration.
2. Introduce heuristics for loop throttling and state pruning.
3. Measure incremental code discovery vs. runtime.

State cloning cost is low (~2KB RAM + ~256 bytes CPU/mapper per state; 100 queued states ≈ 200KB). Bank PRG data is shared read-only. Simple value copy suffices — no copy-on-write needed.

Acceptance:

1. Coverage gain on mapper-heavy ROMs with bounded runtime overhead.
2. Feature remains optional behind CLI controls.

Implementation notes:

- Advisory emulator trace now supports bounded branch-state exploration:
  - `internal/trace/m6502emu/trace.go`
  - `Config.MaxBranchStates` added.
  - New inferred state model: `BranchAlternate` with `(FromPC, Address, MappingSignature, BranchTarget, FallthroughTarget, Taken)`.
  - Conditional branches (`BCC/BCS/BEQ/BMI/BNE/BPL/BVC/BVS`) are detected from traced instructions.
  - Alternate destination is inferred from the observed next PC and relative branch operand.
- State pruning and throttling controls:
  - Existing per-PC loop throttle remains (`MaxVisitsPerPC`).
  - Alternate states dedupe by `(Address, MappingSignature)`.
  - Frontier budget enforced via `MaxBranchStates`; excess states are dropped and counted.
- New advisory trace diagnostics:
  - `ConditionalBranchCount`
  - `BranchAlternateCount`
  - `BranchAlternateBudgetDrops`
- Disasm integration:
  - `internal/disasm/emutrace.go`
  - New env control: `RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES`.
  - Inferred alternates are seeded as exploratory parse roots in the mapped context.
  - Important hardening: alternates are **not** registered as authoritative branch destinations, preventing branch-operand target rewrites in jump-destination post-processing.
- Regression tests added:
  - `internal/trace/m6502emu/trace_test.go`
    - `TestRunCollectsBranchAlternatesForNotTakenBranch`
    - `TestRunCollectsBranchAlternatesForTakenBranch`
    - `TestRunBranchAlternatesBudget`

Validation:

- Unit/integration:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase5 go test ./...`
  - Result: success.
- Mapper 7 verification with branch expansion enabled:
  - `RETRODISASM_EMU_TRACE=1 RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES=256 GOCACHE=/tmp/retrodisasm_gocache_phase5 go run . -verify -q -a ca65 -s nes -o /tmp/phase5_bt.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success.
- Mapper 7 debug comparison sample:
  - With `RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES=0`:
    - `conditional_branches=9`, `branch_alternates=0`
  - With `RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES=256`:
    - `conditional_branches=9`, `branch_alternates=3`, `branch_alternate_budget_drops=0`
- Not-working mapper 1 sample (Alfred) remains failing with large PRG mismatch, indicating this phase is stable but not sufficient alone for mapper 1/2 recovery.

### Phase 6: CLI Trace Controls and Config Hardening

Status: Completed (2026-02-23)

1. Promote env-based trace controls to explicit CLI flags.
2. Keep backward-compatible env fallback for existing automation.
3. Validate trace-mode parsing and budget propagation end-to-end.

Acceptance:

1. `-trace-*` flags control advisory emu-trace behavior without env variables.
2. Existing env-based workflows remain functional.
3. Working mapper verification remains green with CLI trace controls enabled.

Implementation notes:

- New user-facing flags in `options.Flags`:
  - `-trace-mode static|emu|hybrid`
  - `-trace-max-instr`
  - `-trace-max-visits-per-state`
  - `-trace-max-branch-states`
  - Files:
    - `internal/options/options.go`
    - `internal/cli/cli.go`
- Disassembler options extended to carry trace settings:
  - `TraceMode`
  - `TraceMaxInstructions`
  - `TraceMaxVisitsPerPC`
  - `TraceMaxBranchStates`
  - Files:
    - `internal/options/options.go`
    - `internal/cli/cli.go`
- Validation/normalization:
  - `trace-mode` is normalized to lowercase and validated against `static|emu|hybrid`.
  - Invalid values return a parse error.
  - File:
    - `internal/cli/cli.go`
- Runtime integration:
  - `runAdvisoryEmuTrace()` now consumes CLI values first, then env fallback.
  - `shouldRunAdvisoryEmuTrace()` enables trace for CLI modes `emu|hybrid`; env `RETRODISASM_EMU_TRACE` remains fallback for compatibility.
  - File:
    - `internal/disasm/emutrace.go`
- Test coverage:
  - `internal/cli/cli_test.go`
  - Added:
    - `trace flags` parse test
    - invalid `-trace-mode` parse failure test

Validation:

- Unit/integration:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase6 go test ./...`
  - Result: success.
- Mapper 7 verification using CLI flags only (no env toggles):
  - `GOCACHE=/tmp/retrodisasm_gocache_phase6 go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-branch-states 256 -trace-max-instr 200000 -trace-max-visits-per-state 32 -o /tmp/phase6_bt_hybrid.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success.
- Mapper 7 debug sample with CLI budgets:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase6 go run . -debug -q -trace-mode hybrid -trace-max-branch-states 256 -trace-max-instr 200000 -trace-max-visits-per-state 32 -o /tmp/phase6_bt_hybrid_debug.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Log highlights:
    - `instructions=482`
    - `conditional_branches=89`
    - `branch_alternates=6`
    - `branch_alternate_budget_drops=0`

### Phase 7: Benchmark Harness Hardening and Sweep Automation

Status: Completed (2026-02-23)

1. Add an explicit sweep harness for `-trace-*` budget tuning.
2. Harden benchmark pass/fail detection so verification failures are not counted as passes.
3. Re-baseline mapper 1/2 not-working set with corrected status detection.

Acceptance:

1. New sweep script can run mapper-filtered budget matrices and write CSV output.
2. Benchmark scripts classify failures correctly even when tool output logs an error with exit code `0`.
3. Re-baseline output produces defensible mapper 1/2 pass/fail numbers.

Implementation notes:

- New sweep automation script:
  - `scripts/benchmark_trace_sweep.sh`
  - Supports:
    - group filter (`-g all|working|notworking`)
    - mapper filter (`-m 1,2,...`)
    - trace mode (`-t static|emu|hybrid`)
    - budget CSVs (`-i`, `-v`, `-b`)
  - Emits per-run CSV columns:
    - `rom,set,mapper,trace_mode,max_instr,max_visits,max_branch,status,duration_ms`
  - Emits aggregated summary by `(mapper,trace_mode,max_instr,max_visits,max_branch)`.
- Existing baseline script hardened:
  - `scripts/benchmark_mapper_corpus.sh`
  - Added `verify_rom()` helper using both:
    - process exit code
    - output-text failure detection (`Disassembling failed|verification failed`)
- CLI exit code semantics fixed:
  - `main.go`
  - If one or more files fail processing, process now exits with status `1`.
  - This removes false-positive pass classification in automation that relies on return codes.

Validation:

- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase7 go test ./...`
  - Result: success.
- Exit-code behavior sanity check:
  - `go run . -verify ... "Alfred Chicken (USA).nes"` now returns `EXIT:1` on verification failure.
- Mapper 1 focused sweep:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1 -i 200000 -v 8 -b 0,256 -o /tmp/phase7_trace_sweep_m1.csv`
  - Summary:
    - branch `0`: `1/2` pass
    - branch `256`: `1/2` pass
- Mapper 2 focused sweep:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0,256 -o /tmp/phase7_trace_sweep_m2.csv`
  - Summary:
    - branch `0`: `2/7` pass
    - branch `256`: `2/7` pass
- Corrected not-working baseline (legacy script, now hardened):
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase7_mapper_corpus_notworking.csv`
  - Summary:
    - mapper `1`: `1/2` pass
    - mapper `2`: `2/7` pass

Post-phase note:

- Earlier benchmark sections that reported universal pass rates were influenced by exit-code-only classification and should be treated as superseded by Phase 7 corrected metrics.

### Phase 8: True Alternate-Path Execution (State Cloning)

Status: Completed (2026-02-23)

1. Replace inferred-only alternate branch handling with executable branch-state frontier.
2. Snapshot and restore CPU, RAM, and mapper runtime state for alternate-path replay.
3. Keep branch exploration bounded by existing budgets.

Acceptance:

1. Alternate branch states are actually executed, not only enqueued as inferred roots.
2. Runtime-state restore is deterministic across mapper writes (including MMC1 shift state).
3. Existing `-verify` working corpus behavior remains stable.

Implementation notes:

- Emulator trace frontier with true state replay:
  - `internal/trace/m6502emu/trace.go`
  - Added execution snapshots for:
    - CPU register/flag state (`A/X/Y/PC/SP/Flags`)
    - bus RAM/PRG-RAM state
    - mapper runtime snapshot
  - Conditional branches now enqueue executable alternate states, restored and stepped later.
  - Visit throttling is now keyed by `(PC, MappingSignature)` to avoid over-collapsing multi-mapping loops.
  - Added metric:
    - `BranchStatesExecuted`
- Bus snapshot/restore support:
  - `internal/trace/m6502emu/bus.go`
  - Added `snapshot()` and `restore()` for RAM/PRG-RAM.
- Mapper runtime snapshot/restore support:
  - `internal/mapper/runtime.go`
  - Added:
    - `SnapshotRuntimeState() any`
    - `RestoreRuntimeState(snapshot any) bool`
  - Snapshot includes mapping signature and MMC1 runtime registers (shift/control/PRG state).
- Mapper runtime snapshot tests:
  - `internal/mapper/mapper_test.go`
  - Added:
    - `TestSnapshotRestoreRuntimeState_UxROM`
    - `TestSnapshotRestoreRuntimeState_MMC1ShiftState`
- Trace execution test proving real alternate replay:
  - `internal/trace/m6502emu/trace_test.go`
  - Added:
    - `TestRunExecutesQueuedBranchState`
- Trace logging now reports executed branch states:
  - `internal/disasm/emutrace.go`
  - Added debug field:
    - `branch_states_executed`

Validation:

- Unit/integration:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase8 go test ./...`
  - Result: success.
- Working mapper verification (CLI trace budgets enabled):
  - `GOCACHE=/tmp/retrodisasm_gocache_phase8 go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 32 -trace-max-branch-states 256 -o /tmp/phase8_bt_hybrid.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Result: success.
- Battletoads debug sample (same budgets):
  - `GOCACHE=/tmp/retrodisasm_gocache_phase8 go run . -debug -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 32 -trace-max-branch-states 256 -o /tmp/phase8_bt_hybrid_debug.asm "internal/testroms/commercial/working/Battletoads (USA).nes"`
  - Log highlights:
    - `instructions=2108`
    - `unique_pc=85`
    - `unique_mapping=3`
    - `branch_alternates=186`
    - `branch_states_executed=186`
- Mapper 1/2 sweep after state-clone implementation:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8 -b 0,256 -o /tmp/phase8_trace_sweep_m12.csv`
  - Summary:
    - mapper `1`: `1/2` pass for branch `0` and `256`
    - mapper `2`: `2/7` pass for branch `0` and `256`

Post-phase note:

- State cloning is now functional and validated, but current mapper 1/2 pass rates did not improve with this initial branch-frontier policy. Remaining gains likely require mapper-/I/O-specific path realism and/or targeted frontier heuristics.

### Phase 9: Failure Artifact Capture and Triage Acceleration

Status: Completed (2026-02-23)

1. Add per-ROM failure artifact capture to sweep and baseline benchmark harnesses.
2. Persist first-pass triage artifacts (verification log, emitted labels, mismatch offsets).
3. Emit artifact paths directly in CSV outputs for fast failure drill-down.

Acceptance:

1. Both benchmark scripts can write deterministic per-failure artifact directories.
2. CSV outputs contain `artifact_path` for failed rows (empty for pass rows).
3. Artifact bundles include enough context to inspect mapper-specific failures without rerunning immediately.

Implementation notes:

- Sweep harness artifact support:
  - `scripts/benchmark_trace_sweep.sh`
  - Added `-d <artifact_dir>` option.
  - Added `artifact_path` CSV column.
  - For each failed ROM/config, writes:
    - `verify.log`
    - `disasm.asm` (if produced)
    - `labels.txt` (label definitions extracted from output asm)
    - `mismatch_offsets.txt` (filtered mismatch/failure lines)
    - `meta.txt` (ROM + trace budget metadata)
  - Artifact layout:
    - `<artifact_dir>/<rom_sanitized>/mode-<trace>_i<instr>_v<visits>_b<branch>/...`
- Baseline harness artifact support:
  - `scripts/benchmark_mapper_corpus.sh`
  - Added `-d <artifact_dir>` option.
  - Added `artifact_path` CSV column.
  - Writes the same artifact files per failed ROM.
  - Artifact layout:
    - `<artifact_dir>/<rom_sanitized>/baseline_<assembler>/...`

Validation:

- Syntax sanity:
  - `bash -n scripts/benchmark_trace_sweep.sh scripts/benchmark_mapper_corpus.sh`
  - Result: success.
- Sweep with artifact capture:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -d /tmp/phase9_artifacts -o /tmp/phase9_trace_sweep_m2.csv`
  - Summary:
    - mapper `2` hybrid (`i=200000`, `v=8`, `b=0`): `2 pass / 5 fail / 7 total`
  - CSV confirms `artifact_path` values on failures.
- Baseline with artifact capture:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -d /tmp/phase9_artifacts_base -o /tmp/phase9_mapper_corpus_notworking.csv`
  - Summary:
    - mapper `1`: `1 pass / 1 fail / 2 total`
    - mapper `2`: `2 pass / 5 fail / 7 total`
  - CSV confirms `artifact_path` values on failures.
- Artifact bundle spot-check:
  - `find /tmp/phase9_artifacts -maxdepth 3 -type f | head`
  - `find /tmp/phase9_artifacts_base -maxdepth 3 -type f | head`
  - Result: expected `verify.log`, `disasm.asm`, `labels.txt`, `mismatch_offsets.txt`, `meta.txt` present.

### Phase 10: Artifact Clustering and Mapper-Triage Summaries

Status: Completed (2026-02-24)

1. Add clustering tooling over captured failure artifacts.
2. Produce mapper-grouped recurring mismatch-offset and emitted-label summaries.
3. Emit triage-ready markdown + CSV outputs for rapid mapper debugging.

Acceptance:

1. Failure artifact roots from baseline/sweep scripts can be analyzed without rerunning disassembly.
2. Output includes:
   - per-artifact details
   - offset cluster counts by mapper
   - label cluster counts by mapper
   - markdown summary with Top-N clusters per mapper
3. Script runs cleanly (no parse/runtime warnings) and supports configurable Top-N.

Implementation notes:

- New clustering tool:
  - `scripts/cluster_failure_artifacts.sh`
  - Inputs:
    - artifact root (`-d`) produced by benchmark scripts using `-d`
  - Outputs:
    - markdown summary (`-o`, default `<artifact_root>/failure_cluster_summary.md`)
    - details CSV (`-c`, default `<artifact_root>/failure_cluster_details.csv`)
    - derived cluster CSVs:
      - `<details>_offset_clusters.csv`
      - `<details>_label_clusters.csv`
  - Key behavior:
    - infers mapper ID from ROM header (`meta.txt -> rom=...`)
    - parses mismatch offsets from `mismatch_offsets.txt`
    - parses emitted labels from `labels.txt`
    - aggregates `artifact_hits` by `(mapper, offset)` and `(mapper, label)`
    - emits Top-N clusters per mapper in markdown (`-n`, default `10`)

Validation:

- Syntax sanity:
  - `bash -n scripts/benchmark_trace_sweep.sh scripts/benchmark_mapper_corpus.sh scripts/cluster_failure_artifacts.sh`
  - Result: success.
- Baseline artifacts + clustering:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -d /tmp/phase10_artifacts -o /tmp/phase10_mapper_corpus_notworking.csv`
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase10_artifacts -o /tmp/phase10_failure_cluster_summary.md -c /tmp/phase10_failure_cluster_details.csv -n 8`
  - Summary highlights:
    - `artifacts analyzed = 6`
    - failures by mapper: `mapper 1 = 1`, `mapper 2 = 5`
- Sweep artifacts + clustering:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0,256 -d /tmp/phase10_artifacts_sweep -o /tmp/phase10_trace_sweep_m2.csv`
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase10_artifacts_sweep -o /tmp/phase10_failure_cluster_sweep_summary.md -c /tmp/phase10_failure_cluster_sweep_details.csv -n 10`
  - Summary highlights:
    - `artifacts analyzed = 10`
    - recurring mapper-2 offset clusters with `artifact_hits = 2`:
      - `0x1fff0`, `0x1fff1`, `0x1fffa`, `0x7f18`, `0x7ffa`, ...
    - recurring mapper-2 label clusters:
      - `NMI` (`8`)
      - `Reset` (`8`)
      - `IRQ`, `IRQ_Bank0`, `IRQ_Bank1`, `IRQ_Bank2`, `NMI_Bank0`, `Reset_Bank1` (each `6`)

Post-phase note:

- The cluster outputs now make repeated mismatch zones and repeated symbol regions explicit, enabling targeted mapper/I/O debugging without rerunning broad sweeps.
- Some failure artifacts contain zero `Offset mismatch` lines (`first_mismatch_offset=none`), which indicates non-offset verification failure modes are also present and should be triaged separately.

### Phase 11: Large-Matrix Sweep Re-Cluster and Stability Extraction

Status: Completed (2026-02-24)

1. Run a larger mapper `1/2` trace-budget matrix with per-failure artifact capture.
2. Extend artifact clustering to emit stability-filtered clusters.
3. Re-cluster matrix artifacts and identify high-hit mismatch zones that persist across configurations.

Acceptance:

1. Matrix sweep executes across multiple `max_visits` and `max_branch` combinations with artifact capture enabled.
2. Clustering output can filter by minimum `artifact_hits` to isolate stable offsets/labels.
3. Documentation includes stable hotspot findings and observed pass/fail behavior across the matrix.

Implementation notes:

- Stability extraction added to clustering tool:
  - `scripts/cluster_failure_artifacts.sh`
  - New option:
    - `-k <min_hits>` (minimum `artifact_hits` threshold; default `2`)
  - New generated outputs:
    - `<details>_offset_stable.csv`
    - `<details>_label_stable.csv`
  - Markdown summary now includes:
    - stability threshold banner
    - per-mapper stable mismatch and stable label tables
- Existing cluster behavior remains unchanged for raw aggregate outputs (`offset_clusters`, `label_clusters`).

Validation:

- Syntax sanity:
  - `bash -n scripts/cluster_failure_artifacts.sh scripts/benchmark_trace_sweep.sh scripts/benchmark_mapper_corpus.sh`
  - Result: success.
- Large matrix sweep with artifacts:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8,16,32 -b 0,64,128,256 -d /tmp/phase11_artifacts_sweep -o /tmp/phase11_trace_sweep_m12.csv`
  - Matrix size:
    - 12 configs (`3 visits * 4 branch`)
    - 9 ROMs
    - 108 runs total
  - Pass/fail summary was invariant across all configs:
    - mapper `1`: `1 pass / 1 fail`
    - mapper `2`: `2 pass / 5 fail`
- Re-cluster matrix artifacts with stability threshold:
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase11_artifacts_sweep -o /tmp/phase11_failure_cluster_summary.md -c /tmp/phase11_failure_cluster_details.csv -n 12 -k 10`
  - Result:
    - `artifacts analyzed = 72` (6 failed ROMs/config * 12 configs)
    - stable CSV outputs emitted (`offset_stable`, `label_stable`)
- Stable hotspot highlights (`artifact_hits >= 10`):
  - mapper `1` stable offsets:
    - `0x7ff8`, `0x7ff9`, `0x7ffe`, `0x7fff`, `0xfff8`, `0xfff9`, `0xfffe`, `0xffff` (all `12`)
  - mapper `2` stable offsets:
    - `0x1`, `0x1fff0`-`0x1fff5`, `0x1fffa`-`0x1fffc`, `0x7f18`-`0x7f1d`, `0x7ffa`-`0x7ffc` (all `12`)
  - mapper `2` stable labels:
    - `NMI` (`48`), `Reset` (`48`)
    - `IRQ`, `IRQ_Bank0`, `IRQ_Bank1`, `IRQ_Bank2`, `NMI_Bank0`, `Reset_Bank1` (each `36`)

Post-phase note:

- Larger branch/visit budgets did not change outcomes for current mapper `1/2` not-working ROMs, reinforcing that the blocker is mapper/I/O behavior fidelity rather than frontier-size tuning.
- The stable mismatch clusters strongly concentrate in vector-adjacent regions, which narrows the next debugging target.

### Phase 12: Multi-Bank Vector Placement Fix (ca65) and Re-Baseline

Status: Completed (2026-02-24)

1. Implement targeted fix for vector-region mismatch hotspots by forcing multi-bank vector emission to bank tail.
2. Re-run not-working and working corpus benchmarks to confirm impact and guard regressions.
3. Re-cluster remaining failures to verify prior vector-adjacent hotspot family is resolved.

Acceptance:

1. Multi-bank `ca65` output writes vectors at fixed last-6-byte bank positions instead of "append at current location".
2. Mapper `1/2` pass rates improve on not-working corpus.
3. Working corpus (`mapper 0/3/7`) remains unchanged.

Implementation notes:

- `ca65` multi-bank vector placement correction:
  - `internal/assembler/ca65/file.go`
  - `writeCode(...)` now returns emitted `endIndex`.
  - `writeBankVectors(...)` now receives `endIndex` and inserts explicit zero padding:
    - `padding = (len(bank.Offsets) - 6) - endIndex`
    - emits `.res <padding>, $00` before `.addr nmi, reset, irq`
  - This guarantees vectors are emitted at fixed bank tail addresses.
- Safety check:
  - if `endIndex` exceeds vector start, writer now returns an explicit overlap error.
- Context:
  - Prior implementation wrote `.addr` immediately after emitted code/data in multi-bank mode, which shifted vectors earlier in bank when tail bytes were mostly zero and directly caused vector-zone mismatch clusters.

Validation:

- Test suite:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase12 go test ./...`
  - Result: success.
- Focused verify checks that were previously failing with vector-zone mismatches now pass:
  - `Bomberman II (USA).nes` (mapper 1) `EXIT:0`
  - `Casino Kid II (USA).nes` (mapper 2) `EXIT:0`
  - `The Black Bass (USA).nes` (mapper 2) `EXIT:0`
- Not-working baseline after fix:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase12_mapper_corpus_notworking.csv`
  - Summary:
    - mapper `1`: `2 pass / 0 fail / 2 total` (was `1/2`)
    - mapper `2`: `4 pass / 3 fail / 7 total` (was `2/7`)
- Working corpus guard:
  - `scripts/benchmark_mapper_corpus.sh -g working -a ca65 -o /tmp/phase12_mapper_corpus_working.csv`
  - Summary unchanged:
    - mapper `0`: `41/41`
    - mapper `3`: `6/6`
    - mapper `7`: `1/1`
- Sweep comparison (same phase-11 matrix shape):
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8,16,32 -b 0,64,128,256 -o /tmp/phase12_trace_sweep_m12_matrix.csv`
  - Highlights:
    - branch `0` now yields improved baseline:
      - mapper `1`: `2/2`
      - mapper `2`: `4/7`
    - mapper `1` still degrades to `1/2` when branch expansion is enabled (`64/128/256`) for this ROM set.
- Post-fix artifact clustering:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8 -b 0 -d /tmp/phase12_artifacts -o /tmp/phase12_trace_sweep_m12_artifacts.csv`
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase12_artifacts -o /tmp/phase12_failure_cluster_summary.md -c /tmp/phase12_failure_cluster_details.csv -n 12 -k 2`
  - Result:
    - remaining failures reduced to 3 ROMs (all mapper 2).
    - previous stable vector-offset clusters are absent (`offset_stable.csv` empty at `min_hits=2`).

Post-phase note:

- The stable vector-adjacent mismatch family identified in Phase 11 is resolved for the current corpus slice; gains came from output-placement correctness rather than additional branch/frontier tuning.
- Remaining failures are now concentrated in:
  - `Alfred Chicken` (large PRG mismatch)
  - `Casino Kid` (assembler range errors)
  - `Archon` (cartridge/PRG load error: unexpected EOF)

### Phase 13: Branch-Expansion Regression Guard (Mapper 1)

Status: Completed (2026-02-24)

1. Isolate and mitigate mapper `1` regression that reappeared when branch alternate budgets were enabled.
2. Tighten advisory-trace seeding policy to avoid speculative parse roots not executed by emulator.
3. Re-run branch-budget sweeps to confirm mapper `1` stability across `max_branch` settings.

Acceptance:

1. `Bomberman II` no longer regresses when `-trace-max-branch-states > 0`.
2. Mapper `1` pass rate remains stable across branch-budget matrix settings.
3. No regressions in mapper `2` and full test suite remains green.

Implementation notes:

- Removed speculative non-executed alternate seeding:
  - `internal/disasm/emutrace.go`
  - `seedFromAdvisoryEmuTrace()` now seeds parse roots only from `result.Steps` (executed instructions), and no longer adds parse roots from `result.BranchAlternates` directly.
- Added mapper-aware branch alternate guard:
  - `internal/disasm/emutrace.go`
  - In `runAdvisoryEmuTrace()`, when cartridge mapper is `1` and requested `MaxBranchStates > 0`, branch alternate exploration is clamped to `0`.
  - Rationale: current mapper `1` corpus shows branch alternates are net-negative and can introduce single-byte mismatches under expanded branch budgets.

Validation:

- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase13 go test ./...`
  - Result: success.
- Targeted regression check:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase13 go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 8 -trace-max-branch-states 64 -o /tmp/phase13_bomberman_b64_guard.asm "internal/testroms/commercial/notworking/Bomberman II (USA).nes"`
  - Result: success (`EXIT:0`).
- Mapper 1/2 branch-budget sweep (`v=8`):
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8 -b 0,64,128,256 -o /tmp/phase13_trace_sweep_m12_v8.csv`
  - Summary:
    - mapper `1`: `2/2` pass for branch `0,64,128,256` (regression removed)
    - mapper `2`: `4/7` pass for branch `0,64,128,256` (unchanged by this guard)
- Matrix confirmation (`v=8,16,32`):
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -i 200000 -v 8,16,32 -b 0,64,128,256 -o /tmp/phase13_trace_sweep_m12_matrix.csv`
  - Summary:
    - mapper `1`: `2/2` pass for all 12 configs
    - mapper `2`: `4/7` pass for all 12 configs

Post-phase note:

- Branch budget tuning for mapper `1` is now stabilized through a conservative guard; this is intentionally pragmatic and can be relaxed later once mapper-1-safe alternate-path heuristics are available.
- Remaining improvement opportunity is concentrated in mapper `2` ROM-specific failures, not branch-budget instability.

### Phase 14: Assembler Failure Forensics in Artifact Capture

Status: Completed (2026-02-24)

1. Improve failure artifacts for mapper-specific triage by capturing assembler error diagnostics directly from verify logs.
2. Add source-line context extraction for `ca65` line-based errors (e.g. `Range error`).
3. Keep disassembly behavior unchanged while increasing failure observability.

Acceptance:

1. Failure artifacts include parsed assembler error lines when present.
2. Artifacts include concise error-type summary and source-line context snippets.
3. Existing pass/fail benchmark behavior remains unchanged.

Implementation notes:

- Enhanced artifact capture in both benchmark scripts:
  - `scripts/benchmark_mapper_corpus.sh`
  - `scripts/benchmark_trace_sweep.sh`
- New per-failure files (only when assembler line errors are present in logs):
  - `assembler_errors.txt`
    - extracted entries like: `out.asm(2361): Error: Range error`
  - `assembler_error_summary.txt`
    - grouped counts by error type
  - `assembler_error_context.txt`
    - nearby `disasm.asm` lines (`line-3` to `line+3`) for each extracted error
    - context extraction is capped (40 entries) to keep artifacts bounded
- Parsing behavior:
  - regex extracts all embedded `out.asm(<line>): Error: ...` fragments even when logged as escaped `\n` in one JSON log line.
  - no new behavior changes in disassembly or mapper runtime; this phase is observability only.

Validation:

- Syntax sanity:
  - `bash -n scripts/benchmark_trace_sweep.sh scripts/benchmark_mapper_corpus.sh scripts/cluster_failure_artifacts.sh`
  - Result: success.
- Not-working baseline with artifact capture:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -d /tmp/phase14_artifacts -o /tmp/phase14_mapper_corpus_notworking.csv`
  - Summary unchanged from Phase 13 baseline:
    - mapper `1`: `2/2` pass
    - mapper `2`: `4/7` pass
  - `Casino Kid` failure artifact now includes:
    - `assembler_errors.txt`
    - `assembler_error_summary.txt`
    - `assembler_error_context.txt`
- Mapper 2 focused sweep with artifact capture:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -d /tmp/phase14_artifacts_sweep -o /tmp/phase14_trace_sweep_m2.csv`
  - Summary:
    - mapper `2`: `4 pass / 3 fail / 7 total`
  - `Casino Kid` sweep artifact also includes assembler forensic files.

Post-phase note:

- Failure triage now has immediate line-level evidence for assembler failures, removing manual reproduction steps for `Casino Kid` and similar issues.
- Remaining mapper `2` failures now split cleanly into:
  - assembler-range class (`Casino Kid`)
  - large PRG mismatch class (`Alfred Chicken`)
  - invalid/corrupt input class (`Archon` unexpected EOF on PRG load)

### Phase 15: Unresolved Relative-Branch Fallback Hardening

Status: Completed (2026-02-24)

1. Eliminate `ca65` range-error failures caused by unresolved `_label_XXXX` symbols used by relative branch opcodes.
2. Keep fallback aliases for unresolved non-branch symbols (`JSR/JMP/.word` cases).
3. Improve mapper-2 notworking corpus pass rate without changing trace-frontier policy.

Acceptance:

1. `Casino Kid (USA).nes` verifies successfully with `-a ca65`.
2. Mapper `2` notworking baseline improves from `4/7` to `5/7` pass.
3. Unit/integration test suite remains green.

Implementation notes:

- Targeted fallback behavior was added in `internal/mapper/processor.go`:
  - During `addMissingSymbolAliases()`, unresolved symbol references are still scanned from emitted code.
  - For unresolved **relative branch** instructions (`BCC/BCS/BEQ/BMI/BNE/BPL/BVC/BVS`), fallback symbol aliasing is bypassed and the instruction is rewritten as raw bytes from the original opcode payload:
    - example output form: `.byte $10, $0F`
  - For unresolved **non-branch** references, existing alias fallback behavior remains unchanged.
- New helpers:
  - `isRelativeBranchCode()`
  - `rewriteRelativeBranchAsBytes()`
- New tests in `internal/mapper/processor_test.go`:
  - `TestSetProgramBanks_RewritesUnresolvedRelativeBranchAsRawBytes`
  - `TestSetProgramBanks_KeepsAliasForUnresolvedNonBranchSymbol`

Validation:

- Mapper package tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase15 go test ./internal/mapper/...`
  - Result: success.
- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase15 go test ./...`
  - Result: success.
- Target ROM verification:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase15 go run . -verify -q -a ca65 -s nes -o /tmp/casino_phase15_after2_I7Ds/out.asm "internal/testroms/commercial/notworking/Casino Kid (USA).nes"`
  - Result: success (`EXIT:0`).
- Notworking mapper baseline:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase15 scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase15_mapper_corpus_notworking.csv`
  - Summary:
    - mapper `1`: `2/2` pass
    - mapper `2`: `5/7` pass (improved from `4/7`)
- Mapper `2` focused sweep:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase15 scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -o /tmp/phase15_trace_sweep_m2.csv`
  - Summary:
    - mapper `2`: `5 pass / 2 fail / 7 total`

Post-phase note:

- The `Casino Kid` assembler-range failure class is resolved by making unresolved relative branches assembler-safe while preserving exact ROM bytes.
- Remaining mapper `2` failure classes are now:
  - `Alfred Chicken` (large PRG mismatch)
  - `Archon` (unexpected EOF / corrupt PRG load)

### Phase 16: Alfred Flow Instrumentation (Mapper-Write Hotspots)

Status: Completed (2026-02-24)

1. Add focused trace instrumentation for mapper-write/flow triage on `Alfred Chicken`.
2. Keep disassembly behavior unchanged while improving observability of mapper activity.
3. Produce concrete next-step evidence for why large PRG mismatch remains.

Acceptance:

1. Advisory emulator trace emits mapper-write concentration metrics (address/PC/transition).
2. Instrumentation is covered by tests and does not regress existing suites.
3. `Alfred Chicken` run captures actionable telemetry to guide the next implementation phase.

Implementation notes:

- Added mapper-write hotspot aggregation to `internal/trace/m6502emu/trace.go`:
  - New result metadata:
    - `MapperWriteUniqueAddresses`
    - `MapperWriteUniquePCs`
    - `MapperWriteUniqueTransitions`
    - `MapperWriteAddressHotspots`
    - `MapperWritePCHotspots`
    - `MapperWriteTransitionHotspots`
  - Hotspot aggregation is computed during result finalization from `BankSwitchWrites`.
  - Top-N hotspot slices are bounded (`mapperHotspotLimit=8`) and sorted deterministically.
- Emu trace logging in `internal/disasm/emutrace.go` now emits:
  - `mapper_write_unique_addresses`
  - `mapper_write_unique_pcs`
  - `mapper_write_unique_transitions`
  - `mapper_write_top_addresses`
  - `mapper_write_top_pcs`
  - `mapper_write_top_transitions`
- Added coverage in `internal/trace/m6502emu/trace_test.go`:
  - `TestRunBuildsMapperWriteHotspots`

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase16 go test ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_phase16 go test ./internal/disasm -count=1`
  - Result: success.
- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase16 go test ./...`
  - Result: success.
- Alfred instrumentation run:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase16 go run . -debug -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 500000 -trace-max-visits-per-state 64 -trace-max-branch-states 0 -o /tmp/alfred_phase16_v64_UoKU/out.asm "internal/testroms/commercial/notworking/Alfred Chicken (USA).nes"`
  - Result: verify still fails with `segment PRG mismatch: 24061 offset mismatches` (expected for this phase).
  - New telemetry:
    - `instructions=212`, `unique_pc=17`, `halt_reason="pc visit limit exceeded at $C5E1"`
    - `mapper_writes=0`
    - `mapper_write_unique_addresses=0`
    - `mapper_write_top_addresses="none"`
- Baseline guard check:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase16 scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase16_mapper_corpus_notworking.csv`
  - Summary unchanged from Phase 15:
    - mapper `1`: `2/2` pass
    - mapper `2`: `5/7` pass

Post-phase note:

- The new instrumentation confirms current Alfred failure is not a late mapper-switch reconstruction issue in traced paths; the advisory trace remains trapped in an early loop (`$C5E1`) and never executes mapper writes.
- This narrows the next corrective work to startup-loop escape realism (I/O stub behavior and/or seeded alternate path policy), not additional mapper-write postprocessing.

### Phase 17: Startup Loop Escape Realism (PPU + Counter-Loop Budget)

Status: Completed (2026-02-24)

1. Improve advisory startup-path realism for mapper-2 failures without changing mapper semantics.
2. Unblock common boot-time delay/clear loops under low `max_visits` settings.
3. Re-run Alfred with default sweep settings (`visits=8`) and measure coverage delta.

Acceptance:

1. Alfred no longer halts in the initial `$C5E1` delay loop under `visits=8`.
2. Emulator trace still deterministic and test suite remains green.
3. Mapper baseline pass/fail behavior does not regress.

Implementation notes:

- Dynamic PPU status stub added in `internal/trace/m6502emu/bus.go`:
  - `$2002` now alternates deterministic clear/set vblank states (`0x00`, `0x80`, ...), instead of hard-coded always-set.
  - `ppuStatusReadCount` is included in bus snapshot/restore so branch-state replay remains deterministic.
- Added bounded startup-loop visit relaxation in `internal/trace/m6502emu/trace.go`:
  - New boot-loop cap: `bootLoopVisitLimit=2048`.
  - When per-PC visit budget is exceeded, advisory trace can temporarily relax only for detected tight counter loops (`DEX/DEY/INX/INY` + short backward branch patterns).
  - Guardrails:
    - no observed mapper *mapping changes* yet (`Changed=true` events disable relaxation)
    - trace progress still bounded by instruction budget and per-loop cap.
- New/updated tests in `internal/trace/m6502emu/trace_test.go`:
  - `TestNesBusPPUStatusStub` (dynamic sequence)
  - `TestNesBusPPUStatusSnapshotRestore`
  - `TestRunEscapesStartupCounterLoopWithDefaultVisitBudget`
  - `TestRunEscapesStartupMemoryClearLoopWithDefaultVisitBudget`

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 go test ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 go test ./internal/disasm -count=1`
  - Result: success.
- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 go test ./...`
  - Result: success.
- Alfred default-visits run:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 go run . -debug -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 8 -trace-max-branch-states 0 -o /tmp/alfred_phase17_v8e_PTld/out.asm "internal/testroms/commercial/notworking/Alfred Chicken (USA).nes"`
  - Result: verify still fails with `segment PRG mismatch: 24061 offset mismatches` (not fixed in this phase).
  - Coverage/flow delta vs Phase 16 (`v=8`):
    - before: `instructions=212`, `unique_pc=17`, `mapper_writes=0`, halt at `$C5E1`
    - after: `instructions=8103`, `unique_pc=87`, `mapper_writes=3`, halt at `$81FA`
- Notworking mapper baseline:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase17_mapper_corpus_notworking.csv`
  - Summary unchanged:
    - mapper `1`: `2/2` pass
    - mapper `2`: `5/7` pass
- Mapper 2 focused sweep:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase17 scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -o /tmp/phase17_trace_sweep_m2.csv`
  - Summary unchanged:
    - mapper `2`: `5 pass / 2 fail / 7 total`

Post-phase note:

- Startup-loop realism materially improved Alfred trace depth under default budgets (5x+ unique PC coverage gain), but ROM verification mismatch remains unchanged.
- The current halt hotspot (`$81FA`) sits in repeated PPU update flow, and nearby control flow includes JOYPAD polling (`$8217`), indicating the next likely gains come from richer IO timing/input stubs rather than mapper-write handling.

### Phase 18: JOYPAD Serial Stub Realism

Status: Completed (2026-02-24)

1. Implement hardware-like controller strobe/latch/shift semantics for `$4016/$4017` in advisory emu mode.
2. Preserve determinism and branch-state reproducibility by snapshotting controller internal state.
3. Re-measure Alfred loop hotspots after controller realism improvements.

Acceptance:

1. JOYPAD reads follow NES serial behavior (strobe, latched shift register, post-8-bit reads).
2. Controller emulation state survives snapshot/restore correctly.
3. No regressions in corpus pass/fail baseline.

Implementation notes:

- `internal/trace/m6502emu/bus.go`:
  - Added controller state:
    - `joypadStrobe`
    - `joypadLatched[2]`
    - `joypadShift[2]`
    - `joypadBitsLeft[2]`
  - Added controller handlers:
    - `writeJoypadStrobe()`
    - `latchJoypads()`
    - `sampleJoypadState()`
    - `readJoypad()`
  - Current input policy is intentionally conservative:
    - deterministic neutral state (no buttons pressed) so behavior remains stable while read protocol becomes realistic.
  - Snapshot/restore now includes all JOYPAD internal fields to keep advisory branch replay deterministic.
- `internal/trace/m6502emu/trace_test.go`:
  - Added:
    - `TestNesBusJoypadStrobeLatchShift`
    - `TestNesBusJoypadSnapshotRestore`

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 go test ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 go test ./internal/disasm -count=1`
  - Result: success.
- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 go test ./...`
  - Result: success.
- Alfred check (`visits=8`):
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 go run . -debug -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 8 -trace-max-branch-states 0 -o /tmp/alfred_phase18_v8_TOUT/out.asm "internal/testroms/commercial/notworking/Alfred Chicken (USA).nes"`
  - Result: verify still fails with `segment PRG mismatch: 24061 offset mismatches`.
  - Telemetry remained effectively unchanged from Phase 17:
    - `instructions=8103`
    - `unique_pc=87`
    - `mapper_writes=3`
    - `halt_reason="pc visit limit exceeded at $81FA"`
- Baseline checks:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase18_mapper_corpus_notworking.csv`
    - mapper `1`: `2/2` pass
    - mapper `2`: `5/7` pass
  - `GOCACHE=/tmp/retrodisasm_gocache_phase18 scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -o /tmp/phase18_trace_sweep_m2.csv`
    - mapper `2`: `5 pass / 2 fail / 7 total`

Post-phase note:

- Controller protocol fidelity is now materially better, but Alfred remains dominated by PPU update/control-loop behavior (`$81FA` hotspot).
- Next gains should focus on PPU-side progression semantics and selective input scripting, not additional controller protocol changes.

### Phase 19: PPU Data-Loop Progression Hook (Targeted Visit-Cap Relaxation)

Status: Completed (2026-02-24)

1. Add a lightweight progression hook for repeated `PPU_DATA` update loops so advisory trace can pass common VRAM streaming loops under default `visits=8`.
2. Keep relaxation tightly scoped to known short PPU stream loop shapes and preserve global boundedness.
3. Re-measure Alfred hotspot/coverage progression after this hook.

Acceptance:

1. A representative `STA $2007` stream loop can complete under default visit budgets in unit tests.
2. Alfred trace moves beyond the prior `$81FA` hotspot under `visits=8`.
3. Full test suite and mapper baseline remain stable.

Implementation notes:

- `internal/trace/m6502emu/trace.go`:
  - Added second bounded relaxation tier:
    - `ppuLoopVisitLimit=8192`
    - `relaxedPPULoopVisitLimit()`
  - Added PPU stream loop detection:
    - `shouldRelaxVisitLimitForPPUDataLoop()`
    - `isPPUDataStreamLoopPC()`
    - `isPPUDataStreamPatternAt()`
    - `isSTAAbsolutePPUData()`
    - `isIndexCompareImmediateOpcode()`
  - Relaxation is still guarded by existing safety checks:
    - no observed mapper mapping transitions (`Changed=true`)
    - bounded step horizon (`len(res.Steps) <= 20000`)
    - capped mapper-write activity threshold.
- `internal/trace/m6502emu/trace_test.go`:
  - Added `TestRunEscapesPPUDataStreamLoopWithDefaultVisitBudget`.

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 go test ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 go test ./internal/disasm -count=1`
  - Result: success.
- Full tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 go test ./...`
  - Result: success.
- Alfred run (`visits=8`):
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 go run . -debug -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 200000 -trace-max-visits-per-state 8 -trace-max-branch-states 0 -o /tmp/alfred_phase19_v8_Lehq/out.asm "internal/testroms/commercial/notworking/Alfred Chicken (USA).nes"`
  - Result: verify still fails with `segment PRG mismatch: 24061 offset mismatches`.
  - Telemetry delta vs Phase 18:
    - before: `instructions=8103`, `unique_pc=87`, `mapper_writes=3`, halt at `$81FA`
    - after: `instructions=8305`, `unique_pc=111`, `mapper_writes=4`, halt at `$C93F`
- Baseline checks:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase19_mapper_corpus_notworking.csv`
    - mapper `1`: `2/2` pass
    - mapper `2`: `5/7` pass
  - `GOCACHE=/tmp/retrodisasm_gocache_phase19 scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -o /tmp/phase19_trace_sweep_m2.csv`
    - mapper `2`: `5 pass / 2 fail / 7 total`

Post-phase note:

- The PPU-loop hook improved Alfred trace progression again (new PCs reached, hotspot shifted deeper), confirming the bottleneck was partly PPU loop churn rather than branch frontier size.
- Verification mismatch remains unchanged, so remaining blockers are likely higher-level frame/input progression and mapper-path realism, not raw startup-loop depth.

### Phase 20: Rom City Rampage Hybrid Stabilization + Coverage Lift

Status: Completed (2026-02-24)

1. Fix `hybrid` parse ordering so emulator seeding cannot suppress static discovery.
2. Extend startup-loop relaxation for long indexed clear loops used by MMC5 startup code.
3. Make high-coverage hybrid (`branch=1024`) verify cleanly for both `asm6` and `ca65`.

Acceptance:

1. `Rom City Rampage.nes` verifies for both assemblers with higher hybrid coverage settings.
2. Generated `.asm` for Rom City Rampage has materially more code than static/low-branch hybrid output.
3. Focused unit/integration suites remain green.

Implementation notes:

- `internal/disasm/disasm.go`:
  - `hybrid` mode now runs in two passes:
    - static `followExecutionFlow()` first
    - emulator-seeded additive pass second
  - This prevents emulator queue seeding from reducing static parse coverage.
- `internal/disasm/emutrace.go`:
  - Added `traceMode()`/`isHybridTraceMode()` helpers used by the new two-pass flow.
- `internal/trace/m6502emu/trace.go`:
  - Startup loop heuristics broadened for long indexed clear loops (e.g. `STA ...,X ... DEX/BNE` patterns).
  - Backward-branch distance checks increased for startup loop detection.
  - Relaxation guard no longer hard-fails only because mapper register writes are numerous when they do not change mapping.
- `internal/trace/m6502emu/trace_test.go`:
  - Added `TestRunEscapesLongIndexedStartupClearLoopWithDefaultVisitBudget`.
- `internal/disasm/code.go`:
  - Branch-target label rewriting is now constrained to safer contexts.
  - For non-default mapping contexts, absolute branch/call operands keep literal targets (avoids cross-mapping label address drift that caused a 1-byte PRG mismatch in Rom City Rampage).
- `scripts/verify_rom_city_rampage.sh`:
  - Default hybrid branch budget updated from `512` to `1024` after stabilization.

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_rcb go test ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_rcb go test ./internal/disasm -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_rcb go test ./internal/mapper -count=1`
  - Result: success.
- Rom City Rampage verify (high-coverage hybrid):
  - `go run . -verify -q -a asm6 -s nes -trace-mode hybrid -trace-max-instr 1000000 -trace-max-visits-per-state 128 -trace-max-branch-states 1024 -o /tmp/rc_verify4_asm6.asm "internal/testroms/special/Rom City Rampage.nes"`
  - `go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 1000000 -trace-max-visits-per-state 128 -trace-max-branch-states 1024 -o /tmp/rc_verify4_ca65.asm "internal/testroms/special/Rom City Rampage.nes"`
  - Result: both pass.
- Regenerated target artifacts in requested directory:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`50664` lines, `code=973`)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`50589` lines, `code=973`)
- Coverage/output comparison (Rom City Rampage):
  - low-branch hybrid/static regime (`branch=512`): `code=893`
  - stabilized high-branch hybrid (`branch=1024`): `code=973`

Post-phase note:

- Rom City Rampage now verifies with both assemblers under a higher-coverage hybrid setting while producing noticeably more disassembled code.
- Remaining opportunity is to further increase `emu_unique_pc` on mapper-heavy paths without relying on broader speculative branch state growth.

### Phase 21: Rom City Rampage Bank-Context Expansion (Verified Higher Coverage)

Status: Completed (2026-02-24)

1. Expand mapper-context bank tracing so additional-bank vector seeding does not skip same-address vectors.
2. Raise Rom City Rampage hybrid trace budgets to a verified higher-coverage profile.
3. Keep strict reassembly parity for both `asm6` and `ca65`.

Acceptance:

1. Rom City Rampage output in `internal/testroms/special` verifies for both assemblers.
2. Rom City Rampage emitted code increases again versus Phase 20 output.
3. Focused architecture/disasm/mapper/trace tests remain green.

Implementation notes:

- `internal/arch/m6502/vectors.go`:
  - `InitializeBankVectors()` now queues valid bank vectors even when vector addresses match the last bank.
  - Rationale: parse keys are mapping-aware, so same CPU address under different mapping can still be distinct code.
- `internal/arch/m6502/vectors_bank_test.go`:
  - Updated coverage for the new behavior:
    - same-address-as-last vectors are now expected to queue when valid
    - invalid-opcode case now isolates only invalid candidate vectors
- `internal/disasm/emutrace.go`:
  - Advisory seed now also queues `BranchAlternates` destinations and vector handlers (`$FFFA/$FFFC/$FFFE`) across discovered mapping signatures (deterministic signature order).
  - This is additive and preserves mapping-context parse behavior.
- `scripts/verify_rom_city_rampage.sh`:
  - Updated default profile to:
    - `-trace-max-instr 2000000`
    - `-trace-max-visits-per-state 1024`
    - `-trace-max-branch-states 4096`

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_rce go test ./internal/disasm ./internal/arch/m6502 ./internal/trace/m6502emu ./internal/mapper -count=1`
  - Result: success.
- Rom City Rampage verify:
  - `go run . -verify -q -a asm6 -s nes -trace-mode hybrid -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096 -o /tmp/rc_revert_asm6.asm "internal/testroms/special/Rom City Rampage.nes"`
  - `go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096 -o /tmp/rc_revert_ca65.asm "internal/testroms/special/Rom City Rampage.nes"`
  - Result: both pass.
- Regenerated target artifacts:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`50791` lines, `code=1075`)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`50716` lines, `code=1075`)

Coverage/output comparison (Rom City Rampage):

- Phase 20 profile (`instr=1,000,000`, `visits=128`, `branch=1024`): `code=973`
- Phase 21 profile (`instr=2,000,000`, `visits=1024`, `branch=4096`): `code=1075`

Post-phase note:

- Rom City Rampage now emits another measurable step up in code while preserving deterministic rebuild parity.
- Output is still data-heavy overall, indicating remaining gains require deeper mapper/IO path realism rather than budget-only tuning.

### Phase 22: Deterministic JOYPAD Input Controls (Trace Config + Sweep)

Status: Completed (2026-02-24)

1. Implement deterministic JOYPAD mask controls for advisory emulator tracing via CLI/env.
2. Preserve default behavior (`neutral input`) while enabling reproducible input-profile experiments.
3. Run a Rom City Rampage JOYPAD sweep to quantify whether startup/menu input gates current coverage.

Acceptance:

1. Users can configure joypad latched state for controller 1/2 in trace mode.
2. New settings are covered by tests and do not regress existing behavior.
3. Rom City Rampage verify remains stable with and without custom JOYPAD masks.

Implementation notes:

- CLI/options plumbing:
  - `internal/options/options.go`
    - Added flags/options:
      - `-trace-joypad1`
      - `-trace-joypad2`
    - Values are controller latched bitmasks:
      - `A=1, B=2, Select=4, Start=8, Up=16, Down=32, Left=64, Right=128`
  - `internal/cli/cli.go`
    - Added propagation into `options.Disassembler`.
  - `internal/cli/cli_test.go`
    - Extended trace-flag parsing test coverage for `trace-joypad1/2`.
- Trace/runtime wiring:
  - `internal/disasm/emutrace.go`
    - Added env fallbacks:
      - `RETRODISASM_EMU_TRACE_JOYPAD1`
      - `RETRODISASM_EMU_TRACE_JOYPAD2`
    - Added joypad state fields to emu trace config and debug logging (`joypad1_state`, `joypad2_state`).
  - `internal/trace/m6502emu/trace.go`
    - Extended `Config` with `Joypad1State`/`Joypad2State`.
    - Passed config into bus construction.
  - `internal/trace/m6502emu/bus.go`
    - `sampleJoypadState()` now returns configured deterministic masks instead of hard-coded neutral.
  - `internal/trace/m6502emu/trace_test.go`
    - Updated bus constructor calls for config argument.
    - Added `TestNesBusJoypadConfiguredState`.

Validation:

- Full test suite:
  - `GOCACHE=/tmp/retrodisasm_gocache_rch go test ./...`
  - Result: success.
- Rom City Rampage verify (baseline high-coverage profile):
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
- JOYPAD mask sweep (Rom City Rampage, `asm6`, verified):
  - Tested masks:
    - `0,1,2,4,8,16,32,64,128,3,5,9,24,48,96,255`
  - Command profile:
    - `-trace-mode hybrid -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096`
    - `-trace-joypad1 <mask>`
  - Result:
    - all masks verified successfully
    - emitted code remained unchanged (`code=1075`) across tested masks.

Post-phase note:

- JOYPAD scripting controls are now available and deterministic, enabling reproducible input experiments without code changes.
- For Rom City Rampage, controller mask variation did not change discovered code in current trace horizons, so the dominant remaining bottleneck is likely PPU/frame progression realism and mapper-path conditions, not neutral input alone.

### Phase 23: Deterministic JOYPAD Timeline Scripting (Per-Latch Sequences)

Status: Completed (2026-02-24)

1. Extend JOYPAD trace controls from static masks to deterministic per-latch sequences.
2. Keep sequence execution deterministic across branch-state replay by snapshotting sequence cursors.
3. Verify Rom City Rampage rebuild parity remains stable while enabling richer scripted input experiments.

Acceptance:

1. Users can provide JOYPAD sequences via CLI/env for controller 1/2.
2. Sequence advances on latch cycles and holds the final state once exhausted.
3. Snapshot/restore preserves sequence position and replay determinism.

Implementation notes:

- CLI/options plumbing:
  - `internal/options/options.go`
    - Added:
      - `-trace-joypad1-seq`
      - `-trace-joypad2-seq`
    - Added fields:
      - `TraceJoypad1Sequence`
      - `TraceJoypad2Sequence`
  - `internal/cli/cli.go`
    - Wired sequence flags into disassembler options.
  - `internal/cli/cli_test.go`
    - Extended trace-flag parsing test coverage for sequence fields.
- Trace config parsing:
  - `internal/disasm/emutrace.go`
    - Added env fallbacks:
      - `RETRODISASM_EMU_TRACE_JOYPAD1_SEQ`
      - `RETRODISASM_EMU_TRACE_JOYPAD2_SEQ`
    - Added robust sequence parsing with CLI precedence:
      - separators: comma/semicolon/colon/whitespace
      - token formats: decimal, `0xNN`, `$NN`
    - Added debug fields:
      - `joypad1_seq_len`
      - `joypad2_seq_len`
  - `internal/disasm/emutrace_test.go`
    - Added parser/env precedence coverage:
      - `TestParseJoypadSequence`
      - `TestParseJoypadSequenceInvalidToken`
      - `TestParseJoypadSequenceSettingPrefersCLI`
      - `TestParseJoypadSequenceSettingUsesEnv`
- Emulator bus/runtime behavior:
  - `internal/trace/m6502emu/trace.go`
    - Extended config:
      - `Joypad1Sequence`
      - `Joypad2Sequence`
    - Defensive sequence-copy normalization added.
  - `internal/trace/m6502emu/bus.go`
    - Added per-controller sequence cursor state (`joypadSeqIndex`) to bus + snapshot.
    - Latch behavior updated:
      - strobe high: latch current scripted state without advancing
      - high->low transition: latch and advance sequence
    - Sequence policy:
      - use scripted value when present
      - fall back to static `trace-joypad*` mask when sequence is absent
      - hold the last scripted value once cursor reaches end
  - `internal/trace/m6502emu/trace_test.go`
    - Added:
      - `TestNesBusJoypadSequenceAdvancesPerLatchCycle`
      - `TestNesBusJoypadSequenceSnapshotRestore`

Validation:

- Focused tests:
  - `GOCACHE=/tmp/retrodisasm_gocache_phase23 go test ./internal/cli ./internal/disasm ./internal/trace/m6502emu -count=1`
  - `GOCACHE=/tmp/retrodisasm_gocache_phase23 go test ./internal/arch/m6502 ./internal/mapper -count=1`
  - Result: success.
- Rom City Rampage baseline verify:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
- Rom City Rampage sequence-profile verify:
  - `go run . -verify -q -a asm6 -s nes -trace-mode hybrid -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096 -trace-joypad1-seq "8,8,0,0,0" -o /tmp/phase23_rc_asm6_seq.asm "internal/testroms/special/Rom City Rampage.nes"`
  - Result: success.
  - Output check:
    - `cmp -s /tmp/phase23_rc_asm6_seq.asm internal/testroms/special/Rom\ City\ Rampage.asm6.asm` -> identical.

Post-phase note:

- Timeline scripting infrastructure is now in place for deterministic menu/runtime progression experiments without code edits.
- For current Rom City Rampage profiles, scripted sequence variation is stable but does not yet increase discovered code, so the next leverage point remains PPU/frame progression realism.

### Phase 24: JOYPAD Timeline Preset Sweep Automation (Rom City Rampage)

Status: Completed (2026-02-24)

1. Add a repeatable JOYPAD timeline preset sweep for Rom City Rampage using the new `-trace-joypad1-seq` controls.
2. Record verification status and coverage deltas in a machine-readable CSV.
3. Keep generated artifacts in `internal/testroms/special` for direct inspection/reuse.

Acceptance:

1. Script supports deterministic preset runs with configurable assembler/trace budgets.
2. Output includes per-preset verify status and code-line heuristic deltas versus neutral baseline.
3. Preset sweep runs cleanly for `asm6` and `ca65`.

Implementation notes:

- New automation script:
  - `scripts/sweep_rom_city_rampage_joypad_timeline.sh`
  - Supports:
    - assembler mode: `asm6|ca65|all`
    - trace budgets: `-i/-v/-b`
    - preset selection: `-p`
    - output dir and CSV path: `-d/-o`
  - Built-in deterministic presets:
    - `neutral`
    - `start_tap`
    - `start_hold`
    - `right_then_start`
    - `down_then_start`
    - `a_then_start`
    - `menu_probe`
- Metrics captured per run:
  - `status` (verify pass/fail)
  - `duration_ms`
  - `asm_lines`
  - `asm_bytes`
  - `code_lines`
  - `code_delta_vs_neutral`
- Coverage heuristic used by the script:
  - counts mnemonic-like lines with regex `^[[:space:]]+[a-z]{3}\b`
  - this matches existing Rom City Rampage reporting (`code=1075` at current high-coverage profile).

Validation:

- Script syntax:
  - `bash -n scripts/sweep_rom_city_rampage_joypad_timeline.sh`
  - Result: success.
- `asm6` sweep run:
  - `scripts/sweep_rom_city_rampage_joypad_timeline.sh -a asm6 -o /tmp/phase24_rc_joypad_sweep_asm6.csv`
  - Result:
    - all 7 presets verified successfully
    - `code_lines=1075` for all presets (`delta=0`).
- `asm6+ca65` sweep run:
  - `scripts/sweep_rom_city_rampage_joypad_timeline.sh -a all -o /tmp/phase24_rc_joypad_sweep_all.csv`
  - Result:
    - all 14 runs (7 presets x 2 assemblers) verified successfully
    - `code_lines=1075` for all presets on both assemblers (`delta=0`).

Post-phase note:

- Timeline preset sweeps are now one-command reproducible and directly comparable, which removes manual setup friction for input-path experiments.
- Current Rom City Rampage traces remain flat across tested JOYPAD timelines, reinforcing that the next gains are more likely from PPU/frame progression fidelity than controller timelines alone.

### Phase 25: Mapper-Triage JOYPAD Timeline Sweep (Mapper 2 + Telemetry)

Status: Completed (2026-02-24)

1. Extend JOYPAD timeline preset sweeps from Rom City Rampage to mapper-2 notworking triage ROMs.
2. Capture advisory emu-trace telemetry per run (`unique_pc`, `mapper_writes`, `halt_reason`, etc.) alongside verify outcome.
3. Add lightweight failure-class tagging to separate input-integrity failures from trace/mapper mismatch failures.

Acceptance:

1. Sweep script supports mapper/group/preset filters and emits machine-readable CSV.
2. Output includes both verify status and telemetry fields required for preset-impact comparison.
3. Mapper-2 notworking sweep runs successfully and produces actionable summary signals.

Implementation notes:

- New triage sweep script:
  - `scripts/sweep_mapper_joypad_timeline.sh`
  - Default target profile:
    - `group=notworking`
    - `mappers=2`
    - `trace-mode=hybrid`
    - `max_instr=200000`
    - `max_visits=8`
    - `max_branch=0`
  - Presets:
    - `neutral`
    - `start_tap`
    - `start_hold`
    - `right_then_start`
    - `down_then_start`
    - `a_then_start`
    - `menu_probe`
- New CSV output fields include:
  - run identity:
    - `rom`, `set`, `mapper`, `preset`, `sequence`
  - result:
    - `status`, `failure_class`, `duration_ms`
  - output size/coverage:
    - `asm_lines`, `asm_bytes`, `code_lines`
  - advisory telemetry:
    - `unique_pc`, `unique_mapping`, `mapper_writes`, `mapping_changes`
    - `conditional_branches`, `branch_alternates`, `branch_states_executed`
    - `halt_reason`, `emu_only_pc`, `static_only_pc`
  - optional artifact pointer:
    - `artifact_path`
- Failure classification tags:
  - `input_corrupt`
  - `assembler_range`
  - `prg_mismatch`
  - `verification_failed`
  - `disasm_failed`
  - `unknown`
- Script now prints end-of-run summary:
  - total/pass/fail counts
  - failure-class counts
  - per-ROM telemetry-variant counts (stability check across presets)

Validation:

- Script syntax:
  - `bash -n scripts/sweep_mapper_joypad_timeline.sh`
  - Result: success.
- Mapper-2 notworking sweep:
  - `scripts/sweep_mapper_joypad_timeline.sh -g notworking -m 2 -o /tmp/phase25_mapper2_joypad_timeline.csv`
  - Summary:
    - `total runs: 49` (`7 ROMs * 7 presets`)
    - `pass runs: 35`
    - `fail runs: 14`
    - failure classes:
      - `prg_mismatch: 7` (`Alfred Chicken`)
      - `input_corrupt: 7` (`Archon`)
- Telemetry correlation highlights:
  - Per-ROM telemetry variants across presets were `1` for all 7 ROMs, i.e. no preset-dependent telemetry divergence under current budgets.
  - `Alfred Chicken` remained fixed at:
    - `unique_pc=111`
    - `mapper_writes=4`
    - `halt_reason="pc visit limit exceeded at $C93F"`
    - `status=fail` (`prg_mismatch`) for all presets.
  - `Archon` consistently classified as `input_corrupt` with no advisory telemetry emitted.

Post-phase note:

- Mapper-2 preset sweeps are now reproducible and telemetry-correlated, but current JOYPAD timelines do not move execution into new mapper/input states for this corpus slice.
- Remaining progress is most likely tied to PPU/frame progression fidelity and/or altered trace budgets/policies, not additional preset variants alone.

### Phase 26: Benchmark Failure-Class Classifier Hardening (Baseline + Sweep)

Status: Completed (2026-02-24)

1. Add explicit failure classification to the main benchmark harnesses so input-corrupt failures are separated from mapper/trace failures.
2. Persist classification in CSV outputs for downstream clustering and triage automation.
3. Add failure-class summary tables to benchmark console output.

Acceptance:

1. Baseline and sweep CSVs include a `failure_class` column.
2. Existing pass/fail totals remain unchanged while failure causes are separated.
3. `Archon`-class corrupt-input failures are clearly tagged as input-integrity failures.

Implementation notes:

- `scripts/benchmark_mapper_corpus.sh`
  - Added `classify_failure()` with categories:
    - `input_corrupt`
    - `assembler_range`
    - `prg_mismatch`
    - `verification_failed`
    - `disasm_failed`
    - `unknown`
  - CSV schema updated:
    - from: `rom,set,mapper,status,duration_ms,artifact_path`
    - to:   `rom,set,mapper,status,failure_class,duration_ms,artifact_path`
  - Console output now includes per-ROM class (`class=<...>`) and aggregate failure-class counts.
- `scripts/benchmark_trace_sweep.sh`
  - Added matching `classify_failure()` logic.
  - CSV schema updated:
    - from: `rom,set,mapper,trace_mode,max_instr,max_visits,max_branch,status,duration_ms,artifact_path`
    - to:   `rom,set,mapper,trace_mode,max_instr,max_visits,max_branch,status,failure_class,duration_ms,artifact_path`
  - Console output now includes per-run class and aggregated class counts by `(mapper, mode, max_instr, max_visits, max_branch)`.

Validation:

- Syntax checks:
  - `bash -n scripts/benchmark_mapper_corpus.sh scripts/benchmark_trace_sweep.sh`
  - Result: success.
- Baseline benchmark smoke run:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -o /tmp/phase26_mapper_corpus_notworking.csv`
  - Summary:
    - mapper `1`: `2 pass / 0 fail`
    - mapper `2`: `5 pass / 2 fail`
    - failure classes:
      - `input_corrupt: 1` (`Archon`)
      - `prg_mismatch: 1` (`Alfred Chicken`)
- Sweep benchmark smoke run:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -a ca65 -o /tmp/phase26_trace_sweep_m2.csv`
  - Summary:
    - mapper `2` config (`hybrid, 200000, 8, 0`): `5 pass / 2 fail / 7 total`
    - failure classes:
      - `input_corrupt: 1`
      - `prg_mismatch: 1`
- CSV header checks:
  - `/tmp/phase26_mapper_corpus_notworking.csv` and `/tmp/phase26_trace_sweep_m2.csv` both contain the new `failure_class` field.

Post-phase note:

- The benchmark layer now cleanly separates trace/mapper regressions from ROM-input integrity failures, reducing false debugging loops on corrupt/truncated inputs.
- This de-risks upcoming PPU/frame-model experiments by making failure-type regressions immediately visible in automation output.

### Phase 27: Classifier-Aware Artifact Clustering Outputs

Status: Completed (2026-02-24)

1. Extend artifact clustering to consume/derive failure classes and emit class-grouped triage outputs.
2. Propagate `failure_class` into failure artifact metadata so clustering does not depend on fragile log parsing.
3. Add markdown sections that correlate mapper hotspots with failure class.

Acceptance:

1. Cluster details CSV includes `failure_class` per artifact.
2. New class-grouped cluster CSV outputs are generated (mapper+class counts, class-scoped offsets/labels).
3. Existing cluster outputs remain intact and benchmark harnesses stay compatible.

Implementation notes:

- Failure metadata propagation:
  - `scripts/benchmark_mapper_corpus.sh`
    - `write_failure_artifacts(...)` now receives `failure_class`.
    - `meta.txt` now includes `failure_class=<...>`.
  - `scripts/benchmark_trace_sweep.sh`
    - same `failure_class` propagation into `meta.txt`.
- Clustering enhancements:
  - `scripts/cluster_failure_artifacts.sh`
    - Added failure-class resolution:
      - read from `meta.txt` when available
      - fallback classification from `verify.log` when missing (backward-compatible with older artifacts)
    - Details CSV schema updated:
      - from: `rom_name,mapper,config,first_mismatch_offset,mismatch_count,labels_count,artifact_path`
      - to:   `rom_name,mapper,failure_class,config,first_mismatch_offset,mismatch_count,labels_count,artifact_path`
    - Added generated outputs:
      - `*_failure_class_clusters.csv` (`mapper,failure_class,artifact_hits`)
      - `*_offset_by_failure_class.csv`
      - `*_label_by_failure_class.csv`
      - stable variants for both class-scoped offset/label clusters (`*_stable.csv`)
    - Markdown summary additions:
      - `Failures by Class`
      - `Failures by Mapper + Class`
      - per-mapper class-scoped Top-N mismatch-offset and label tables.

Validation:

- Syntax checks:
  - `bash -n scripts/benchmark_mapper_corpus.sh scripts/benchmark_trace_sweep.sh scripts/cluster_failure_artifacts.sh`
  - Result: success.
- Baseline artifacts + clustering:
  - `scripts/benchmark_mapper_corpus.sh -g notworking -a ca65 -d /tmp/phase27_artifacts_base -o /tmp/phase27_mapper_corpus_notworking.csv`
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase27_artifacts_base -o /tmp/phase27_failure_cluster_summary.md -c /tmp/phase27_failure_cluster_details.csv -n 8 -k 2`
  - Result highlights:
    - artifacts analyzed: `2`
    - class clusters: `input_corrupt=1`, `prg_mismatch=1`
    - details CSV includes `failure_class`.
- Sweep artifacts + clustering:
  - `scripts/benchmark_trace_sweep.sh -g notworking -m 2 -i 200000 -v 8 -b 0 -a ca65 -d /tmp/phase27_artifacts_sweep -o /tmp/phase27_trace_sweep_m2.csv`
  - `scripts/cluster_failure_artifacts.sh -d /tmp/phase27_artifacts_sweep -o /tmp/phase27_sweep_failure_cluster_summary.md -c /tmp/phase27_sweep_failure_cluster_details.csv -n 8 -k 2`
  - Result highlights:
    - artifacts analyzed: `2`
    - class-aware markdown sections generated and populated
    - class-scoped offset/label CSV outputs generated successfully.

Post-phase note:

- Clustering now preserves failure semantics end-to-end, so corrupt-input artifacts are separated from mapper/trace mismatch artifacts in both CSV and markdown triage.
- This closes the tooling loop needed to evaluate upcoming PPU/frame-model experiments with cleaner regression signals.

### Phase 28: Core Code-Density Lift (Mapped-Bank Entry Seeding + NMI Hook)

Status: Completed (2026-02-24)

1. Increase emitted `.asm` code density through core disassembly/emulation changes (no new benchmark/sweep scripts).
2. Improve coverage from additional mapped banks by seeding conservative bank-entry anchors before per-bank tracing.
3. Add deterministic synthetic NMI support (gated by `PPUCTRL` NMI enable) so frame/NMI-driven code paths are reachable in advisory emu mode.

Acceptance:

1. Rom City Rampage output in `internal/testroms/special` verifies for both `asm6` and `ca65`.
2. Code-line metric increases versus Phase 21-27 baseline (`code=1075`).
3. Full Go test suite remains green.

Implementation notes:

- Additional bank tracing enhancement:
  - `internal/disasm/banks.go`
  - Added `seedLikelyMappedBankEntryPoints()` during `processAdditionalBanks()`:
    - seeds conservative anchors: `$8000,$A000,$C000,$E000`
    - only when first opcode matches likely entry/control-flow setup opcodes
    - then existing `followExecutionFlow()` processes those contexts.
  - Rationale: additional-bank vector tracing alone can miss bank-local routine islands with no unique vectors.
- Deterministic synthetic NMI plumbing:
  - `internal/trace/m6502emu/bus.go`
    - tracks `PPUCTRL` writes (`$2000`) and snapshots/restores this state.
    - added `ppuNMIEnabled()` gate.
  - `internal/trace/m6502emu/trace.go`
    - added deterministic synthetic NMI cadence (`syntheticNMIInterval=2048` instructions), gated by `ppuNMIEnabled()`.
    - integrates `cpu.TriggerNMI()` + `cpu.CheckInterrupts()` into main trace loop.
    - branch-state snapshot/restore now includes NMI cadence state (`instructionsSinceNMI`) for replay determinism.
  - `internal/trace/m6502emu/trace_test.go`
    - added/updated coverage:
      - `TestNesBusPPUCtrlNMIEnabledAndSnapshotRestore`
      - `TestRunTriggersSyntheticNMIWhenEnabled`

Validation:

- Focused and full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./internal/disasm ./internal/trace/m6502emu ./internal/mapper ./internal/arch/m6502 -count=1`
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`50937` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`50862` lines)
- Code-density result (same metric used in prior phases):
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - Before (Phase 21-27 baseline): `1075`
  - After Phase 28: `1191`
  - Delta: `+116` code lines.
- Trace telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `1112 -> 1194`
  - `parsed_offsets`: `1381 -> 1502`
  - `code_bytes_marked`: `2350 -> 2590`

Post-phase note:

- This phase directly improves `.asm` code density with core pipeline logic, not external tooling.
- Rom City Rampage remains fully reassemblable while emitting materially more code.

### Phase 29: Mapped-Bank Call-Target Expansion (Official Opcode Gate)

Status: Completed (2026-02-24)

1. Further increase emitted `.asm` code density by expanding mapped-bank call-target discovery.
2. Keep discovery bounded and assembly-safe by gating targets to official 6502 routine-start opcodes.
3. Preserve existing verification behavior for both `asm6` and `ca65`.

Acceptance:

1. Rom City Rampage still verifies for both assemblers.
2. Instruction-line metric increases materially beyond Phase 28.
3. Full Go test suite remains green.

Implementation notes:

- Core mapped-bank discovery expansion:
  - `internal/disasm/banks.go`
  - `seedLikelyMappedBankCallTargets()` updates:
    - increased per-bank candidate budget from `1024` to `2048`
    - replaced narrow entry-opcode whitelist target check with `isLikelyM6502RoutineStartOpcode(...)`
  - `isLikelyM6502RoutineStartOpcode(...)` uses `retrogolib` opcode table:
    - requires opcode to be defined and official (`Instruction != nil`, `Unofficial == false`)
    - excludes `brk`, `rti`, and `rts` as routine starts.
- No new script/tooling changes in this phase; this is a core disassembly logic change.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`56022` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`55947` lines)
- Code-density result (same metric used in prior phases):
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 28: `1191`
  - After Phase 29: `4764`
  - Delta: `+3573` code lines.
- Trace telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `5323`
  - `parsed_offsets`: `5694`
  - `code_bytes_marked`: `10331`

Post-phase note:

- This phase delivered a large code-density lift on the priority ROM without adding new helper scripts.
- Output remains reassemblable under both target assemblers.

### Phase 30: Mapped-Bank Pointer-Table Target Seeding (Shape-Guarded)

Status: Completed (2026-02-24)

1. Implement conservative pointer-table target seeding in mapped-bank discovery to recover callback/jump-table-only routines.
2. Keep behavior bounded and verification-safe using explicit table-shape guards and strict seeding caps.
3. Preserve existing output validity for `asm6` and `ca65`.

Acceptance:

1. Rom City Rampage verifies for both assemblers after adding pointer-table seeding.
2. Instruction-line density increases beyond Phase 29.
3. Full Go test suite remains green.

Implementation notes:

- Core disassembly update:
  - `internal/disasm/banks.go`
  - Added `seedLikelyMappedBankPointerTableTargets()` in `processAdditionalBanks()` after entry/call-target seeding.
- New pointer-table seeding guardrails:
  - source starts only on even addresses (`16-bit` shape guard)
  - source bytes must not already be typed as `CodeOffset`
  - contiguous run parsing of little-endian targets with:
    - `minRunEntries=4`
    - `maxRunEntries=64`
    - `maxSeedsPerBank=1024`
  - each target must pass:
    - valid code address bounds
    - official routine-start opcode gate (`isLikelyM6502RoutineStartOpcode`)
  - additional shape test (`hasLikelyPointerTableShape`):
    - minimum distinct targets
    - at least one close-neighbor pair (clustered routine region signal)
  - dedupes seeded targets per mapped bank pass.
- No script/tooling changes in this phase.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Code-density result (same metric used in prior phases):
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 29: `4764`
  - After Phase 30: `7418`
  - Delta: `+2654` code lines.
- Trace telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `9232`
  - `parsed_offsets`: `10151`
  - `code_bytes_marked`: `15740`

Post-phase note:

- This phase materially increased real disassembled code lines while keeping output reassemblable.
- The pointer-table pass is intentionally constrained to avoid uncontrolled data-to-code promotion.

### Phase 31: Mapper-1 Branch Alternate Policy (Clamp, Not Disable)

Status: Completed (2026-02-24)

1. Replace the mapper-1 branch-alternate hard disable with a mapper-safe budget policy.
2. Re-enable alternate-path exploration for mapper 1 while capping frontier growth under high trace budgets.
3. Preserve existing verification behavior for priority ROM outputs.

Acceptance:

1. Mapper-1 traces no longer force `MaxBranchStates=0`.
2. Mapper-1 runs show active branch alternates with bounded budgets.
3. Full Go tests and Rom City Rampage verification remain green.

Implementation notes:

- Policy change in advisory emu-trace integration:
  - `internal/disasm/emutrace.go`
  - Removed hard-disable block:
    - previous behavior: mapper 1 + `MaxBranchStates>0` => forced `0`.
  - Added `tunedBranchStateBudgetForMapper(...)`:
    - mapper 1 only
    - low budget cap: `512`
    - mid budget cap: `128` (`max_instructions >= 500000` or `max_visits_per_state >= 256`)
    - high budget cap: `64` (`max_instructions >= 1000000` or `max_visits_per_state >= 512`)
    - non-mapper-1 remains unchanged.
  - Added debug telemetry when policy adjusts requested budget.
- New tests:
  - `internal/disasm/emutrace_test.go`
  - Added coverage for:
    - mapper-1 high-budget clamp
    - mapper-1 mid-budget clamp
    - mapper-1 low-budget passthrough
    - non-mapper-1 unchanged behavior.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Mapper-1 policy proof (The Bard's Tale):
  - `go run . -debug ... -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096 ... "internal/testroms/commercial/notworking/The Bard's Tale (USA).nes"`
  - Log highlights:
    - `requested_max_branch_states=4096`
    - `effective_max_branch_states=64`
    - `branch_alternates=198` (re-enabled, not forced to `0`)
    - `branch_alternate_budget_drops=3023` (bounded as intended).
- Mapper-1 verify check:
  - `go run . -verify -q -a ca65 -s nes -trace-mode hybrid -trace-max-instr 2000000 -trace-max-visits-per-state 1024 -trace-max-branch-states 4096 -o /tmp/bards_phase31_verify.ca65.asm "internal/testroms/commercial/notworking/The Bard's Tale (USA).nes"`
  - Result: success.
- Rom City Rampage regression guard:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged from Phase 30:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
    - instruction-line metric: `7418`.

Post-phase note:

- Mapper-1 alternate-path exploration is now active under controlled limits.
- This phase improves policy correctness and mapper safety; primary Rom City code-density gains remain from Phases 29-30.

### Phase 32: Mapper-Aware Split Pointer-Table Seeding (`tbl_lo`/`tbl_hi`)

Status: Completed (2026-02-24)

1. Implement split low/high-byte pointer-table target seeding in mapped-bank discovery.
2. Keep split-table detection conservative via code-pattern and table-shape guards.
3. Preserve stability and reassembly validity for Rom City Rampage outputs.

Acceptance:

1. Split-table seeding is integrated into additional-bank processing.
2. Full Go test suite remains green.
3. Rom City Rampage verification remains green with no regressions.

Implementation notes:

- Core discovery integration:
  - `internal/disasm/banks.go`
  - `processAdditionalBanks()` now calls:
    - `seedLikelyMappedBankSplitPointerTableTargets()`
- New split-table detection pass:
  - Scans for paired indexed absolute loads (`LDA abs,X` / `LDA abs,Y`) within a short lookahead window.
  - Derives candidate `(table_lo, table_hi)` starts from paired load operands.
  - Requires table-start validity and bounded inter-table distance.
  - Tries both low/high orientation orders.
- New split-table target extraction guardrails:
  - bounded run length (`min=4`, `max=64`)
  - per-bank seed and pair caps (`maxSeedsPerBank=768`, `maxPairsPerBank=256`)
  - source bytes must not already be typed as `CodeOffset`
  - target must be a valid code address and pass official routine-start opcode gating
  - target sequence must satisfy pointer-table shape checks (distinct targets + neighborhood signal).
- Helper functions added:
  - `extractSplitPointerTargets(...)`
  - `readWordAt(...)`
  - `isLikelySplitTableAddress(...)`
  - `isLikelySplitPointerLoadOpcode(...)`
  - `tableByteDistance(...)`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 31: `7418`
  - After Phase 32: `7418`
  - Delta: `+0` code lines (no regression).
- Trace telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `9232`
  - `parsed_offsets`: `10151`
  - `code_bytes_marked`: `15740`
  - unchanged vs Phase 31 in this guarded configuration.

Post-phase note:

- The split-table framework is now in the core pipeline.
- On Rom City Rampage, current guard thresholds are stable but too conservative to yield additional code lift, so tuning should focus on pattern coverage rather than broader unguarded seeding.

### Phase 33: Split-Table Correlation Tuning (Index-Mode Pairing + Runtime Use Guard)

Status: Completed (2026-02-24)

1. Expand split-table candidate coverage to additional indexed-load pair shapes.
2. Reduce false positives by requiring nearby runtime pointer-use correlation.
3. Keep output and verification stability unchanged while improving detection precision.

Acceptance:

1. Split-table pairing supports compatible indexed-load variants beyond identical-opcode pairs.
2. Candidates are gated by nearby pointer-store and indirect-use evidence.
3. Full tests and Rom City verification remain green.

Implementation notes:

- Split-table pair-shape expansion:
  - `internal/disasm/banks.go`
  - Replaced strict `secondOp == firstOp` requirement with index-mode compatibility:
    - `isCompatibleSplitPointerLoadPair(...)`
    - `splitPointerIndexMode(...)`
  - Added conservative additional load variants:
    - `LDA abs,X` / `LDY abs,X`
    - `LDA abs,Y` / `LDX abs,Y`
- Runtime-use correlation guard:
  - Added `hasSplitPointerRuntimeCorrelation(...)` to require nearby evidence of:
    - consecutive pointer-byte stores (`STA/STX/STY` to `addr` and `addr+1`)
    - plus either:
      - `JMP (addr)` indirect use, or
      - indirect zeropage opcode usage on the same pointer base.
  - Correlation scan uses decoded opcode lengths (`opcodeSizeBytes(...)`) instead of raw byte stepping.
- Helper additions:
  - `readByteWordOperand(...)`
  - `opcodeSizeBytes(...)`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 32: `7418`
  - After Phase 33: `7418`
  - Delta: `+0` code lines (no regression).
- Trace telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `9232`
  - `parsed_offsets`: `10151`
  - `code_bytes_marked`: `15740`
  - unchanged vs Phase 32.

Post-phase note:

- This phase improved split-table candidate quality and runtime-shape fidelity.
- Remaining gain likely requires broader pointer-build pattern coverage (transfer/arithmetic variants), not looser global thresholds.

### Phase 34: Transfer/Arithmetic Pointer-Build Correlation Extension

Status: Completed (2026-02-24)

1. Extend split-table runtime correlation for transfer/arithmetic-built pointer variants.
2. Preserve strict indirect-use confirmation to avoid broad false-positive code promotion.
3. Keep verification and output stability for Rom City Rampage.

Acceptance:

1. Split-table correlation supports indexed store and pointer-byte arithmetic evidence.
2. Correlation still requires indirect-use evidence in the same local window.
3. Full tests and Rom City verification remain green.

Implementation notes:

- Correlation expansion in `internal/disasm/banks.go`:
  - `hasSplitPointerRuntimeCorrelation(...)` now tracks:
    - indexed pointer-byte stores (`STA/STX/STY` indexed zeropage forms)
    - pointer-byte arithmetic ops on zeropage (`INC/DEC` zp variants)
    - transfer/arithmetic signal opcodes (`TAX/TAY/TXA/TYA`, `ADC/SBC`, index inc/dec, carry prep)
  - Maintains strict indirect-use gate:
    - requires `JMP (abs)` and/or zeropage indirect addressing usage in the same lookahead window.
  - Variant match path now accepts:
    - consecutive indexed-store pointer-byte pairs, or
    - consecutive pointer-byte arithmetic pairs,
    - only when transfer/arithmetic signal + indirect-use evidence are both present.
- Helper additions:
  - `hasConsecutiveAddressPair(...)`
  - `isPointerArithmeticZPOpcode(...)`
  - `isPointerTransferArithmeticSignalOpcode(...)`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 33: `7418`
  - After Phase 34: `7418`
  - Delta: `+0` code lines (no regression).
- Trace telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `static_unique_pc`: `9232`
  - `parsed_offsets`: `10151`
  - `code_bytes_marked`: `15740`
  - unchanged vs Phase 33.

Post-phase note:

- Pointer-build correlation now covers the planned transfer/arithmetic variant family while preserving strict safety guards.
- Rom City coverage remains flat, indicating the remaining gap is likely mapper/PPU path reachability rather than split-table shape detection breadth.

### Phase 35: Split-Seeding Telemetry Counters

Status: Completed (2026-02-24)

1. Add split-seeding telemetry counters so Phases 29-34 behavior can be measured per ROM.
2. Surface counter values in existing trace-stat logging (no new tooling required).
3. Keep disassembly output and verification behavior unchanged.

Acceptance:

1. Trace stats include candidate-pair, correlation-reject, and accepted-target counters.
2. Counters are populated on Rom City Rampage runs.
3. Full tests and Rom City verification remain green.

Implementation notes:

- Trace stats model/logging updates:
  - `internal/disasm/stats.go`
  - Added fields:
    - `splitSeedCandidatePairs`
    - `splitSeedRejectedByCorr`
    - `splitSeedAcceptedTargets`
  - Added matching `Trace stats` debug output keys:
    - `split_seed_candidate_pairs`
    - `split_seed_rejected_correlation`
    - `split_seed_accepted_targets`
- Split-seeding instrumentation:
  - `internal/disasm/banks.go`
  - `seedLikelyMappedBankSplitPointerTableTargets()` now records:
    - candidate pairs after load/table-shape checks
    - correlation rejects when runtime-use guard fails
    - accepted seeded targets when a split-derived target is queued.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Telemetry proof (Rom City Rampage, asm6, hybrid profile):
  - `Trace stats` now includes:
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=41`
    - `split_seed_accepted_targets=0`
  - Existing core stats unchanged:
    - `static_unique_pc=9232`
    - `parsed_offsets=10151`
    - `code_bytes_marked=15740`
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 34: `7418`
  - After Phase 35: `7418`
  - Delta: `+0` code lines (no regression).

Post-phase note:

- The split-seeding pipeline is now measurable and debuggable per ROM without external scripts.
- Rom City counters show current bottleneck clearly: many candidate pairs are rejected by correlation and zero targets are accepted, enabling precise next-step tuning.

### Phase 36: Counter-Driven Split Threshold Tuning

Status: Completed (2026-02-24)

1. Tune split-seeding thresholds using Phase-35 counters.
2. Reduce correlation over-rejection while keeping bounded behavior.
3. Add extraction-reject telemetry to identify the next bottleneck precisely.

Acceptance:

1. Split thresholds are adjusted in core logic (no script/tooling changes).
2. Trace stats expose extraction rejections explicitly.
3. Full tests and Rom City verification remain green.

Implementation notes:

- Split threshold tuning in `internal/disasm/banks.go`:
  - increased correlation lookahead:
    - `correlationLookahead: 48 -> 80`
  - relaxed split extraction minimums:
    - `minRunEntries: 4 -> 2`
    - `minDistinctTargets: 3 -> 2`
  - extraction now tolerates bounded leading non-target entries:
    - `maxLeadingSkips = 8` before run start.
  - correlation variant path relaxed for indexed-store pointer-byte pairs:
    - no longer requires transfer/arithmetic signal for that specific pair type
    - strict indirect-use confirmation is still required.
- New extraction reject counter:
  - `internal/disasm/stats.go`
  - Added:
    - `splitSeedRejectedExtract`
  - Logged as:
    - `split_seed_rejected_extract`
- Instrumentation point:
  - `internal/disasm/banks.go`
  - increments `splitSeedRejectedExtract` when both split-table orientations fail extraction.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`60841` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60766` lines)
- Telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - Before (Phase 35):
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=41`
    - `split_seed_accepted_targets=0`
  - After (Phase 36):
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=38`
    - `split_seed_rejected_extract=9`
    - `split_seed_accepted_targets=0`
  - Interpretation:
    - correlation gate improved slightly (`-3` rejects),
    - surviving candidates now fail at extraction, not correlation.
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 35: `7418`
  - After Phase 36: `7418`
  - Delta: `+0` code lines (no regression).

Post-phase note:

- The active bottleneck has shifted and is now quantified: split candidates are reaching extraction, but extraction still yields zero accepted targets.
- Next gains should target split extraction acceptance rules rather than further correlation relaxation.

### Phase 37: Split Extraction Acceptance Tuning (Sparse/Short Runs)

Status: Completed (2026-02-24)

1. Tune split extraction acceptance rules to convert post-correlation candidates into real seeded targets.
2. Keep the changes bounded via sparse-run limits and existing correlation/target gates.
3. Re-measure Rom City code-density lift after extraction-focused tuning.

Acceptance:

1. Split extraction produces non-zero accepted targets on Rom City.
2. Full tests and Rom City verification remain green.
3. Output remains reassemblable for `ca65` and `asm6`.

Implementation notes:

- Extraction threshold tuning in `internal/disasm/banks.go`:
  - split seeding configuration:
    - `minRunEntries: 2 -> 1`
    - `minDistinctTargets: 2 -> 1`
  - extraction now tolerates sparse rows after run start:
    - added `maxTrailingSkips = 2`
  - correlation lookahead retained at widened value (`80`) from Phase 36.
- Shape/acceptance behavior:
  - single-target split runs can be accepted (`len(targets)==1`) when they pass all existing validity/opcode/correlation guards.
  - multi-target runs still prefer table-shape checks, with code-evidence fallback.
- Correlation variant path remains strict on indirect-use evidence, but indexed-store pair path no longer requires transfer/arithmetic signal.

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61042` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60967` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 36: `7418`
  - After Phase 37: `7542`
  - Delta: `+124` code lines.
- Telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - Before (Phase 36):
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=38`
    - `split_seed_rejected_extract=9`
    - `split_seed_accepted_targets=0`
    - `static_unique_pc=9232`
    - `parsed_offsets=10151`
    - `code_bytes_marked=15740`
  - After (Phase 37):
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=38`
    - `split_seed_rejected_extract=5`
    - `split_seed_accepted_targets=16`
    - `static_unique_pc=9387`
    - `parsed_offsets=10297`
    - `code_bytes_marked=16032`

Post-phase note:

- Extraction acceptance tuning is now generating real split-derived seeds and measurable code lift on the priority ROM.
- Remaining rejected-extract volume indicates additional gain is still available without broadening correlation criteria.

### Phase 38: Extraction Reject-Breakdown Telemetry

Status: Completed (2026-02-24)

1. Add fine-grained extraction reject counters (`invalid_target`, `opcode_gate`, `shape`) to pinpoint the dominant blocker.
2. Keep seeding behavior unchanged while adding observability.
3. Use Rom City telemetry to identify the next highest-impact tuning target.

Acceptance:

1. Trace stats include split extraction reject-breakdown fields.
2. Full tests and Rom City verification remain green.
3. Code output remains stable versus Phase 37.

Implementation notes:

- Stats model/log updates:
  - `internal/disasm/stats.go`
  - Added fields:
    - `splitSeedRejectInvalid`
    - `splitSeedRejectOpcode`
    - `splitSeedRejectShape`
  - Added log keys:
    - `split_seed_reject_invalid_target`
    - `split_seed_reject_opcode_gate`
    - `split_seed_reject_shape`
- Extraction instrumentation:
  - `internal/disasm/banks.go`
  - `extractSplitPointerTargets(...)` now increments reject counters when:
    - target pointer value is invalid/out-of-range/code-like source (`invalid_target`)
    - target opcode fails routine-start gate (`opcode_gate`)
    - final run fails shape/evidence acceptance (`shape`).

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged from Phase 37:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61042` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60967` lines)
- Telemetry sample (Rom City Rampage, asm6, hybrid profile):
  - `split_seed_candidate_pairs=47`
  - `split_seed_rejected_correlation=38`
  - `split_seed_rejected_extract=5`
  - `split_seed_reject_invalid_target=107`
  - `split_seed_reject_opcode_gate=30`
  - `split_seed_reject_shape=1`
  - `split_seed_accepted_targets=16`
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 37: `7542`
  - After Phase 38: `7542`
  - Delta: `+0` code lines (no regression).

Post-phase note:

- Reject-breakdown shows the dominant extraction blocker is `invalid_target`, not shape filtering.
- Next gains should focus on reducing invalid-pointer noise before relaxing opcode/shape gates.

### Phase 39: Plausibility Prefilter for Invalid-Target Suppression

Status: Completed (2026-02-24)

1. Add a pre-extraction split-table plausibility gate to reduce invalid-target churn before opcode reads.
2. Keep accepted split targets and Rom City code-density stable.
3. Extend telemetry with plausibility-reject visibility.

Acceptance:

1. Invalid-target reject count is reduced materially.
2. Rom City output and verification stay stable.
3. Full test suite remains green.

Implementation notes:

- New plausibility gate:
  - `internal/disasm/banks.go`
  - Added `hasSplitTableTargetWindowCoherence(...)`:
    - samples split low/high tables before extraction opcode checks
    - requires at least one plausible sampled pointer target
    - bounds page spread to reject incoherent table pairs.
- Pair gating flow update:
  - split candidates now require at least one plausible orientation (`forward` or `reverse`) before correlation/extraction.
  - added `splitSeedRejectedPlaus` counter when both orientations fail plausibility.
- Stats/log updates:
  - `internal/disasm/stats.go`
  - Added/logged:
    - `split_seed_rejected_plausibility`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged from Phase 37:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61042` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60967` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 38: `7542`
  - After Phase 39: `7542`
  - Delta: `+0` code lines (no regression).
- Telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - Before (Phase 38):
    - `split_seed_candidate_pairs=47`
    - `split_seed_rejected_correlation=38`
    - `split_seed_rejected_extract=5`
    - `split_seed_reject_invalid_target=107`
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_reject_shape=1`
    - `split_seed_accepted_targets=16`
  - After (Phase 39):
    - `split_seed_candidate_pairs=28`
    - `split_seed_rejected_correlation=24`
    - `split_seed_rejected_plausibility=19`
    - `split_seed_rejected_extract=0`
    - `split_seed_reject_invalid_target=17`
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_reject_shape=1`
    - `split_seed_accepted_targets=16`

Post-phase note:

- The plausibility gate removed most invalid-target churn while preserving accepted split seeds and Rom City code density.
- With invalid-target noise reduced, `opcode_gate` is now the dominant extraction reject class.

### Phase 40: Guarded Weak Singleton Acceptance for Opcode-Gated Seeds

Status: Completed (2026-02-24)

1. Add a guarded weak-entry tier for split-derived singleton targets that fail opcode-gate.
2. Keep the gate bounded by existing code-evidence signals to avoid broad data-to-code promotion.
3. Add explicit telemetry for weak-entry accepts.

Acceptance:

1. `split_seed_accepted_weak` is emitted in trace stats.
2. Full tests and Rom City verification remain green.
3. Rom City output remains stable (no regression in line count or code-density).

Implementation notes:

- Weak singleton acceptance:
  - `internal/disasm/banks.go`
  - `extractSplitPointerTargets(...)` now allows a singleton target through the opcode-fail path when:
    - no prior targets were accepted in the run
    - target passes `isWeakSplitEntryCandidate(...)`.
  - Accepted weak singleton seeds increment `splitSeedAcceptedWeak` and terminate the extraction run early (bounded singleton behavior).
- Weak-entry evidence gate:
  - `internal/disasm/banks.go`
  - Added `isWeakSplitEntryCandidate(target uint16) bool` requiring existing mapper evidence:
    - `CodeOffset`
    - `CallDestination`
    - `FunctionReference`
    - `JumpEngine`
- Stats/log updates:
  - `internal/disasm/stats.go`
  - Added/logged:
    - `split_seed_accepted_weak`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61042` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60967` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 39: `7542`
  - After Phase 40: `7542`
  - Delta: `+0` code lines.
- Telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - Before (Phase 39):
    - `split_seed_candidate_pairs=28`
    - `split_seed_rejected_correlation=24`
    - `split_seed_rejected_plausibility=19`
    - `split_seed_rejected_extract=0`
    - `split_seed_reject_invalid_target=17`
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_reject_shape=1`
    - `split_seed_accepted_targets=16`
  - After (Phase 40):
    - `split_seed_candidate_pairs=28`
    - `split_seed_rejected_correlation=24`
    - `split_seed_rejected_plausibility=19`
    - `split_seed_rejected_extract=0`
    - `split_seed_reject_invalid_target=17`
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_reject_shape=1`
    - `split_seed_accepted_targets=16`
    - `split_seed_accepted_weak=0`

Post-phase note:

- The weak singleton tier is now wired and measurable, but Rom City currently yields no qualifying weak candidates under strict evidence gating.
- Next gain should come from characterizing `opcode_gate` rejects by opcode class and expanding weak evidence only where telemetry supports it.

### Phase 41: Opcode-Class Reject Telemetry + RTS/RTI Adjacent-Evidence Weak Tier

Status: Completed (2026-02-24)

1. Split opcode-gate rejects into explicit classes to isolate low-risk expansion paths.
2. Add a second-stage weak-entry acceptance for opcode-gated singleton `RTS/RTI` targets when nearby code evidence exists.
3. Keep behavior bounded and measurable (no broad relaxation).

Acceptance:

1. Trace stats include opcode-class reject keys plus `split_seed_accepted_weak_rts_rti`.
2. Full tests and Rom City verification remain green.
3. Rom City output and code-density remain stable unless a qualified weak singleton is found.

Implementation notes:

- Opcode-class reject accounting:
  - `internal/disasm/stats.go`
  - Added/logged:
    - `split_seed_reject_opcode_read_error`
    - `split_seed_reject_opcode_invalid`
    - `split_seed_reject_opcode_unofficial`
    - `split_seed_reject_opcode_brk`
    - `split_seed_reject_opcode_rts`
    - `split_seed_reject_opcode_rti`
    - `split_seed_reject_opcode_other`
- Extraction flow updates:
  - `internal/disasm/banks.go`
  - `extractSplitPointerTargets(...)` now:
    - classifies read errors separately under opcode-gate rejects
    - classifies opcode-gate failures by opcode category
    - supports bounded singleton weak acceptance for `RTS/RTI` when `hasAdjacentSplitEntryEvidence(...)` is true.
- New helpers:
  - `internal/disasm/banks.go`
  - `hasAdjacentSplitEntryEvidence(target, end)` (radius=8, code-window bounded)
  - `recordSplitOpcodeGateReject(op)`
  - `isWeakSplitEntryTerminatorOpcode(op)` (`RTS`/`RTI`)
- Weak acceptance telemetry:
  - `internal/disasm/stats.go`
  - Added/logged:
    - `split_seed_accepted_weak_rts_rti`

Validation:

- Full tests:
  - `GOCACHE=/tmp/gocache_retrodisasm go test ./... -count=1`
  - Result: success.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61042` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60967` lines)
- Code-density result:
  - `rg -n '^[[:space:]]+[a-z]{3}\b' ... | wc -l`
  - After Phase 40: `7542`
  - After Phase 41: `7542`
  - Delta: `+0` code lines.
- Telemetry delta (Rom City Rampage, asm6, hybrid profile):
  - Before (Phase 40):
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_accepted_weak=0`
  - After (Phase 41):
    - `split_seed_reject_opcode_gate=30`
    - `split_seed_reject_opcode_read_error=0`
    - `split_seed_reject_opcode_invalid=11`
    - `split_seed_reject_opcode_unofficial=6`
    - `split_seed_reject_opcode_brk=0`
    - `split_seed_reject_opcode_rts=13`
    - `split_seed_reject_opcode_rti=0`
    - `split_seed_reject_opcode_other=0`
    - `split_seed_accepted_weak=0`
    - `split_seed_accepted_weak_rts_rti=0`
    - `split_seed_accepted_targets=16`
    - `split_seed_candidate_pairs=28`
    - `split_seed_rejected_correlation=24`
    - `split_seed_rejected_plausibility=19`
    - `split_seed_rejected_extract=0`
    - `split_seed_reject_invalid_target=17`
    - `split_seed_reject_shape=1`

Post-phase note:

- The dominant opcode-gate class is now confirmed as `RTS` (13/30), followed by invalid opcodes (11/30).
- The new `RTS/RTI + adjacent evidence` weak tier is active but produced no qualifying singleton in Rom City under current radius/evidence constraints.

### Phase 42: Bank-Switch Comments in Assembly Output

Status: Completed (2026-02-24)

1. Add bank-switch comments to assembly output at instructions that perform observed PRG bank switches.
2. Use emu trace `BankSwitchWrite` events (only `Changed=true`) for precise annotation — no static guessing.
3. Add mapper register description helper for human-readable register names.

Acceptance:

1. Bank-switch instructions in Rom City Rampage output show `; bank switch: MMC5 PRG bank select` comments.
2. Both `asm6` and `ca65` outputs verify successfully.
3. Full test suite and lint remain clean.

Implementation notes:

- New mapper register description method:
  - `internal/mapper/mapper.go`
  - Added `MapperRegisterDescription(address uint16) (string, bool)`:
    - Mapper 1 (MMC1): `$8000+` → "MMC1 bank select"
    - Mapper 2 (UxROM): `$8000+` → "UxROM bank select"
    - Mapper 5 (MMC5): `$5100` → "MMC5 PRG mode", `$5114-$5117` → "MMC5 PRG bank select"
    - Mapper 7 (AxROM): `$8000+` → "AxROM bank select"
- Bank-switch annotation pass:
  - `internal/disasm/emutrace.go`
  - Added `annotateBankSwitchWrites(result)`:
    - iterates `BankSwitchWrites` where `Changed == true`
    - deduplicates by `(PC, BeforeMapping)` to avoid repeat comments
    - restores correct mapping context before offset lookup
    - skips offsets that already have a comment
    - sets comment to `"bank switch: <description>"` or `"bank switch"` for unknown mappers
- Wiring in Process flow:
  - `internal/disasm/disasm.go`
  - `annotateBankSwitchWrites(emuTrace)` called after `logAdvisoryEmuTraceComparison` and before `PostProcessCode`.
- Static-only mode is unaffected: no bank-switch comments without emu trace data, avoiding noise from ambiguous register writes.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61045` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`60970` lines)
- Bank-switch comment verification:
  - `grep "bank switch" "internal/testroms/special/Rom City Rampage.ca65.asm"`
  - Result: 2 bank-switch comments found:
    - `sta a:_var_5115  ; $D79E  8D 15 51  bank switch: MMC5 PRG bank select`
    - `sta a:$5116      ; $E0A0  8D 16 51  bank switch: MMC5 PRG bank select`
  - Both are `STA` instructions writing to MMC5 PRG bank select registers (`$5115`, `$5116`).
- Code-density result:
  - `grep -cE '^\s+[a-z]{3}\b' ... | wc -l`
  - After Phase 41: `7542`
  - After Phase 42: `7542`
  - Delta: `+0` code lines (annotation-only change, no code-density impact).

Post-phase note:

- Bank-switch comments provide immediate readability improvement for reverse engineering mapper-heavy ROMs without changing disassembly behavior.
- The annotation is purely observational — it marks only writes that the emulator actually observed changing PRG mapping, not all mapper register writes.
- Output is still deterministic and reassemblable with both target assemblers.

### Phase 43: Post-Bank-Switch Loop Relaxation + Mid-Run RTS/RTI Split Acceptance

Status: Completed (2026-02-24)

1. Remove bank-switch guard from `canRelaxVisitLimitForLoop()` — the tight-loop pattern detectors are sufficient safety gates regardless of which bank is mapped. Raise the step threshold from 20,000 to `cfg.MaxInstructions/2`.
2. Accept RTS/RTI targets in established split-table runs — when a run has 2+ accepted entries, RTS/RTI stub functions are valid dispatch table entries.
3. Add `splitSeedAcceptedMidRunRT` telemetry counter.

Acceptance:

1. Both `asm6` and `ca65` outputs verify successfully for Rom City Rampage.
2. Full test suite and lint remain clean.
3. Code density improved from 7542 to 7580 (+38 lines).

Implementation notes:

- `internal/trace/m6502emu/trace.go`:
  - `canRelaxVisitLimitForLoop` now accepts `cfg Config` parameter and uses `cfg.MaxInstructions/2` threshold instead of hardcoded 20,000.
  - Removed the `BankSwitchWrites` loop guard that blocked all loop relaxation after any bank switch.
  - Threaded `cfg` through: `maybeRelaxVisitLimit` → `shouldRelaxVisitLimitForStartupLoop` / `shouldRelaxVisitLimitForPPUDataLoop` → `canRelaxVisitLimitForLoop`.
- `internal/disasm/banks.go`:
  - Added mid-run RTS/RTI acceptance in `classifySplitTarget`: when `started == true` and target opcode is RTS/RTI, accept as `splitEntryAccept`.
  - This allows dispatch tables with RTS stub entries to be fully traversed instead of being terminated prematurely.
- `internal/disasm/stats.go`:
  - Added `splitSeedAcceptedMidRunRT` field with log key `split_seed_accepted_mid_run_rts_rti`.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61102` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`61027` lines)
- Code-density result:
  - After Phase 42: `7542`
  - After Phase 43: `7580`
  - Delta: `+38` code lines.
- Telemetry:
  - `split_seed_accepted_mid_run_rts_rti`: 30 (new RTS stub entries accepted in split tables).
  - Emu trace still halts at `$E2E7` with `unique_pc=495` (unchanged — the bank-switch guard removal enables relaxation but the halt PC requires further PPU/frame progression work).

### Phase 44: Inside-Loop Body Visit Relaxation

Status: Completed (2026-02-24)

1. Add `isInsideTightBackwardBranchLoop()` — scans forward from the current PC (up to 12 bytes) for any conditional branch whose target is at or before the current PC. This detects when the current instruction is inside a tight backward-branching loop body (e.g., `BIT $2002 / BPL .-3` where the visit limit hits at BIT, not BPL).
2. Wire into `isTightCounterLoopPC()` right after the `isTightBackwardBranchLoop` check, giving `bootLoopVisitLimit` (2048) to any instruction inside a tight backward loop.
3. Add `TestRunEscapesPPUStatusPollingLoopAtNonBranchPC` — verifies the trace escapes a PPU polling loop when the visit limit fires at the non-branch instruction.

Acceptance:

1. Both `asm6` and `ca65` outputs verify successfully for Rom City Rampage.
2. Full test suite and lint remain clean.
3. Emu trace halt address advanced from `$E2E7` to `$E2F1` (trace progresses past previous stall point).

Implementation notes:

- `internal/trace/m6502emu/trace.go`:
  - Added `isInsideTightBackwardBranchLoop(mapper, pc)` that scans forward up to 12 bytes for a conditional branch opcode whose computed target is at or before `pc`, with total loop span ≤ 0x40 bytes.
  - Inserted call in `isTightCounterLoopPC` between `isTightBackwardBranchLoop` and `isNearbyCounterBranchPattern` checks.
- `internal/trace/m6502emu/trace_test.go`:
  - Added `TestRunEscapesPPUStatusPollingLoopAtNonBranchPC`: PPU polling loop (`BIT $2002` / `BPL .-3`) with visit limit hitting at the BIT instruction. Verifies the trace escapes and reaches the mapper write marker after the loop.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61102` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`61027` lines)
- Code-density result:
  - After Phase 43: `7580`
  - After Phase 44: `7580`
  - Delta: `+0` code lines (loop-relaxation-only change; the improved loop escape does not produce new split-table entries).
- Telemetry:
  - Emu trace halt moved from `$E2E7` to `$E2F1` — the inside-loop body detector successfully relaxed the visit limit at the non-branch instruction, allowing the trace to escape and reach further code.
  - `unique_pc=495` (unchanged — the newly reachable PCs between `$E2E7` and `$E2F1` were already visited via other paths).

### Phase 45: PPU Status Polling Loop Detection

Status: Completed (2026-02-24)

1. Add `isPPUStatusPollingLoopPC()` — detects `BIT/LDA $2002 + conditional backward branch` patterns, the classic NES vblank-wait idiom.
2. Wire into `isPPUDataStreamLoopPC()` so PPU status polling loops get `ppuLoopVisitLimit` (8192) instead of `bootLoopVisitLimit` (2048).
3. Add `TestRunEscapesPPUStatusPollingLoopWithHighVisitBudget` — verifies the trace escapes a PPU polling loop even when `MaxVisitsPerPC >= bootLoopVisitLimit`, proving the higher PPU limit fires.

Acceptance:

1. Both `asm6` and `ca65` outputs verify successfully for Rom City Rampage.
2. Full test suite and lint remain clean.
3. Emu trace instructions increased from 288,831 to 292,138 (+3,307 — the higher PPU limit is active at some hot sites).

Implementation notes:

- `internal/trace/m6502emu/trace.go`:
  - Added `isPPUStatusPollingLoopPC(mapper, pc)` that scans backward up to 6 bytes for `BIT/LDA $2002` (opcodes `$2C/$AD`) followed by a conditional backward branch.
  - Added `isPPUStatusPollingPatternAt(mapper, start)` helper that validates the full pattern: read opcode + $2002 address + branch opcode + backward target.
  - Inserted call at top of `isPPUDataStreamLoopPC`, checked before the existing `STA $2007` stream-loop detector.
- `internal/trace/m6502emu/trace_test.go`:
  - Added `TestRunEscapesPPUStatusPollingLoopWithHighVisitBudget`: uses `MaxVisitsPerPC=2048` (equal to `bootLoopVisitLimit`), proving only `ppuLoopVisitLimit=8192` allows escape.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output:
    - `internal/testroms/special/Rom City Rampage.asm6.asm` (`61102` lines)
    - `internal/testroms/special/Rom City Rampage.ca65.asm` (`61027` lines)
- Code-density result:
  - After Phase 44: `7580`
  - After Phase 45: `7580`
  - Delta: `+0` code lines (PPU-limit-only change; PPU polling sites don't produce new split-table entries).
- Telemetry:
  - Emu trace instructions: 292,138 (up from 288,831) — the PPU polling detector grants the higher limit at legitimate vblank-wait sites.
  - Halt still at `$E2F1` — investigation reveals this address contains `$E2 $19` (unofficial NOP #imm), indicating the trace is executing through **data**, not stuck in a PPU loop. The visit limit correctly halts data-execution paths.
  - `unique_pc=495`, `emu_only_pc=0` (unchanged — the trace hasn't broken through to bank-switched code that static analysis can't reach).

Analysis: The halt at `$E2F1` is a data-execution issue, not a loop detection gap. The address range `$E2E0-$E2FF` contains repeated `$E2 $19` data bytes that the CPU interprets as unofficial 2-byte NOPs. The visit limit correctly stops this path. Future trace quality improvements should focus on detecting when the trace enters data regions (e.g., unofficial opcode sequences) and aborting early rather than consuming visit budget.

### Phase 46: Unofficial Opcode Trap

Status: Completed (2026-02-24)

1. Add `isUnofficialOpcode()` check in the main trace loop — before the visit limit check, read the opcode at the current PC. If it's an unofficial/undefined 6502 opcode, treat the path as a dead-end and restore the next branch state.
2. Add `TestRunBailsOnUnofficialOpcodeAndContinuesAlternatePath` — verifies the trace skips unofficial opcodes and successfully follows alternate branch paths to reach valid code.

Acceptance:

1. Both `asm6` and `ca65` outputs verify successfully for Rom City Rampage.
2. Full test suite and lint remain clean.
3. Unofficial opcode trap correctly prevents data-region execution (verified by unit test).

Implementation notes:

- `internal/trace/m6502emu/trace.go`:
  - Added `isUnofficialOpcode(op byte) bool` that checks `cpu6502.Opcodes[op]` for `Instruction == nil || Instruction.Unofficial`.
  - Inserted check in `runTraceLoop` after `CheckInterrupts()` and before `checkAndHandleVisitLimit()`. When the trap fires, it sets `lastHaltReason` and calls `restoreNextState` to continue with the next queued branch state.
- `internal/trace/m6502emu/trace_test.go`:
  - Added `TestRunBailsOnUnofficialOpcodeAndContinuesAlternatePath`: primary path contains unofficial `$E2 $19` opcodes; alternate branch path leads to a mapper write marker. Test verifies the trace reaches the mapper write AND never visits the unofficial opcode PCs.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged (`61027` / `61102` lines).
- Code-density result:
  - After Phase 45: `7580`
  - After Phase 46: `7580`
  - Delta: `+0` code lines.
- Telemetry:
  - Metrics unchanged for Rom City Rampage — the halt at `$E2F1` occurs in a **non-default bank mapping** where that CPU address maps to different (official) physical bytes. The unofficial data at `$E2F1` in the default mapping is not what the trace encounters.
  - The trap is a safety improvement that prevents data execution in traces that drift into regions containing unofficial opcodes. Its impact will be visible on ROMs where the trace actually reaches unofficial opcodes in the active bank mapping.

Analysis: The unchanged metrics reveal a key insight about the emu trace's behavior. The halt at `$E2F1` is NOT the trace executing unofficial opcodes — it's executing **official code in a different bank mapping** at the same CPU address. The `unique_mapping=3` confirms the trace operates under multiple mappings. The unofficial opcodes visible at `$E2F1` in the disassembly output are from the **default mapping** (which the static pass disassembles), while the emu trace executes a **switched mapping** at that address. This explains why the trace discovers `emu_only_pc=0` — it's visiting the same CPU addresses as static analysis, just under different bank mappings. The emu trace's value is in mapper-write discovery (6,157 writes, 1,032 mapping changes) and branch-alternate seeding (8,343 states), not in discovering new CPU addresses.

### Phase 47: Stack Dispatch Correlation Signal

Status: Completed (2026-02-24)

1. Add PHA+PHA+RTS stack dispatch detection as a split-table correlation signal. This is a common NES dispatch pattern: `LDA tbl_lo,X / PHA / LDA tbl_hi,X / PHA / RTS` builds the target address on the stack and returns to it. No ZP stores or indirect references are needed.
2. Track `phaCount` and `stackDispatch` in `correlationState`. The signal fires when 2+ PHA instructions precede an RTS in the correlation window.
3. Add `splitSeedAcceptedStackDisp` telemetry counter.

Acceptance:

1. Both `asm6` and `ca65` outputs verify successfully for Rom City Rampage.
2. Full test suite and lint remain clean.
3. Stack dispatch signal recovered 2 pairs from correlation rejection; downstream opcode/shape gates correctly identified them as data, confirming the multi-stage pipeline's safety.

Implementation notes:

- `internal/disasm/banks.go`:
  - Added `phaCount int` and `stackDispatch bool` to `correlationState`.
  - Extended `collectCorrelationOp` to track PHA (0x48) count and detect RTS (0x60) following 2+ PHAs.
  - Added stack dispatch as the first check in `checkCorrelationSignals`, before the store-based checks.
  - Added `splitSeedAcceptedStackDisp` increment in `hasSplitPointerRuntimeCorrelation` when stack dispatch is the accepting signal.
- `internal/disasm/stats.go`:
  - Added `splitSeedAcceptedStackDisp` field with log key `split_seed_accepted_stack_dispatch`.

Validation:

- Full tests:
  - `go test ./...`
  - Result: success.
- Lint:
  - `make lint`
  - Result: `0 issues`.
- Rom City Rampage verify/regeneration:
  - `bash scripts/verify_rom_city_rampage.sh`
  - Result: success for both `ca65` and `asm6`.
  - Output unchanged (`61027` / `61102` lines).
- Code-density result:
  - After Phase 46: `7580`
  - After Phase 47: `7580`
  - Delta: `+0` code lines.
- Telemetry:
  - `split_seed_rejected_correlation`: 24 → 22 (-2 pairs recovered by stack dispatch signal).
  - `split_seed_accepted_stack_dispatch`: 2 (new pairs passing correlation).
  - `split_seed_rejected_extract`: 0 → 1 (one recovered pair failed extraction).
  - `split_seed_reject_invalid_target`: 25 → 32 (+7 new entries checked from recovered pairs).
  - `split_seed_reject_opcode_gate`: 36 → 41 (+5 opcode rejects from new entries).
  - `split_seed_reject_shape`: 0 → 2 (+2 shape rejects from new entries).
  - `split_seed_accepted_targets`: 37 (unchanged — recovered pairs contained data, not genuine dispatch tables).

Analysis: The stack dispatch signal correctly expanded correlation coverage — 2 pairs that were previously rejected now pass. However, the downstream gates (opcode, shape, invalid target) correctly identified these pairs' target entries as data. This demonstrates the multi-stage pipeline's defense-in-depth: the correlation check can be safely broadened because downstream gates catch false positives. The remaining 22 correlation rejections are pairs without any recognizable dispatch mechanism in their vicinity.

### Phase 48: Post-Bank-Switch Frontier Prioritization

**Goal:** Increase the chance that branch alternates discover code reachable only through bank-switched mappings by prioritizing post-bank-switch alternates in the frontier queue.

**Background:** Phase 47 analysis showed `emu_only_pc=0` — the emulator trace discovers no PCs that static analysis misses. All 8,343 branch alternates explored code under the same initial mapping. Branch alternates created after mapper writes (different `MappingSignature`) are more likely to reach bank-switched code paths.

An initial attempt at per-state visit budget isolation was explored and abandoned. Per-state isolation — giving each branch alternate a fresh visit counter — was counterproductive because it removed the "fast-fail" mechanism: in the original shared counter, hot PCs (already visited 1024+ times by the primary trace) immediately block alternates, forcing them to quickly yield and let other alternates run. With per-state isolation, alternates re-entered boot loops at full budget, wasting instruction budget and resulting in fewer alternates explored (4678 vs 8343) and fewer unique PCs (142 vs 495).

**Implementation:**

1. Track `initialMapping` in `traceState` — captured at the start of `runTraceLoop`.
2. Split the frontier into two queues: `priorityFrontier` (post-bank-switch alternates) and `frontier` (normal alternates).
3. In `recordBranchAlternate`: if `signatureBefore != ts.initialMapping`, append to `priorityFrontier`; otherwise append to `frontier`.
4. In `restoreNextState`: pop from `priorityFrontier` first (FIFO), then from `frontier`.
5. Budget check uses `ts.frontierSize()` (sum of both queues).

**Test:** `TestRunPostBankSwitchAlternatesPrioritized` — creates a sequence with a pre-switch BEQ alternate and a post-switch BEQ alternate, verifies the post-switch alternate is explored first in the trace steps.

**Validation:**

- `go test ./...` — all pass.
- `make lint` — 0 issues.
- `bash scripts/verify_rom_city_rampage.sh` — both assemblers pass.
- Code-density result:
  - After Phase 47: `7580`
  - After Phase 48: `7580`
  - Delta: `+0` code lines.
- Emu trace metrics (comparison):
  - `unique_pc`: 495 → 495 (no regression)
  - `emu_only_pc`: 0 → 0 (no improvement on emu-only discovery for this ROM)
  - `instructions`: 292,138 → 280,812 (-4% more efficient)
  - `branch_alternates`: 8,343 → 9,104 (+9% more alternates explored)
  - `branch_alternate_budget_drops`: 9,718 → 6,141 (-37% fewer drops)
  - `code_bytes_marked`: 15,863 → 16,106 (+243 more code bytes discovered)
  - `parsed_offsets`: 9,984 → 10,349 (+365 more offsets parsed)
  - `elapsed` (emu trace only): 617ms → 791ms (+28%)
  - `halt_reason`: `$E2F1` → `$C24B` (different halt site — trace takes different paths)
- Output unchanged: ca65 61,027 lines, asm6 61,102 lines.

**Analysis:** The frontier prioritization successfully explores more branch alternates (+9%) with fewer budget drops (-37%) by giving post-bank-switch alternates first access. The +243 code bytes marked indicates the prioritization helps downstream static analysis discover additional code (via more diverse trace coverage influencing queue seeding). The `emu_only_pc=0` result confirms that for Rom City Rampage specifically, all emu-discovered code is redundant with static analysis — the split-table pipeline already finds these PCs. The value of frontier prioritization will be greater for ROMs where bank-switched code isn't reachable via static pointer tables.

## Progress Summary (as of 2026-02-24)

### Completed Phases

| Phase | Title | Key Outcome |
|-------|-------|-------------|
| 0 | Instrumentation and Baseline | Stats, benchmark scripts, baseline metrics |
| 0.5 | Multi-Bank Vector Tracing | `MapBank`, `RestoreDefaultMapping`, `BankVectors`, `InitializeBankVectors` |
| 1 | Emulator Trace Prototype | `internal/trace/m6502emu` with NES bus, advisory mode |
| 2 | Bank-Aware Parse Keys | `ParseKey{PC, MappingID}`, mapping-aware dedupe |
| 3 | Mapper Runtime Integration | `ApplyMapperWrite`, `RestoreMappingSignature`, mapper 7/2/1 |
| 4 | Symbol and Output Stabilization | `uniqueLabelName`, alias dedupe, missing-symbol fallback |
| 5 | Controlled Branch Expansion | `BranchAlternate`, bounded alternate-path frontier |
| 6 | CLI Trace Controls | `-trace-mode`, `-trace-max-*` flags with env fallback |
| 7 | Benchmark Harness Hardening | Sweep script, corrected pass/fail classification |
| 8 | True Alternate-Path Execution | CPU/RAM/mapper state snapshot+restore, real replay |
| 9 | Failure Artifact Capture | Per-ROM artifact bundles in benchmark harnesses |
| 10 | Artifact Clustering | Mapper-grouped mismatch-offset and label summaries |
| 11 | Large-Matrix Sweep | Stability-filtered clusters, budget-invariant results |
| 12 | Multi-Bank Vector Placement Fix | ca65 bank-tail vector emission, mapper 1: 2/2, mapper 2: 4/7 |
| 13 | Branch-Expansion Regression Guard | Mapper-1 branch clamp, stable across budget matrix |
| 14 | Assembler Failure Forensics | Assembler error context extraction in artifacts |
| 15 | Unresolved Relative-Branch Hardening | Raw-byte rewrite for unresolved branch targets, mapper 2: 5/7 |
| 16 | Alfred Flow Instrumentation | Mapper-write hotspot aggregation telemetry |
| 17 | Startup Loop Escape Realism | PPU status alternation, counter-loop relaxation |
| 18 | JOYPAD Serial Stub Realism | Strobe/latch/shift controller emulation |
| 19 | PPU Data-Loop Progression Hook | `STA $2007` stream-loop visit relaxation |
| 20 | Rom City Rampage Hybrid Stabilization | Two-pass hybrid, long indexed clear loops, `code=973` |
| 21 | Rom City Bank-Context Expansion | Same-address vector seeding, raised budgets, `code=1075` |
| 22 | Deterministic JOYPAD Input Controls | `-trace-joypad1/2` static masks |
| 23 | JOYPAD Timeline Scripting | `-trace-joypad1-seq/-joypad2-seq` per-latch sequences |
| 24 | JOYPAD Timeline Preset Sweep | Rom City Rampage preset sweep automation |
| 25 | Mapper-Triage JOYPAD Sweep | Mapper-2 notworking sweep with telemetry capture |
| 26 | Benchmark Failure-Class Classifier | `failure_class` in baseline and sweep CSVs |
| 27 | Classifier-Aware Clustering | Class-grouped cluster outputs in markdown/CSV |
| 28 | Core Code-Density Lift | Bank-entry seeding + synthetic NMI, `code=1191` |
| 29 | Mapped-Bank Call-Target Expansion | Official opcode gate, `code=4764` |
| 30 | Pointer-Table Target Seeding | Shape-guarded contiguous pointer-table pass, `code=7418` |
| 31 | Mapper-1 Branch Policy | Clamp-not-disable, tiered budget caps |
| 32 | Split Pointer-Table Seeding | `tbl_lo`/`tbl_hi` framework (stable, `code=7418`) |
| 33 | Split-Table Correlation Tuning | Index-mode pairing + runtime-use guard |
| 34 | Transfer/Arithmetic Correlation | Broader pointer-build pattern coverage |
| 35 | Split-Seeding Telemetry | Candidate/reject/accept counters |
| 36 | Counter-Driven Threshold Tuning | Reduced correlation over-rejection |
| 37 | Split Extraction Acceptance | Sparse/short runs, `code=7542` (+124) |
| 38 | Extraction Reject-Breakdown | `invalid_target`/`opcode_gate`/`shape` counters |
| 39 | Plausibility Prefilter | Target-window coherence gate |
| 40 | Weak Singleton Acceptance | Code-evidence-gated weak entry tier |
| 41 | Opcode-Class Reject Telemetry | RTS/RTI adjacent-evidence weak tier |
| 42 | Bank-Switch Comments | `MapperRegisterDescription`, emu-trace-driven annotation |
| 43 | Loop Relaxation + Mid-Run RTS | Remove bank-switch guard, mid-run RTS/RTI split acceptance, `code=7580` |
| 44 | Inside-Loop Body Relaxation | Non-branch instruction loop-body detection, trace halt advanced `$E2E7`→`$E2F1` |
| 45 | PPU Status Polling Detection | `BIT/LDA $2002` loop detection with `ppuLoopVisitLimit=8192`, +3,307 trace instructions |
| 46 | Unofficial Opcode Trap | Skip paths executing unofficial/undefined opcodes, prevent data-region budget waste |
| 47 | Stack Dispatch Correlation | PHA+PHA+RTS correlation signal, recovered 2 pairs (downstream gates confirmed as data) |
| 48 | Post-Bank-Switch Frontier Prioritization | Dual-queue frontier: post-switch alternates explored first, +9% alternates, -37% drops, +243 code bytes |

### Current Metrics

| Metric | Value |
|--------|-------|
| Mapper 0 pass rate | 41/41 |
| Mapper 3 pass rate | 6/6 |
| Mapper 7 pass rate | 1/1 |
| Mapper 1 pass rate (notworking) | 2/2 |
| Mapper 2 pass rate (notworking) | 5/7 |
| Rom City Rampage code lines | 7580 |
| Rom City Rampage asm6 lines | 61102 |
| Rom City Rampage ca65 lines | 61027 |
| Rom City Rampage bank-switch comments | 2 |
| Go test suite | all pass |
| Build status | clean |

### Remaining Failure Classes

| ROM | Mapper | Failure Class | Notes |
|-----|--------|---------------|-------|
| Alfred Chicken (USA) | 2 | `prg_mismatch` | 24061 offset mismatches; trace halts at `$C93F` PPU loop; `unique_pc=111`, `mapper_writes=4` |
| Archon (USA) | 2 | `input_corrupt` | Unexpected EOF on PRG load; likely truncated/corrupt ROM dump |

### Key Architecture Decisions Made

1. **Hybrid two-pass model** (Phase 20): Static pass first, then emulator-seeded additive pass prevents emu queue from suppressing static discovery.
2. **Advisory trace** (Phase 1+): Emulator trace is informational; disassembly decisions remain independent, preventing cascade from emu-path errors.
3. **Mapper-1 branch clamp** (Phase 31): Tiered budget caps (512/128/64) based on instruction/visit budgets; pragmatic guard until mapper-1-safe heuristics are available.
4. **Split-table pipeline** (Phases 32-41, 43, 47): Multi-stage gated pipeline: plausibility → correlation → extraction → opcode gate → shape → weak-entry tiers → mid-run RTS/RTI acceptance → stack dispatch correlation.
5. **Deterministic I/O stubs** (Phases 17-19, 43-46): PPU status alternation, controller serial emulation, PPU data-loop relaxation; all snapshot-safe for branch replay. Phase 43 removed the bank-switch guard that blocked loop relaxation for mapper-heavy ROMs. Phase 44 added inside-loop body detection so non-branch instructions within tight loops also get relaxed visit limits. Phase 45 added PPU status polling (`BIT/LDA $2002`) detection for the higher `ppuLoopVisitLimit`. Phase 46 added unofficial opcode trap to bail out of data-execution paths.
6. **Frontier prioritization** (Phase 48): Dual-queue frontier where post-bank-switch alternates are explored before initial-mapping alternates. Per-state visit budget isolation was investigated and abandoned because the shared counter's "fast-fail" at hot PCs is a beneficial side-effect that keeps alternates efficiently yielding.
6. **Non-default mapping label safety** (Phase 20): Branch/call operands in non-default mapping contexts keep literal targets to prevent cross-mapping address drift.
7. **Emu-trace-driven bank-switch comments** (Phase 42): Only annotates writes that actually changed PRG mapping during emulation, avoiding noise from CHR/misc register writes. Static-only mode gets no bank-switch comments.

### Files Added/Modified on Branch

New packages:
- `internal/trace/m6502emu/` — Advisory emulator trace engine (trace.go, bus.go, trace_test.go)
- `internal/mapper/runtime.go` — Mapper runtime write emulation + snapshot/restore
- `internal/mapper/processor.go` — Multi-bank symbol alias + relative-branch rewrite
- `internal/disasm/banks.go` — Additional bank tracing + pointer-table seeding pipeline
- `internal/disasm/emutrace.go` — Advisory emu-trace integration + config parsing + bank-switch annotation
- `internal/disasm/parsekey.go` — `ParseKey{PC, MappingID}` type
- `internal/disasm/stats.go` — Trace statistics model

Modified core:
- `internal/disasm/disasm.go` — Hybrid two-pass flow, architecture interface extension
- `internal/disasm/parser.go` — Mapping-aware dedupe and per-key state restore
- `internal/disasm/code.go` — `uniqueLabelName`, non-default mapping label safety
- `internal/disasm/data.go` — ParseKey threading
- `internal/mapper/mapper.go` — `MapBank`, `RestoreDefaultMapping`, `BankCount`, `BankVectors`, `MappingSignature`, `ResolveAddress`, `MapperRegisterDescription`
- `internal/mapper/cdl.go` — Multi-bank CDL handling
- `internal/assembler/ca65/file.go` — Bank-tail vector placement
- `internal/writer/writer.go` — Alias dedupe
- `internal/options/options.go` — Trace CLI options
- `internal/cli/cli.go` — Trace flag parsing/validation
- `internal/arch/m6502/vectors.go` — `InitializeBankVectors`
- `internal/arch/chip8/chip8.go` — No-op `InitializeBankVectors`
- `main.go` — Exit code fix for multi-file failures

Scripts:
- `scripts/benchmark_mapper_corpus.sh` — Baseline benchmark with artifact capture + failure classification
- `scripts/benchmark_trace_sweep.sh` — Budget-matrix sweep with artifact capture
- `scripts/cluster_failure_artifacts.sh` — Failure artifact clustering + stability extraction
- `scripts/sweep_mapper_joypad_timeline.sh` — Mapper-triage JOYPAD sweep
- `scripts/sweep_rom_city_rampage_joypad_timeline.sh` — Rom City JOYPAD preset sweep
- `scripts/verify_rom_city_rampage.sh` — Quick verify script

## Testing Plan

1. Unit tests
   - Mapper register write semantics per mapper (7/2/1).
   - Snapshot hash/apply correctness (CPU, RAM, mapper runtime, MMC1 shift state).
   - Parse key dedupe behavior (same PC across mappings).
   - Split-table extraction with plausibility/correlation/opcode gates.
   - JOYPAD serial protocol and sequence advancement.
   - PPU status alternation and snapshot determinism.

2. Integration tests
   - Existing `internal/disasm` and `internal/mapper` suites.
   - Advisory emu-trace config parsing and env fallback.
   - Multi-bank vector tracing with same-address vector seeding.
   - Hybrid two-pass ordering (static first, emu additive).
   - Comparison tests: `static` vs `emu` vs `hybrid` trace mode.

3. End-to-end verification
   - Run `-verify` on existing working ROM corpus (mapper 0/3/7).
   - Track mapper-specific pass rate progression (mapper 1/2 notworking).
   - Rom City Rampage dual-assembler verification.
   - Benchmark sweep scripts for regression detection.

## Risks and Mitigations

1. State explosion from branch/path forking.
   - Mitigation: strict budgets + deterministic primary path default.
   - Status: mitigated via `MaxBranchStates`, per-PC visit limits, mapper-1 tiered clamp.

2. Performance regressions.
   - Mitigation: opt-in mode first (`-trace-mode`), profile hot paths, cache snapshot transitions.
   - Status: hybrid mode is opt-in; default remains `static`.

3. Symbol instability/output diffs.
   - Mitigation: deterministic naming rules, compatibility mode for existing output style.
   - Status: resolved via `uniqueLabelName`, alias dedupe, non-default mapping label safety.

4. Incorrect mapper write emulation.
   - Mitigation: mapper-specific unit tests and ROM-based regression tests.
   - Status: mapper 7/2/1/5 covered by unit tests and corpus sweeps.

5. I/O-dependent infinite loops.
   - Stub values may not satisfy wait conditions (e.g., polling $2002 for specific PPU state).
   - Mitigation: per-PC visit limit, boot-loop relaxation, PPU data-loop relaxation.
   - Status: materially improved via Phases 17-19, 44-45, 48. Current halt at `$C24B` (after Phase 48 frontier prioritization changed exploration order). The trace explores more diverse paths via post-bank-switch alternate prioritization.

6. Mapper state divergence.
   - Stubs cause different code paths than real hardware, potentially missing or mis-tracing branches.
   - Mitigation: trace is advisory; static heuristics remain as fallback. Compare coverage metrics.
   - Status: advisory model proven stable across 41 phases.

7. Split-table false-positive code promotion.
   - Aggressive pointer-table seeding can promote data as code, causing verification failures.
   - Mitigation: multi-stage gated pipeline (plausibility → correlation → extraction → opcode → shape → weak tiers → mid-run RTS/RTI).
   - Status: zero false-positive regressions through Phase 48. Phase 47's stack dispatch signal validated the pipeline's defense-in-depth: 2 new pairs passed correlation but downstream gates correctly blocked data entries. Phase 48's frontier prioritization caused no output changes.

## Glossary

- **Mapping Snapshot**: A copy of the `mapped` slice (8 entries) representing which physical 8KB bank is assigned to each CPU address window at a point in time.
- **Mapping Signature/ID**: A hash or unique identifier derived from a mapping snapshot, used for deduplication and parse-keying.
- **Parse Key**: `(PC, MappingID)` tuple that uniquely identifies an instruction in context. Replaces plain `uint16` PC for multi-bank awareness.
- **Bank Window**: An 8KB region of CPU address space ($8000, $A000, $C000, $E000 for PRG). The mapper assigns a physical bank to each window.
- **Physical PRG Offset**: The byte position within the ROM's PRG data. Computed from the bank's `dataStart` + address offset within the 8KB window.
- **Advisory Trace**: Emulator trace output that informs but does not directly control disassembly decisions. Disasm pass consumes trace states as additive seed sources.
- **Hybrid Mode**: Two-pass disassembly where static analysis runs first, then emulator-discovered states are seeded additively.
- **Split Pointer Table**: NES pattern where jump/callback targets are stored as separate low-byte and high-byte arrays, reconstructed at runtime via indexed loads.

## Immediate Next Steps

### High Priority — Emu Trace Quality

Current bottleneck: the emu trace discovers 495 unique PCs but **0 emu-only PCs** — it visits only code that static analysis already finds. Phase 48 added frontier prioritization (post-bank-switch alternates explored first), resulting in +9% more alternates and +243 code bytes, but `emu_only_pc` remains 0 for this ROM because the split-table pipeline already discovers all bank-switched code statically.

1. ~~**Branch state visit budget isolation**~~ — Investigated in Phase 48 and abandoned. Per-state isolation removed the "fast-fail" mechanism at hot PCs, causing alternates to re-enter boot loops at full budget. Result: fewer alternates explored (4678 vs 8343) and fewer unique PCs (142 vs 495). The shared visit counter's saturation at hot PCs is actually beneficial — it forces alternates to quickly yield and let other alternates run.
2. ~~**Post-bank-switch frontier prioritization**~~ — Implemented in Phase 48. Dual-queue frontier (`priorityFrontier` + `frontier`) explores post-switch alternates first. Metrics: +9% alternates, -37% drops, +243 code bytes. No emu_only_pc improvement for Rom City Rampage since static analysis already covers those paths.
3. **Unofficial opcode trap** — ~~Implemented in Phase 46.~~ The trap correctly skips data-region paths. For Rom City Rampage the trap has no metric impact because the halt site contains official code in the active bank mapping.
4. **Frontier diversity scoring** — Current alternates from different branch points but the same mapping largely explore the same code. Consider deduplicating frontier entries by PC range (e.g., merge alternates whose starting PCs are within 32 bytes under the same mapping) to increase path diversity.

### High Priority — Split-Table Pipeline

Current state (post-Phase 47): 28 candidate pairs, 22 rejected by correlation (79%), 37+30 accepted targets. Phase 47's PHA+RTS signal recovered 2 pairs from correlation, but their entries were correctly blocked by downstream gates — confirming the pipeline's defense-in-depth. The remaining 22 correlation rejections are genuine false candidates without dispatch mechanisms.

4. **Correlation relaxation for high-coherence pairs** — The 22 remaining correlation rejections are the largest pool, but Phase 47's experiment showed that broadening correlation doesn't necessarily yield new code — downstream gates correctly caught the 2 recovered pairs as data. Further relaxation should focus on pairs with unusually high plausibility (10+ valid sampled targets) rather than pattern-based signals.
5. **Distance-aware adjacent evidence telemetry** — Emit minimum delta from split target to nearest code evidence to data-drive the current `radius=8` guard. This enables evidence-based radius tuning.
6. **Remaining RTS singleton rescue** — 3 RTS rejects remain from table entries. Consider accepting RTS targets in tables where the table base address is referenced by at least 2 split-load instructions (higher upstream confidence).

### Medium Priority — Remaining Failure Triage

7. **Alfred Chicken PPU/frame-model experiments** — Current trace halts at `$C93F` (PPU update loop) with `unique_pc=111` and `mapper_writes=4`. Phase 45's PPU status polling detector may help; needs testing. Progress may also require synthetic frame-progression hooks.
8. **Alfred Chicken experiment matrix** — Compact `max_visits` × PPU-stub variant matrix fed through class-aware clustering to identify which changes shift failure class or hotspot region.
9. **Archon input integrity** — Confirm whether `Archon (USA).nes` is a genuinely truncated/corrupt dump or requires loader-side tolerance for non-standard PRG sizes.

### Lower Priority — Policy Refinement

10. **Mapper-1 clamp threshold tuning** — Use real benchmark telemetry (coverage deltas, runtime impact) to relax tiered caps where data shows stability. Current caps (512/128/64) may be unnecessarily conservative for some visit/instruction profiles.
11. **Default trace-mode evaluation** — Once coverage and stability are proven across the full corpus, evaluate promoting `hybrid` as default trace mode (currently `static`).
12. **Mapper 4 (MMC3) support** — The next most common mapper after 0/1/2/3/7. Requires IRQ counter emulation for scanline-based bank switching. Design should follow the established `ApplyMapperWrite` + runtime snapshot pattern.

### Optimization and Cleanup

13. **Split-seeding pipeline consolidation** — Phases 32-41 added incremental tuning knobs and telemetry counters. Consider consolidating threshold constants into a configurable struct for easier experimentation.
14. **Benchmark script deduplication** — `benchmark_mapper_corpus.sh`, `benchmark_trace_sweep.sh`, and `sweep_mapper_joypad_timeline.sh` share significant verify/classify logic. Extract common helpers to reduce maintenance burden.
15. **Trace stats field rationalization** — Some split-seed telemetry fields (40+) may be better grouped or conditionally emitted to reduce log noise for non-split-seeding use cases.
