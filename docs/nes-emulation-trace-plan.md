# NES Emulator-Assisted Trace Plan

Date: 2026-02-23

## Goal

Improve NES disassembly accuracy by simulating CPU execution and mapper state transitions so code discovery works across bank switches, not only within the default static mapping.

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
   - Adds runtime bank-switch operations and immutable snapshot export.
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

## Mapper Emulation Scope

Implement incrementally by mapper priority already present in repo fixtures:

- Working corpus: mapper `0` (41 ROMs), `3` (6 ROMs), `7` (1 ROM)
- Not-working corpus: mapper `1` (2 ROMs), `2` (7 ROMs)

Roadmap:

1. Phase A: Mapper 0 (NROM), mapper 3 (CNROM), mapper 7 (AxROM)
2. Phase B: Mapper 2 (UxROM), mapper 1 (MMC1)
3. Phase C: Additional mappers as needed

## Execution Strategy

### Deterministic Baseline (first)

1. Start CPU at Reset vector.
2. Step instructions with budget limits:
   - max instructions
   - max branch frontier size
   - max visits per `(pc, mapping_signature)`
3. Follow deterministic control flow.
4. Record dynamic bank-switch writes and resulting mappings.

### Controlled Path Expansion (second)

For conditional branches, allow bounded exploration:

1. Continue with actual CPU flags (primary path).
2. Optionally enqueue alternate path with cloned CPU+mapper state.
3. Budget gate prevents explosion.

This gives meaningful extra coverage without full symbolic execution.

## Data Model Changes

1. `internal/disasm`:
   - Change parse queues/sets from `uint16` to `ParseKey`.
   - Add mapping context to `AddAddressToParse`.

2. `internal/offset`:
   - Extend `BankReference` with mapping snapshot info (or mapping ID).

3. `internal/mapper`:
   - Add runtime write handling for mapper register ranges.
   - Add snapshot create/apply/lookups.
   - Add helpers to map CPU address to physical PRG offset under a given snapshot.

4. Label naming:
   - Add optional bank-qualified naming mode for collisions, e.g. `_func_b03_8000`.
   - Keep current names for single-bank/simple cases.

## CLI and Config Plan

Add opt-in flags first, then evaluate defaulting:

1. `-trace-mode static|emu|hybrid` (default `static` initially)
2. `-trace-max-instr N`
3. `-trace-max-branch-states N`
4. `-trace-max-visits-per-state N`

This keeps rollout safe and benchmarkable.

## Phased Implementation Plan

### Phase 0: Instrumentation and Baseline

1. Add trace stats struct and debug output.
2. Add mapper corpus benchmark script (coverage/accuracy metrics).
3. Document baseline pass/fail by mapper and ROM set.

Acceptance:

1. Existing tests pass.
2. Baseline metrics are reproducible.

### Phase 1: Emulator Trace Prototype (Advisory Only)

1. Implement `internal/trace/m6502emu` with custom memory bus.
2. Record executed `(pc, mapping_signature)` and bank-switch events.
3. Do not alter disassembly output yet; log comparison only.

Acceptance:

1. Emulator trace runs on mapper 0/3/7 fixtures.
2. No output regressions in current pipeline.

### Phase 2: Bank-Aware Parse Keys

1. Introduce `ParseKey` and update parse queues/dedupe.
2. Thread mapping ID through parse API and branch bookkeeping.
3. Add tests for same `PC` parsed in multiple mapping contexts.

Acceptance:

1. New tests cover duplicate-CPU-address multi-bank scenarios.
2. Existing tests remain green.

### Phase 3: Mapper Runtime Integration

1. Implement runtime writes for mapper 2/3/7 first, then mapper 1.
2. Feed emulator mapping snapshots into disasm reads (`OffsetInfo`/`ReadMemory` by snapshot).
3. Fix CDL multi-bank handling while touching mapper internals.

Acceptance:

1. Mapper 7 Battletoads path coverage improves across switched banks.
2. Mapper 2/1 not-working sample set shows measurable progress.

### Phase 4: Symbol and Output Stabilization

1. Add bank-qualified symbol fallback for collisions.
2. Ensure assembler outputs (`ca65`, `asm6`, `nesasm`, `retroasm`) remain valid.
3. Add regression tests for duplicate logical addresses across banks.

Acceptance:

1. `-verify` passes for unchanged working ROMs.
2. No symbol collision regressions on multi-bank outputs.

### Phase 5: Controlled Branch Expansion

1. Add optional bounded alternate-branch exploration.
2. Introduce heuristics for loop throttling and state pruning.
3. Measure incremental code discovery vs. runtime.

Acceptance:

1. Coverage gain on mapper-heavy ROMs with bounded runtime overhead.
2. Feature remains optional behind CLI controls.

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

## Immediate Next Steps

1. Implement Phase 0 metrics scaffolding.
2. Build `m6502emu` trace prototype with mapper 0/3/7 runtime bus.
3. Land `ParseKey` refactor before expanding mapper coverage.

