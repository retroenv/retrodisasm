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

1. Implement `internal/trace/m6502emu` with custom NES memory bus.
2. Implement NES address map (RAM, mirrors, PPU/APU stubs, mapper delegation).
3. Record executed `(pc, mapping_signature)` and bank-switch events.
4. Do not alter disassembly output yet; log comparison only.

Acceptance:

1. Emulator trace runs on mapper 0/7 fixtures (mapper 3 needs no PRG runtime handling).
2. No output regressions in current pipeline.

### Phase 2: Bank-Aware Parse Keys

1. Introduce `ParseKey` and update parse queues/dedupe.
2. Thread mapping ID through parse API and branch bookkeeping.
3. Add tests for same `PC` parsed in multiple mapping contexts.

Acceptance:

1. New tests cover duplicate-CPU-address multi-bank scenarios.
2. Existing tests remain green.

### Phase 3: Mapper Runtime Integration

1. Implement runtime writes for mapper 7 first, then mapper 2, then mapper 1.
   - Mapper 3 is excluded: it only switches CHR banks, not PRG.
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

State cloning cost is low (~2KB RAM + ~256 bytes CPU/mapper per state; 100 queued states ≈ 200KB). Bank PRG data is shared read-only. Simple value copy suffices — no copy-on-write needed.

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

1. Build `m6502emu` trace prototype with mapper 0/7 runtime bus (Phase 1).
2. Land `ParseKey` refactor before expanding mapper coverage (Phase 2).
3. Add mapper runtime write integration for mapper 7, then 2, then 1 (Phase 3).
