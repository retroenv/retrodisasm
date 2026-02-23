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
- **Mapper 2 (UxROM)**: 16KB switchable bank at $8000-$BFFF, $C000-$FFFF fixed to last bank
- **Mapper 7 (AxROM)**: 32KB switchable bank at $8000-$FFFF
- **Mapper 1 (MMC1)**: 16KB or 32KB switchable via serial register

**No PRG switching (mapper 0 equivalent for disassembly):**
- **Mapper 0 (NROM)**: Fixed PRG, no bank switching
- **Mapper 3 (CNROM)**: Switches CHR banks only; PRG is fixed — functionally identical to mapper 0 for disassembly

### Mapper Register Specifications

- **Mapper 0**: No writes (fixed)
- **Mapper 2**: $8000-$FFFF write → low bits select 16KB bank at $8000-$BFFF; $C000-$FFFF fixed to last bank
- **Mapper 3**: $8000-$FFFF write → CHR bank select only (ignore for PRG disassembly)
- **Mapper 7**: $8000-$FFFF write → bits 0-2 select 32KB PRG bank; bit 4 = VRAM mirror (irrelevant for disassembly)
- **Mapper 1 (MMC1)**: Serial 5-bit shift register at $8000-$FFFF; bit 7 resets; address bits 14-13 select target register (control, CHR0, CHR1, PRG)

### Internal 8KB Windowing

The `Mapper` struct uses a fixed `bankWindowSize` of `0x2000` (8KB) with 8 slots covering the full 64KB address space. Runtime mapper writes must update the correct number of 8KB slots:

- **Mapper 7** (32KB switch): update all 4 PRG slots ($8000, $A000, $C000, $E000)
- **Mapper 2** (16KB at $8000-$BFFF): update 2 slots ($8000, $A000); $C000/$E000 fixed to last bank
- **Mapper 1** (variable): depends on PRG mode register (16KB or 32KB switching)

### Working Corpus

- Working corpus: mapper `0` (41 ROMs), `3` (6 ROMs), `7` (1 ROM)
- Not-working corpus: mapper `1` (2 ROMs), `2` (7 ROMs)

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

- `$2002` (PPUSTATUS): Return `0x80` (VBlank set) — satisfies common `waitVBlank` loops
- `$4016/$4017` (controllers): Return 0 (no input)
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

## Testing Plan

1. Unit tests
   - Mapper register write semantics per mapper.
   - Snapshot hash/apply correctness.
   - Parse key dedupe behavior.

2. Integration tests
   - Existing `internal/disasm` and `internal/mapper` suites.
   - New ROM fixtures targeting deliberate bank switches.
   - Comparison tests: `static` vs `emu` trace mode.

3. End-to-end verification
   - Run `-verify` on existing working ROM corpus.
   - Track mapper-specific pass rate progression.

## Risks and Mitigations

1. State explosion from branch/path forking.
   - Mitigation: strict budgets + deterministic primary path default.

2. Performance regressions.
   - Mitigation: opt-in mode first, profile hot paths, cache snapshot transitions.

3. Symbol instability/output diffs.
   - Mitigation: deterministic naming rules, compatibility mode for existing output style.

4. Incorrect mapper write emulation.
   - Mitigation: mapper-specific unit tests and ROM-based regression tests.

5. I/O-dependent infinite loops.
   - Stub values may not satisfy wait conditions (e.g., polling $2002 for specific PPU state).
   - Mitigation: per-PC visit limit (e.g., 8 visits) terminates stuck paths.

6. Mapper state divergence.
   - Stubs cause different code paths than real hardware, potentially missing or mis-tracing branches.
   - Mitigation: trace is advisory; static heuristics remain as fallback. Compare coverage metrics.

## Glossary

- **Mapping Snapshot**: A copy of the `mapped` slice (8 entries) representing which physical 8KB bank is assigned to each CPU address window at a point in time.
- **Mapping Signature/ID**: A hash or unique identifier derived from a mapping snapshot, used for deduplication and parse-keying.
- **Parse Key**: `(PC, MappingID)` tuple that uniquely identifies an instruction in context. Replaces plain `uint16` PC for multi-bank awareness.
- **Bank Window**: An 8KB region of CPU address space ($8000, $A000, $C000, $E000 for PRG). The mapper assigns a physical bank to each window.
- **Physical PRG Offset**: The byte position within the ROM's PRG data. Computed from the bank's `dataStart` + address offset within the 8KB window.

## Immediate Next Steps

1. Implement true alternate-path execution (CPU+RAM+mapper state cloning) for selected high-value branch frontiers.
2. Run larger sweep matrices (`max_visits` and `max_branch`) for mapper 1/2 and compare discovery vs runtime from Phase 7 baselines.
3. Add per-ROM failure artifact capture (first mismatch offsets + emitted labels) to speed mapper-specific debugging.
