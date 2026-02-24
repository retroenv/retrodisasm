// Package m6502emu provides an advisory NES CPU trace using the 6502 emulator.
package m6502emu

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	cpu6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

const (
	defaultMaxInstructions = 100000
	defaultMaxVisitsPerPC  = 8
	defaultMaxBranchStates = 0
	mapperHotspotLimit     = 8
	bootLoopVisitLimit     = 2048
	ppuLoopVisitLimit      = 8192
	syntheticNMIInterval   = 2048
)

// Mapper defines the mapper functions needed by the emulator trace.
type Mapper interface {
	ReadMemory(address uint16) byte
	MappingSignature() uint64
	ResolveAddress(address uint16) (bankID int, physicalOffset uint32, ok bool)
	ApplyMapperWrite(address uint16, value byte) bool
	SnapshotRuntimeState() any
	RestoreRuntimeState(snapshot any) bool
}

// Config controls advisory trace execution limits.
type Config struct {
	MaxInstructions int
	MaxVisitsPerPC  int
	MaxBranchStates int
	Joypad1State    byte
	Joypad2State    byte
	Joypad1Sequence []byte
	Joypad2Sequence []byte
}

// TraceStep contains a single executed instruction with mapping metadata.
type TraceStep struct {
	PC               uint16
	OpcodeName       string
	OpcodeOperands   []byte
	MappingSignature uint64
	BankID           int
	PhysicalOffset   uint32
	HasPhysical      bool
}

// BranchAlternate records a conditional-branch alternate destination inferred from trace flow.
type BranchAlternate struct {
	FromPC            uint16
	Address           uint16
	MappingSignature  uint64
	BranchTarget      uint16
	FallthroughTarget uint16
	Taken             bool
}

// BankSwitchWrite records a write in mapper register space.
type BankSwitchWrite struct {
	PC            uint16
	Address       uint16
	Value         byte
	BeforeMapping uint64
	AfterMapping  uint64
	Changed       bool
}

// MapperWriteAddressHotspot summarizes mapper writes grouped by write address.
type MapperWriteAddressHotspot struct {
	Address      uint16
	Count        int
	ChangedCount int
}

// MapperWritePCHotspot summarizes mapper writes grouped by write source PC.
type MapperWritePCHotspot struct {
	PC           uint16
	Count        int
	ChangedCount int
}

// MapperWriteTransitionHotspot summarizes mapper writes grouped by mapping transition.
type MapperWriteTransitionHotspot struct {
	BeforeMapping uint64
	AfterMapping  uint64
	Count         int
}

// Result holds all advisory trace output.
type Result struct {
	Steps            []TraceStep
	BankSwitchWrites []BankSwitchWrite
	BranchAlternates []BranchAlternate

	UniquePCCount              int
	UniqueMappingCount         int
	ConditionalBranchCount     int
	BranchAlternateCount       int
	BranchAlternateBudgetDrops int
	BranchStatesExecuted       int
	Instructions               int
	HaltReason                 string
	Duration                   time.Duration

	MapperWriteUniqueAddresses    int
	MapperWriteUniquePCs          int
	MapperWriteUniqueTransitions  int
	MapperWriteAddressHotspots    []MapperWriteAddressHotspot
	MapperWritePCHotspots         []MapperWritePCHotspot
	MapperWriteTransitionHotspots []MapperWriteTransitionHotspot
}

type cpuSnapshot struct {
	A  uint8
	X  uint8
	Y  uint8
	PC uint16
	SP uint8

	Flags cpu6502.Flags
}

type executionSnapshot struct {
	cpu    cpuSnapshot
	bus    busSnapshot
	mapper any

	instructionsSinceNMI int
}

type visitKey struct {
	PC        uint16
	MappingID uint64
}

type branchStateKey struct {
	PC        uint16
	MappingID uint64
	A         uint8
	X         uint8
	Y         uint8
	SP        uint8
	Flags     uint8
}

