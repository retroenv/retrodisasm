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

	visits := make(map[visitKey]int, 4096)
	visitCaps := make(map[visitKey]int, 128)
	uniquePCs := make(map[uint16]struct{}, 4096)
	uniqueMappings := make(map[uint64]struct{}, 64)
	queuedBranches := make(map[branchStateKey]struct{}, cfg.MaxBranchStates)
	frontier := make([]executionSnapshot, 0, cfg.MaxBranchStates)
	lastHaltReason := ""
	instructionsSinceNMI := 0

	for len(res.Steps) < cfg.MaxInstructions {
		select {
		case <-ctx.Done():
			res.HaltReason = "context cancelled"
			finalizeResult(res, uniquePCs, uniqueMappings)
			return res, nil
		default:
		}

		if shouldTriggerSyntheticNMI(bus, instructionsSinceNMI) {
			cpu.TriggerNMI()
			instructionsSinceNMI = 0
		}
		cpu.CheckInterrupts()

		signatureBefore := mapper.MappingSignature()
		vk := visitKey{PC: cpu.PC, MappingID: signatureBefore}
		limit := cfg.MaxVisitsPerPC
		if capLimit, ok := visitCaps[vk]; ok {
			limit = capLimit
		}
		visits[vk]++
		if visits[vk] > limit {
			if limit == cfg.MaxVisitsPerPC && shouldRelaxVisitLimitForStartupLoop(mapper, res, cpu.PC) {
				limit = relaxedVisitLimit(cfg.MaxVisitsPerPC)
				visitCaps[vk] = limit
			} else if limit == cfg.MaxVisitsPerPC && shouldRelaxVisitLimitForPPUDataLoop(mapper, res, cpu.PC) {
				limit = relaxedPPULoopVisitLimit(cfg.MaxVisitsPerPC)
				visitCaps[vk] = limit
			}
		}
		if visits[vk] > limit {
			lastHaltReason = fmt.Sprintf("pc visit limit exceeded at $%04X", cpu.PC)
			if !restoreNextState(&frontier, cpu, bus, mapper, &instructionsSinceNMI, res) {
				break
			}
			continue
		}

		pre := snapshotExecutionState(cpu, bus, mapper, instructionsSinceNMI)
		if err := cpu.Step(); err != nil {
			lastHaltReason = err.Error()
			if !restoreNextState(&frontier, cpu, bus, mapper, &instructionsSinceNMI, res) {
				break
			}
			continue
		}
		instructionsSinceNMI++

		ts := cpu.TraceStep
		signatureAfter := mapper.MappingSignature()
		uniquePCs[ts.PC] = struct{}{}
		uniqueMappings[signatureAfter] = struct{}{}

		bankID, physicalOffset, hasPhysical := mapper.ResolveAddress(ts.PC)

		operands := make([]byte, len(ts.OpcodeOperands))
		copy(operands, ts.OpcodeOperands)

		step := TraceStep{
			PC:               ts.PC,
			OpcodeName:       ts.Opcode.Instruction.Name,
			OpcodeOperands:   operands,
			MappingSignature: signatureAfter,
			BankID:           bankID,
			PhysicalOffset:   physicalOffset,
			HasPhysical:      hasPhysical,
		}
		res.Steps = append(res.Steps, step)

		if !isConditionalBranch(step.OpcodeName) || len(step.OpcodeOperands) == 0 {
			continue
		}
		res.ConditionalBranchCount++

		if cfg.MaxBranchStates <= 0 {
			continue
		}

		altPC, target, fallthroughPC, taken, ok := alternateBranchTarget(step, cpu.PC)
		if !ok {
			continue
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
		if _, exists := queuedBranches[key]; exists {
			continue
		}
		queuedBranches[key] = struct{}{}

		if len(frontier) >= cfg.MaxBranchStates {
			res.BranchAlternateBudgetDrops++
			continue
		}

		frontier = append(frontier, branchSnapshot)
		res.BranchAlternates = append(res.BranchAlternates, BranchAlternate{
			FromPC:            step.PC,
			Address:           altPC,
			MappingSignature:  signatureBefore,
			BranchTarget:      target,
			FallthroughTarget: fallthroughPC,
			Taken:             taken,
		})
	}

	if res.HaltReason == "" {
		switch {
		case len(res.Steps) >= cfg.MaxInstructions:
			res.HaltReason = "instruction budget exhausted"
		case lastHaltReason != "":
			res.HaltReason = lastHaltReason
		default:
			res.HaltReason = "trace halted"
		}
	}

	finalizeResult(res, uniquePCs, uniqueMappings)
	return res, nil
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

func summarizeMapperWrites(res *Result) {
	if len(res.BankSwitchWrites) == 0 {
		return
	}

	type countPair struct {
		count   int
		changed int
	}
	type transitionKey struct {
		before uint64
		after  uint64
	}

	addressCounts := map[uint16]countPair{}
	pcCounts := map[uint16]countPair{}
	transitionCounts := map[transitionKey]int{}

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

		key := transitionKey{before: event.BeforeMapping, after: event.AfterMapping}
		transitionCounts[key]++
	}

	res.MapperWriteUniqueAddresses = len(addressCounts)
	res.MapperWriteUniquePCs = len(pcCounts)
	res.MapperWriteUniqueTransitions = len(transitionCounts)

	addressHotspots := make([]MapperWriteAddressHotspot, 0, len(addressCounts))
	for address, c := range addressCounts {
		addressHotspots = append(addressHotspots, MapperWriteAddressHotspot{
			Address:      address,
			Count:        c.count,
			ChangedCount: c.changed,
		})
	}
	slices.SortFunc(addressHotspots, func(a, b MapperWriteAddressHotspot) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.ChangedCount != b.ChangedCount {
			return b.ChangedCount - a.ChangedCount
		}
		switch {
		case a.Address < b.Address:
			return -1
		case a.Address > b.Address:
			return 1
		default:
			return 0
		}
	})
	res.MapperWriteAddressHotspots = truncateHotspots(addressHotspots, mapperHotspotLimit)

	pcHotspots := make([]MapperWritePCHotspot, 0, len(pcCounts))
	for pc, c := range pcCounts {
		pcHotspots = append(pcHotspots, MapperWritePCHotspot{
			PC:           pc,
			Count:        c.count,
			ChangedCount: c.changed,
		})
	}
	slices.SortFunc(pcHotspots, func(a, b MapperWritePCHotspot) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.ChangedCount != b.ChangedCount {
			return b.ChangedCount - a.ChangedCount
		}
		switch {
		case a.PC < b.PC:
			return -1
		case a.PC > b.PC:
			return 1
		default:
			return 0
		}
	})
	res.MapperWritePCHotspots = truncateHotspots(pcHotspots, mapperHotspotLimit)

	transitionHotspots := make([]MapperWriteTransitionHotspot, 0, len(transitionCounts))
	for key, count := range transitionCounts {
		transitionHotspots = append(transitionHotspots, MapperWriteTransitionHotspot{
			BeforeMapping: key.before,
			AfterMapping:  key.after,
			Count:         count,
		})
	}
	slices.SortFunc(transitionHotspots, func(a, b MapperWriteTransitionHotspot) int {
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
	res.MapperWriteTransitionHotspots = truncateHotspots(transitionHotspots, mapperHotspotLimit)
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

func restoreNextState(frontier *[]executionSnapshot, cpu *cpu6502.CPU, bus *nesBus, mapper Mapper,
	instructionsSinceNMI *int, res *Result) bool {
	if len(*frontier) == 0 {
		return false
	}
	next := (*frontier)[0]
	*frontier = (*frontier)[1:]
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

func shouldRelaxVisitLimitForStartupLoop(mapper Mapper, res *Result, pc uint16) bool {
	if !canRelaxVisitLimitForLoop(res, pc) {
		return false
	}
	return isTightCounterLoopPC(mapper, pc)
}

func shouldRelaxVisitLimitForPPUDataLoop(mapper Mapper, res *Result, pc uint16) bool {
	if !canRelaxVisitLimitForLoop(res, pc) {
		return false
	}
	return isPPUDataStreamLoopPC(mapper, pc)
}

func canRelaxVisitLimitForLoop(res *Result, pc uint16) bool {
	if len(res.Steps) > 20000 {
		return false
	}
	for _, event := range res.BankSwitchWrites {
		if event.Changed {
			return false
		}
	}
	if pc < 0x8000 {
		return false
	}
	return true
}

func isTightCounterLoopPC(mapper Mapper, pc uint16) bool {
	op0 := mapper.ReadMemory(pc)

	// Branch endpoint in a tight backward loop, e.g. BNE -4.
	if isConditionalBranchOpcode(op0) {
		target := pc + 2 + uint16(int16(int8(mapper.ReadMemory(pc+1))))
		if target <= pc && pc-target <= 0x40 {
			return true
		}
	}

	// Counter + backward-branch patterns starting near this PC.
	// This catches common startup delays and RAM clear loops with small loop bodies.
	for offset := uint16(0); offset <= 5; offset++ {
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

	// Long indexed clear loops are common during startup, e.g.:
	//   STA ...,X
	//   ...
	//   DEX
	//   BNE <loop-start>
	// Visit caps usually trigger at the loop start store instruction, not near DEX/BNE.
	if isStoreOpcode(mapper.ReadMemory(pc)) {
		for offset := uint16(0); offset <= 0x30; offset++ {
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
	}

	if isDelayLoopPrefaceOpcode(op0) &&
		isIndexCounterOpcode(mapper.ReadMemory(pc+1)) &&
		isConditionalBranchOpcode(mapper.ReadMemory(pc+2)) {
		target := pc + 4 + uint16(int16(int8(mapper.ReadMemory(pc+3))))
		if target == pc || target == pc+1 {
			return true
		}
	}
	return false
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

func isPPUDataStreamLoopPC(mapper Mapper, pc uint16) bool {
	// Check current PC and nearby PCs for a short streaming loop:
	//   STA $2007
	//   INY/DEX/...
	//   CPY/CPX #imm
	//   B?? <back>
	for back := uint16(0); back <= 8; back++ {
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