// traceState holds the mutable state for a single Run execution.
type traceState struct {
	visits         map[visitKey]int
	visitCaps      map[visitKey]int
	uniquePCs      map[uint16]struct{}
	uniqueMappings map[uint64]struct{}
	queuedBranches map[branchStateKey]struct{}

	// frontier holds normal branch alternates (initial mapping).
	// priorityFrontier holds post-bank-switch alternates, explored first.
	frontier         []executionSnapshot
	priorityFrontier []executionSnapshot
	initialMapping   uint64

	instructionsSinceNMI int
	lastHaltReason       string
}

func newTraceState(cfg Config) *traceState {
	return &traceState{
		visits:           make(map[visitKey]int, 4096),
		visitCaps:        make(map[visitKey]int, 128),
		uniquePCs:        make(map[uint16]struct{}, 4096),
		uniqueMappings:   make(map[uint64]struct{}, 64),
		queuedBranches:   make(map[branchStateKey]struct{}, cfg.MaxBranchStates),
		frontier:         make([]executionSnapshot, 0, cfg.MaxBranchStates),
		priorityFrontier: make([]executionSnapshot, 0, 64),
	}
}

// frontierSize returns the total number of queued branch alternate states.
func (ts *traceState) frontierSize() int {
	return len(ts.frontier) + len(ts.priorityFrontier)
}

// Run executes a bounded advisory 6502 trace over a NES cartridge using mapper-backed PRG reads.
func Run(ctx context.Context, cart *cartridge.Cartridge, mapper Mapper, cfg Config) (*Result, error) {
	cfg = normalizeConfig(cfg)

	res := &Result{}
	start := time.Now()
	defer func() {
		res.Duration = time.Since(start)
	}()

	bus := newNesBus(cart, mapper, cfg)
	var currentPC uint16

	bus.onMapperWrite = func(address uint16, value byte) {
		before := mapper.MappingSignature()
		changed := mapper.ApplyMapperWrite(address, value)
		after := mapper.MappingSignature()
		res.BankSwitchWrites = append(res.BankSwitchWrites, BankSwitchWrite{
			PC:            currentPC,
			Address:       address,
			Value:         value,
			BeforeMapping: before,
			AfterMapping:  after,
			Changed:       changed || before != after,
		})
	}

	mem, err := cpu6502.NewMemory(bus)
	if err != nil {
		return nil, fmt.Errorf("creating 6502 memory: %w", err)
	}

	cpu := cpu6502.New(mem,
		cpu6502.WithTracing(),
		cpu6502.WithPreExecutionHook(func(c *cpu6502.CPU, _ *cpu6502.Instruction, _ ...any) {
			currentPC = c.PC
		}),
	)

	ts := newTraceState(cfg)
	runTraceLoop(ctx, cpu, bus, mapper, cfg, res, ts)

	if res.HaltReason == "" {
		switch {
		case len(res.Steps) >= cfg.MaxInstructions:
			res.HaltReason = "instruction budget exhausted"
		case ts.lastHaltReason != "":
			res.HaltReason = ts.lastHaltReason
		default:
			res.HaltReason = "trace halted"
		}
	}

	finalizeResult(res, ts.uniquePCs, ts.uniqueMappings)
	return res, nil
}

// runTraceLoop is the main execution loop for the advisory trace.
func runTraceLoop(
	ctx context.Context, cpu *cpu6502.CPU, bus *nesBus, mapper Mapper,
	cfg Config, res *Result, ts *traceState,
) {

	ts.initialMapping = mapper.MappingSignature()

	for len(res.Steps) < cfg.MaxInstructions {
		select {
		case <-ctx.Done():
			res.HaltReason = "context cancelled"
			finalizeResult(res, ts.uniquePCs, ts.uniqueMappings)
			return
		default:
		}

		if shouldTriggerSyntheticNMI(bus, ts.instructionsSinceNMI) {
			cpu.TriggerNMI()
			ts.instructionsSinceNMI = 0
		}
		cpu.CheckInterrupts()

		if isUnofficialOpcode(mapper.ReadMemory(cpu.PC)) {
			ts.lastHaltReason = fmt.Sprintf("unofficial opcode $%02X at $%04X", mapper.ReadMemory(cpu.PC), cpu.PC)
			if !restoreNextState(ts, cpu, bus, mapper, &ts.instructionsSinceNMI, res) {
				break
			}
			continue
		}

		signatureBefore := mapper.MappingSignature()
		if !checkAndHandleVisitLimit(cpu, bus, mapper, cfg, res, ts, signatureBefore) {
			break
		}
		if res.HaltReason != "" {
			return
		}

		pre := snapshotExecutionState(cpu, bus, mapper, ts.instructionsSinceNMI)
		if err := cpu.Step(); err != nil {
			ts.lastHaltReason = err.Error()
			if !restoreNextState(ts, cpu, bus, mapper, &ts.instructionsSinceNMI, res) {
				break
			}
			continue
		}
		ts.instructionsSinceNMI++

		step := recordTraceStep(cpu, mapper, res, ts.uniquePCs, ts.uniqueMappings)
		recordBranchAlternate(step, pre, signatureBefore, cpu.PC, cfg, res, ts)
	}
}

// checkAndHandleVisitLimit checks the visit limit for the current PC and handles limit exceeded.
// Returns false if the loop should break.
func checkAndHandleVisitLimit(
	cpu *cpu6502.CPU, bus *nesBus, mapper Mapper, cfg Config,
	res *Result, ts *traceState, signatureBefore uint64,
) bool {

	vk := visitKey{PC: cpu.PC, MappingID: signatureBefore}
	limit := cfg.MaxVisitsPerPC
	if capLimit, ok := ts.visitCaps[vk]; ok {
		limit = capLimit
	}
	ts.visits[vk]++
	if ts.visits[vk] > limit {
		limit = maybeRelaxVisitLimit(cpu, mapper, cfg, res, ts, vk, limit)
	}
	if ts.visits[vk] > limit {
		ts.lastHaltReason = fmt.Sprintf("pc visit limit exceeded at $%04X", cpu.PC)
		return restoreNextState(ts, cpu, bus, mapper, &ts.instructionsSinceNMI, res)
	}
	return true
}

// maybeRelaxVisitLimit attempts to raise the visit limit for known loop patterns.
func maybeRelaxVisitLimit(
	cpu *cpu6502.CPU, mapper Mapper, cfg Config,
	res *Result, ts *traceState, vk visitKey, limit int,
) int {

	if limit != cfg.MaxVisitsPerPC {
		return limit
	}
	if shouldRelaxVisitLimitForStartupLoop(mapper, cfg, res, cpu.PC) {
		limit = relaxedVisitLimit(cfg.MaxVisitsPerPC)
		ts.visitCaps[vk] = limit
	} else if shouldRelaxVisitLimitForPPUDataLoop(mapper, cfg, res, cpu.PC) {
		limit = relaxedPPULoopVisitLimit(cfg.MaxVisitsPerPC)
		ts.visitCaps[vk] = limit
	}
	return limit
}

// recordTraceStep appends the executed instruction to the result and returns the step.
func recordTraceStep(
	cpu *cpu6502.CPU, mapper Mapper, res *Result,
	uniquePCs map[uint16]struct{}, uniqueMappings map[uint64]struct{},
) TraceStep {

	rawStep := cpu.TraceStep
	signatureAfter := mapper.MappingSignature()
	uniquePCs[rawStep.PC] = struct{}{}
	uniqueMappings[signatureAfter] = struct{}{}

	bankID, physicalOffset, hasPhysical := mapper.ResolveAddress(rawStep.PC)
	operands := make([]byte, len(rawStep.OpcodeOperands))
	copy(operands, rawStep.OpcodeOperands)

	step := TraceStep{
		PC:               rawStep.PC,
		OpcodeName:       rawStep.Opcode.Instruction.Name,
		OpcodeOperands:   operands,
		MappingSignature: signatureAfter,
		BankID:           bankID,
		PhysicalOffset:   physicalOffset,
		HasPhysical:      hasPhysical,
	}
	res.Steps = append(res.Steps, step)
	return step
}

// recordBranchAlternate enqueues an alternate branch state if applicable.
func recordBranchAlternate(
	step TraceStep, pre executionSnapshot, signatureBefore uint64, nextPC uint16,
	cfg Config, res *Result, ts *traceState,
) {

	if !isConditionalBranch(step.OpcodeName) || len(step.OpcodeOperands) == 0 {
		return
	}
	res.ConditionalBranchCount++

	if cfg.MaxBranchStates <= 0 {
		return
	}

	altPC, target, fallthroughPC, taken, ok := alternateBranchTarget(step, nextPC)
	if !ok {
		return
	}

	branchSnapshot := pre
	branchSnapshot.cpu.PC = altPC
	key := branchStateKey{
		PC:        altPC,
		MappingID: signatureBefore,
		A:         branchSnapshot.cpu.A,
		X:         branchSnapshot.cpu.X,
		Y:         branchSnapshot.cpu.Y,
		SP:        branchSnapshot.cpu.SP,
		Flags:     flagsByte(branchSnapshot.cpu.Flags),
	}
	if _, exists := ts.queuedBranches[key]; exists {
		return
	}
	ts.queuedBranches[key] = struct{}{}

	if ts.frontierSize() >= cfg.MaxBranchStates {
		res.BranchAlternateBudgetDrops++
		return
	}

	// Prioritize post-bank-switch alternates: they are more likely to
	// discover code in different bank mappings that static analysis misses.
	if signatureBefore != ts.initialMapping {
		ts.priorityFrontier = append(ts.priorityFrontier, branchSnapshot)
	} else {
		ts.frontier = append(ts.frontier, branchSnapshot)
	}
	res.BranchAlternates = append(res.BranchAlternates, BranchAlternate{
		FromPC:            step.PC,
		Address:           altPC,
		MappingSignature:  signatureBefore,
		BranchTarget:      target,
		FallthroughTarget: fallthroughPC,
		Taken:             taken,
	})
}

func normalizeConfig(cfg Config) Config {
	if cfg.MaxInstructions <= 0 {
		cfg.MaxInstructions = defaultMaxInstructions
	}
	if cfg.MaxVisitsPerPC <= 0 {
		cfg.MaxVisitsPerPC = defaultMaxVisitsPerPC
	}
	if cfg.MaxBranchStates < 0 {
		cfg.MaxBranchStates = defaultMaxBranchStates
	}
	cfg.Joypad1State = clampJoypadState(cfg.Joypad1State)
	cfg.Joypad2State = clampJoypadState(cfg.Joypad2State)
	cfg.Joypad1Sequence = copyJoypadSequence(cfg.Joypad1Sequence)
	cfg.Joypad2Sequence = copyJoypadSequence(cfg.Joypad2Sequence)
	return cfg
}

func clampJoypadState(state byte) byte {
	return state
}

func copyJoypadSequence(seq []byte) []byte {
	if len(seq) == 0 {
		return nil
	}
	copied := make([]byte, len(seq))
	copy(copied, seq)
	for i := range copied {
		copied[i] = clampJoypadState(copied[i])
	}
	return copied
}

func finalizeResult(res *Result, uniquePCs map[uint16]struct{}, uniqueMappings map[uint64]struct{}) {
	res.Instructions = len(res.Steps)
	res.UniquePCCount = len(uniquePCs)
	res.UniqueMappingCount = len(uniqueMappings)
	res.BranchAlternateCount = len(res.BranchAlternates)
	summarizeMapperWrites(res)
}

type mapperWriteCountPair struct {
	count   int
	changed int
}

type mapperWriteTransitionKey struct {
	before uint64
	after  uint64
}

func summarizeMapperWrites(res *Result) {
	if len(res.BankSwitchWrites) == 0 {
		return
	}

	addressCounts := map[uint16]mapperWriteCountPair{}
	pcCounts := map[uint16]mapperWriteCountPair{}
	transitionCounts := map[mapperWriteTransitionKey]int{}

	for _, event := range res.BankSwitchWrites {
		addr := addressCounts[event.Address]
		addr.count++
		if event.Changed {
			addr.changed++
		}
		addressCounts[event.Address] = addr

		pc := pcCounts[event.PC]
		pc.count++
		if event.Changed {
			pc.changed++
		}
		pcCounts[event.PC] = pc

		key := mapperWriteTransitionKey{before: event.BeforeMapping, after: event.AfterMapping}
		transitionCounts[key]++
	}

	res.MapperWriteUniqueAddresses = len(addressCounts)
	res.MapperWriteUniquePCs = len(pcCounts)
	res.MapperWriteUniqueTransitions = len(transitionCounts)

	res.MapperWriteAddressHotspots = buildAddressHotspots(addressCounts)
	res.MapperWritePCHotspots = buildPCHotspots(pcCounts)
	res.MapperWriteTransitionHotspots = buildTransitionHotspots(transitionCounts)
}

func buildAddressHotspots(counts map[uint16]mapperWriteCountPair) []MapperWriteAddressHotspot {
	hotspots := make([]MapperWriteAddressHotspot, 0, len(counts))
	for address, c := range counts {
		hotspots = append(hotspots, MapperWriteAddressHotspot{
			Address:      address,
			Count:        c.count,
			ChangedCount: c.changed,
		})
	}
	slices.SortFunc(hotspots, func(a, b MapperWriteAddressHotspot) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.ChangedCount != b.ChangedCount {
			return b.ChangedCount - a.ChangedCount
		}
		if a.Address < b.Address {
			return -1
		}
		if a.Address > b.Address {
			return 1
		}
		return 0
	})
	return truncateHotspots(hotspots, mapperHotspotLimit)
}

func buildPCHotspots(counts map[uint16]mapperWriteCountPair) []MapperWritePCHotspot {
	hotspots := make([]MapperWritePCHotspot, 0, len(counts))
	for pc, c := range counts {
		hotspots = append(hotspots, MapperWritePCHotspot{
			PC:           pc,
			Count:        c.count,
			ChangedCount: c.changed,
		})
	}
	slices.SortFunc(hotspots, func(a, b MapperWritePCHotspot) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.ChangedCount != b.ChangedCount {
			return b.ChangedCount - a.ChangedCount
		}
		if a.PC < b.PC {
			return -1
		}
		if a.PC > b.PC {
			return 1
		}
		return 0
	})
	return truncateHotspots(hotspots, mapperHotspotLimit)
}

func buildTransitionHotspots(counts map[mapperWriteTransitionKey]int) []MapperWriteTransitionHotspot {
	hotspots := make([]MapperWriteTransitionHotspot, 0, len(counts))
	for key, count := range counts {
		hotspots = append(hotspots, MapperWriteTransitionHotspot{
			BeforeMapping: key.before,
			AfterMapping:  key.after,
			Count:         count,
		})
	}
	slices.SortFunc(hotspots, func(a, b MapperWriteTransitionHotspot) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.BeforeMapping != b.BeforeMapping {
			if a.BeforeMapping < b.BeforeMapping {
				return -1
			}
			return 1
		}
		if a.AfterMapping < b.AfterMapping {
			return -1
		}
		if a.AfterMapping > b.AfterMapping {
			return 1
		}
		return 0
	})
	return truncateHotspots(hotspots, mapperHotspotLimit)
}

func truncateHotspots[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func isConditionalBranch(opName string) bool {
	switch strings.ToUpper(opName) {
	case "BCC", "BCS", "BEQ", "BMI", "BNE", "BPL", "BVC", "BVS":
		return true
	default:
		return false
	}
}

func isUnofficialOpcode(op byte) bool {
	opcode := cpu6502.Opcodes[op]
	return opcode.Instruction == nil || opcode.Instruction.Unofficial
}

func alternateBranchTarget(step TraceStep, nextPC uint16) (alternate, target, fallthroughPC uint16,
	taken, ok bool) {

	operand := step.OpcodeOperands[len(step.OpcodeOperands)-1]
	fallthroughPC = step.PC + 2
	target = uint16(int32(fallthroughPC) + int32(int8(operand)))

	switch nextPC {
	case target:
		return fallthroughPC, target, fallthroughPC, true, true
	case fallthroughPC:
		return target, target, fallthroughPC, false, true
	default:
		return 0, target, fallthroughPC, false, false
	}
}

func snapshotExecutionState(cpu *cpu6502.CPU, bus *nesBus, mapper Mapper, instructionsSinceNMI int) executionSnapshot {
	return executionSnapshot{
		cpu: cpuSnapshot{
			A:  cpu.A,
			X:  cpu.X,
			Y:  cpu.Y,
			PC: cpu.PC,
			SP: cpu.SP,
			Flags: cpu6502.Flags{
				C: cpu.Flags.C,
				Z: cpu.Flags.Z,
				I: cpu.Flags.I,
				D: cpu.Flags.D,
				B: cpu.Flags.B,
				U: cpu.Flags.U,
				V: cpu.Flags.V,
				N: cpu.Flags.N,
			},
		},
		bus:                  bus.snapshot(),
		mapper:               mapper.SnapshotRuntimeState(),
		instructionsSinceNMI: instructionsSinceNMI,
	}
}

func restoreExecutionState(state executionSnapshot, cpu *cpu6502.CPU, bus *nesBus, mapper Mapper,
	instructionsSinceNMI *int) bool {

	if !mapper.RestoreRuntimeState(state.mapper) {
		return false
	}
	bus.restore(state.bus)
	cpu.A = state.cpu.A
	cpu.X = state.cpu.X
	cpu.Y = state.cpu.Y
	cpu.PC = state.cpu.PC
	cpu.SP = state.cpu.SP
	cpu.Flags = state.cpu.Flags
	*instructionsSinceNMI = state.instructionsSinceNMI
	return true
}

func restoreNextState(ts *traceState, cpu *cpu6502.CPU, bus *nesBus, mapper Mapper,
	instructionsSinceNMI *int, res *Result) bool {

	var next executionSnapshot
	switch {
	case len(ts.priorityFrontier) > 0:
		next = ts.priorityFrontier[0]
		ts.priorityFrontier = ts.priorityFrontier[1:]
	case len(ts.frontier) > 0:
		next = ts.frontier[0]
		ts.frontier = ts.frontier[1:]
	default:
		return false
	}
	if !restoreExecutionState(next, cpu, bus, mapper, instructionsSinceNMI) {
		return false
	}
	res.BranchStatesExecuted++
	return true
}

func shouldTriggerSyntheticNMI(bus *nesBus, instructionsSinceNMI int) bool {
	if !bus.ppuNMIEnabled() {
		return false
	}
	return instructionsSinceNMI >= syntheticNMIInterval
}

func flagsByte(flags cpu6502.Flags) uint8 {
	return flags.C |
		(flags.Z << 1) |
		(flags.I << 2) |
		(flags.D << 3) |
		(flags.B << 4) |
		(flags.U << 5) |
		(flags.V << 6) |
		(flags.N << 7)
}

func relaxedVisitLimit(base int) int {
	if base >= bootLoopVisitLimit {
		return base
	}
	return bootLoopVisitLimit
}

func relaxedPPULoopVisitLimit(base int) int {
	if base >= ppuLoopVisitLimit {
		return base
	}
	return ppuLoopVisitLimit
}

func shouldRelaxVisitLimitForStartupLoop(mapper Mapper, cfg Config, res *Result, pc uint16) bool {
	if !canRelaxVisitLimitForLoop(cfg, res, pc) {
		return false
	}
	return isTightCounterLoopPC(mapper, pc)
}

func shouldRelaxVisitLimitForPPUDataLoop(mapper Mapper, cfg Config, res *Result, pc uint16) bool {
	if !canRelaxVisitLimitForLoop(cfg, res, pc) {
		return false
	}
	return isPPUDataStreamLoopPC(mapper, pc)
}

func canRelaxVisitLimitForLoop(cfg Config, res *Result, pc uint16) bool {
	if len(res.Steps) > cfg.MaxInstructions/2 {
		return false
	}
	return pc >= 0x8000
}

func isTightCounterLoopPC(mapper Mapper, pc uint16) bool {
	op0 := mapper.ReadMemory(pc)

	if isTightBackwardBranchLoop(mapper, pc, op0) {
		return true
	}
	if isInsideTightBackwardBranchLoop(mapper, pc) {
		return true
	}
	if isNearbyCounterBranchPattern(mapper, pc) {
		return true
	}
	if isStoreOpcode(op0) && isLongIndexedClearLoop(mapper, pc) {
		return true
	}
	return isDelayLoopPattern(mapper, pc, op0)
}

// isTightBackwardBranchLoop checks if pc is the branch endpoint of a tight backward loop.
func isTightBackwardBranchLoop(mapper Mapper, pc uint16, op0 byte) bool {
	if !isConditionalBranchOpcode(op0) {
		return false
	}
	target := pc + 2 + uint16(int16(int8(mapper.ReadMemory(pc+1))))
	return target <= pc && pc-target <= 0x40
}

// isInsideTightBackwardBranchLoop checks whether pc is inside a short loop
// body by scanning forward for a nearby conditional backward branch.
func isInsideTightBackwardBranchLoop(mapper Mapper, pc uint16) bool {
	for offset := uint16(1); offset <= 12; offset++ {
		checkPC := pc + offset
		if checkPC < 0x8000 {
			continue
		}
		op := mapper.ReadMemory(checkPC)
		if !isConditionalBranchOpcode(op) {
			continue
		}
		target := checkPC + 2 + uint16(int16(int8(mapper.ReadMemory(checkPC+1))))
		if target <= pc && checkPC+2-target <= 0x40 {
			return true
		}
	}
	return false
}

// isNearbyCounterBranchPattern checks for DEX/DEY/INX/INY + BNE patterns near pc.
func isNearbyCounterBranchPattern(mapper Mapper, pc uint16) bool {
	for offset := range uint16(6) {
		counterPC := pc + offset
		if !isIndexCounterOpcode(mapper.ReadMemory(counterPC)) {
			continue
		}
		if !isConditionalBranchOpcode(mapper.ReadMemory(counterPC + 1)) {
			continue
		}
		target := counterPC + 3 + uint16(int16(int8(mapper.ReadMemory(counterPC+2))))
		if isNearbyLoopTarget(pc, counterPC, target) {
			return true
		}
	}
	return false
}

// isLongIndexedClearLoop checks for a long indexed clear loop body containing pc.
func isLongIndexedClearLoop(mapper Mapper, pc uint16) bool {
	for offset := range uint16(0x31) {
		counterPC := pc + offset
		if !isIndexCounterOpcode(mapper.ReadMemory(counterPC)) {
			continue
		}
		if !isConditionalBranchOpcode(mapper.ReadMemory(counterPC + 1)) {
			continue
		}
		target := counterPC + 3 + uint16(int16(int8(mapper.ReadMemory(counterPC+2))))
		if target > counterPC {
			continue
		}
		distance := int(counterPC) - int(target)
		if distance <= 0 || distance > 0x40 {
			continue
		}
		if target <= pc && pc <= counterPC {
			return true
		}
	}
	return false
}

// isDelayLoopPattern checks for a 2-3 instruction delay loop starting at pc.
func isDelayLoopPattern(mapper Mapper, pc uint16, op0 byte) bool {
	if !isDelayLoopPrefaceOpcode(op0) {
		return false
	}
	if !isIndexCounterOpcode(mapper.ReadMemory(pc + 1)) {
		return false
	}
	if !isConditionalBranchOpcode(mapper.ReadMemory(pc + 2)) {
		return false
	}
	target := pc + 4 + uint16(int16(int8(mapper.ReadMemory(pc+3))))
	return target == pc || target == pc+1
}

func isNearbyLoopTarget(basePC, counterPC, target uint16) bool {
	if target > counterPC {
		return false
	}

	distance := int(counterPC) - int(target)
	if distance > 0x40 {
		return false
	}

	return target <= basePC+2
}

func isConditionalBranchOpcode(op byte) bool {
	switch op {
	case 0x90, // BCC
		0xB0, // BCS
		0xF0, // BEQ
		0x30, // BMI
		0xD0, // BNE
		0x10, // BPL
		0x50, // BVC
		0x70: // BVS
		return true
	default:
		return false
	}
}

func isIndexCounterOpcode(op byte) bool {
	switch op {
	case 0xCA, // DEX
		0x88, // DEY
		0xE8, // INX
		0xC8: // INY
		return true
	default:
		return false
	}
}

func isDelayLoopPrefaceOpcode(op byte) bool {
	switch op {
	case 0x48, // PHA
		0x08, // PHP
		0xEA, // NOP
		0x8A, // TXA
		0x98, // TYA
		0xAA, // TAX
		0xA8, // TAY
		0x18, // CLC
		0x38, // SEC
		0x58, // CLI
		0x78, // SEI
		0xD8, // CLD
		0xF8: // SED
		return true
	default:
		return false
	}
}

func isStoreOpcode(op byte) bool {
	switch op {
	case 0x85, // STA zp
		0x95, // STA zp,X
		0x8D, // STA abs
		0x9D, // STA abs,X
		0x99, // STA abs,Y
		0x81, // STA (zp,X)
		0x91: // STA (zp),Y
		return true
	default:
		return false
	}
}

// isPPUStatusPollingLoopPC checks whether pc is inside a PPU status polling
// loop: BIT/LDA $2002 followed by a conditional backward branch. These loops
// are reached many times across branch states and need the higher PPU visit
// limit rather than the boot-loop limit.
func isPPUStatusPollingLoopPC(mapper Mapper, pc uint16) bool {
	for back := range uint16(6) {
		if pc < back {
			continue
		}
		start := pc - back
		if isPPUStatusPollingPatternAt(mapper, start) && pc <= start+5 {
			return true
		}
	}
	return false
}

func isPPUStatusPollingPatternAt(mapper Mapper, start uint16) bool {
	op := mapper.ReadMemory(start)
	if op != 0x2C && op != 0xAD { // BIT abs / LDA abs
		return false
	}
	low := mapper.ReadMemory(start + 1)
	high := mapper.ReadMemory(start + 2)
	if uint16(high)<<8|uint16(low) != 0x2002 {
		return false
	}
	branchOp := mapper.ReadMemory(start + 3)
	if !isConditionalBranchOpcode(branchOp) {
		return false
	}
	target := start + 5 + uint16(int16(int8(mapper.ReadMemory(start+4))))
	return target <= start
}

func isPPUDataStreamLoopPC(mapper Mapper, pc uint16) bool {
	if isPPUStatusPollingLoopPC(mapper, pc) {
		return true
	}

	// Check current PC and nearby PCs for a short streaming loop:
	//   STA $2007
	//   INY/DEX/...
	//   CPY/CPX #imm
	//   B?? <back>
	for back := range uint16(9) {
		if pc < back {
			continue
		}
		start := pc - back
		if !isPPUDataStreamPatternAt(mapper, start) {
			continue
		}
		if pc <= start+8 {
			return true
		}
	}
	return false
}

func isPPUDataStreamPatternAt(mapper Mapper, start uint16) bool {
	if !isSTAAbsolutePPUData(mapper, start) {
		return false
	}
	if !isIndexCounterOpcode(mapper.ReadMemory(start + 3)) {
		return false
	}
	if !isIndexCompareImmediateOpcode(mapper.ReadMemory(start + 4)) {
		return false
	}
	if !isConditionalBranchOpcode(mapper.ReadMemory(start + 6)) {
		return false
	}

	branchPC := start + 6
	target := branchPC + 2 + uint16(int16(int8(mapper.ReadMemory(start+7))))
	distance := int(branchPC) - int(target)
	if distance <= 0 || distance > 16 {
		return false
	}
	return target <= start+3
}

func isSTAAbsolutePPUData(mapper Mapper, pc uint16) bool {
	return mapper.ReadMemory(pc) == 0x8D &&
		mapper.ReadMemory(pc+1) == 0x07 &&
		mapper.ReadMemory(pc+2) == 0x20
}

func isIndexCompareImmediateOpcode(op byte) bool {
	switch op {
	case 0xC0, // CPY #imm
		0xE0: // CPX #imm
		return true
	default:
		return false
	}
}
