package disasm

import (
	"context"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/program"
	cpum6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
)

// processAdditionalBanks traces unique vectors of non-last PRG banks by temporarily remapping
// the full $8000-$FFFF range to each bank. Runs two passes: the first discovers code,
// the second re-collects targets (including newly discovered ones) and seeds them.
func (dis *Disasm) processAdditionalBanks(ctx context.Context) error {
	if dis.options.Binary {
		return nil
	}

	bankCount := dis.mapper.BankCount()
	if bankCount <= 1 {
		return nil
	}

	defer func() {
		dis.processingAdditionalBanks = false
		dis.mapper.RestoreDefaultMapping()
	}()
	dis.processingAdditionalBanks = true

	// Pass 1: initial seeding with default mapping call targets.
	crossBankTargets := dis.collectCallTargetAddresses()
	if err := dis.processAdditionalBanksPass(ctx, bankCount, crossBankTargets); err != nil {
		return err
	}

	// Additional passes: re-collect targets including those discovered in prior
	// passes. Stop when no new call targets are found.
	const maxPasses = 4
	for pass := 2; pass <= maxPasses; pass++ {
		dis.mapper.RestoreDefaultMapping()
		prevCount := len(crossBankTargets)
		crossBankTargets = dis.collectCallTargetAddresses()
		if len(crossBankTargets) <= prevCount {
			break
		}
		if err := dis.processAdditionalBanksPass(ctx, bankCount, crossBankTargets); err != nil {
			return err
		}
	}

	return nil
}

func (dis *Disasm) processAdditionalBanksPass(ctx context.Context, bankCount int, crossBankTargets []uint16) error {
	dis.stats.additionalBanksConsidered += uint64(bankCount)

	for bankIndex := range bankCount {
		dis.mapper.MapBank(bankIndex)

		dis.seedLikelyMappedBankEntryPoints()
		dis.seedLikelyMappedBankCallTargets()
		dis.seedCrossBankCallTargets(crossBankTargets)
		dis.seedLikelyMappedBankPointerTableTargets()
		dis.seedLikelyMappedBankSplitPointerTableTargets()

		if err := dis.arch.InitializeBankVectors(bankIndex); err != nil {
			return fmt.Errorf("initializing vectors for bank %d: %w", bankIndex, err)
		}

		queuedBefore := len(dis.offsetsToParse)
		if err := dis.followExecutionFlow(ctx); err != nil {
			return fmt.Errorf("tracing additional bank %d: %w", bankIndex, err)
		}

		dis.stats.additionalBanksProcessed++
		if len(dis.offsetsToParse) > queuedBefore {
			dis.stats.additionalBankQueueGrowth += uint64(len(dis.offsetsToParse) - queuedBefore)
		}
	}
	return nil
}

// collectCallTargetAddresses returns CPU addresses that have the CallDestination
// flag set in the current (default) mapping. These are JSR/JMP targets from
// traced code that can be checked against other bank mappings.
func (dis *Disasm) collectCallTargetAddresses() []uint16 {
	start := dis.codeBaseAddress
	end := dis.arch.LastCodeAddress()
	targets := make([]uint16, 0, 256)

	for addr := start; addr <= end; addr++ {
		offsetInfo := dis.mapper.OffsetInfo(addr)
		if offsetInfo != nil && offsetInfo.IsType(program.CallDestination|program.FunctionReference) {
			targets = append(targets, addr)
		}
	}
	dis.stats.crossBankCallTargetsCollected = uint64(len(targets))
	return targets
}

// seedCrossBankCallTargets checks previously collected call target addresses
// against the currently mapped bank. If a target address contains a likely
// routine start opcode in the new mapping, it is seeded for tracing.
func (dis *Disasm) seedCrossBankCallTargets(targets []uint16) {
	for _, addr := range targets {
		if !dis.isValidCodeAddress(addr) || addr > dis.arch.LastCodeAddress() {
			continue
		}
		op, err := dis.ReadMemory(addr)
		if err != nil || !isLikelyM6502RoutineStartOpcode(op) {
			continue
		}
		// Use strict validation for cross-bank targets because the risk of
		// data-as-code false positives is much higher when testing addresses
		// from other bank mappings against the current bank's data.
		if !dis.validateCodeSequenceStrict(addr) {
			continue
		}
		dis.AddAddressToParse(addr, addr, 0, nil, false)
		dis.stats.crossBankCallTargetsSeeded++
	}
}

// seedLikelyMappedBankEntryPoints queues a few fixed-window anchors that commonly
// host routines in bank-switched NES code. This is intentionally conservative to
// avoid exploding parse noise from pure data banks.
func (dis *Disasm) seedLikelyMappedBankEntryPoints() {
	anchors := []uint16{0x8000, 0xA000, 0xC000, 0xE000}
	for _, addr := range anchors {
		if !dis.isValidCodeAddress(addr) || addr > dis.arch.LastCodeAddress() {
			continue
		}
		op, err := dis.ReadMemory(addr)
		if err != nil || !isLikelyM6502EntryOpcode(op) {
			continue
		}
		if !dis.validateCodeSequence(addr) {
			continue
		}
		dis.AddAddressToParse(addr, addr, 0, nil, false)
	}
}

func isLikelyM6502EntryOpcode(op byte) bool {
	switch op {
	case 0x4C, // JMP abs
		0x20, // JSR abs
		0x78, // SEI
		0x58, // CLI
		0xD8, // CLD
		0xF8, // SED
		0x18, // CLC
		0x38, // SEC
		0xA2, // LDX #
		0xA0, // LDY #
		0xA9, // LDA #
		0xEA: // NOP
		return true
	default:
		return false
	}
}

// seedLikelyMappedBankCallTargets scans already-classified code in the currently
// mapped bank for absolute JSR/JMP targets and queues targets that look like
// routine entry points. Only addresses already marked as CodeOffset are scanned,
// preventing data bytes ($20/$4C) from being misinterpreted as JSR/JMP opcodes.
func (dis *Disasm) seedLikelyMappedBankCallTargets() {
	const maxCandidatesPerBank = 2048

	start := dis.codeBaseAddress
	end := dis.arch.LastCodeAddress()
	if end <= start+2 {
		return
	}

	queued := 0
	for addr := start; addr < end-2; addr++ {
		if queued >= maxCandidatesPerBank {
			break
		}

		target, ok := dis.extractCallTarget(addr, end)
		if !ok {
			continue
		}

		dis.AddAddressToParse(target, target, addr, nil, false)
		queued++
	}
}

// extractCallTarget checks if addr contains a JSR/JMP instruction in already-classified
// code and returns the validated target address. Only scans bytes already marked as
// CodeOffset, preventing data bytes from being misinterpreted as JSR/JMP opcodes.
func (dis *Disasm) extractCallTarget(addr, end uint16) (uint16, bool) {
	offsetInfo := dis.mapper.OffsetInfo(addr)
	if !offsetInfo.IsType(program.CodeOffset) {
		return 0, false
	}

	op, err := dis.ReadMemory(addr)
	if err != nil {
		return 0, false
	}
	if op != 0x20 && op != 0x4C { // JSR abs / JMP abs
		return 0, false
	}

	low, err := dis.ReadMemory(addr + 1)
	if err != nil {
		return 0, false
	}
	high, err := dis.ReadMemory(addr + 2)
	if err != nil {
		return 0, false
	}
	target := uint16(high)<<8 | uint16(low)
	if !dis.isValidCodeAddress(target) || target > end {
		return 0, false
	}

	targetOp, err := dis.ReadMemory(target)
	if err != nil || !isLikelyM6502RoutineStartOpcode(targetOp) {
		return 0, false
	}
	if !dis.validateCodeSequence(target) {
		return 0, false
	}

	return target, true
}

// seedLikelyMappedBankPointerTableTargets scans for contiguous little-endian
// pointer tables in mapped banks and seeds targets that look like routine
// starts. This is intentionally guarded to avoid exploding false positives.
func (dis *Disasm) seedLikelyMappedBankPointerTableTargets() {
	const (
		maxSeedsPerBank    = 1024
		maxRunEntries      = 64
		minRunEntries      = 4
		minDistinctTargets = 3
	)

	start := dis.codeBaseAddress
	end := dis.arch.LastCodeAddress()
	if end <= start+1 {
		return
	}

	seeded := 0
	seededTargets := map[uint16]struct{}{}

	for addr := start; addr < end-1 && seeded < maxSeedsPerBank; addr++ {
		// Conservative shape guard: pointer tables are 16-bit words.
		if addr&1 != 0 {
			continue
		}
		if dis.isPointerTableAddressBlocked(addr) {
			continue
		}

		targets := dis.scanPointerTableRun(addr, end, maxRunEntries)
		if len(targets) < minRunEntries || !hasLikelyPointerTableShape(targets, minDistinctTargets) {
			continue
		}

		seeded += dis.seedPointerTableTargets(targets, addr, &seededTargets, maxSeedsPerBank-seeded)

		// Skip across the accepted table run.
		addr += uint16(len(targets)*2 - 1)
	}
}

// isPointerTableAddressBlocked returns true if either byte at addr or addr+1 is already code.
func (dis *Disasm) isPointerTableAddressBlocked(addr uint16) bool {
	return dis.mapper.OffsetInfo(addr).IsType(program.CodeOffset) ||
		dis.mapper.OffsetInfo(addr+1).IsType(program.CodeOffset)
}

// scanPointerTableRun scans for a contiguous run of valid pointer table entries starting at addr.
func (dis *Disasm) scanPointerTableRun(addr, end uint16, maxRunEntries int) []uint16 {
	targets := make([]uint16, 0, maxRunEntries)
	for entryAddr := addr; entryAddr < end-1 && len(targets) < maxRunEntries; entryAddr += 2 {
		if dis.mapper.OffsetInfo(entryAddr).IsType(program.CodeOffset) ||
			dis.mapper.OffsetInfo(entryAddr+1).IsType(program.CodeOffset) {

			break
		}

		low, err := dis.ReadMemory(entryAddr)
		if err != nil {
			break
		}
		high, err := dis.ReadMemory(entryAddr + 1)
		if err != nil {
			break
		}

		target := uint16(high)<<8 | uint16(low)
		if !dis.isValidCodeAddress(target) || target > end {
			break
		}

		targetOp, err := dis.ReadMemory(target)
		if err != nil || !isLikelyM6502RoutineStartOpcode(targetOp) {
			break
		}

		targets = append(targets, target)
	}
	return targets
}

// seedPointerTableTargets queues unique targets from a validated pointer table.
// Returns the number of newly seeded targets.
func (dis *Disasm) seedPointerTableTargets(
	targets []uint16, fromAddr uint16, seededTargets *map[uint16]struct{}, budget int,
) int {

	seeded := 0
	for _, target := range targets {
		if seeded >= budget {
			break
		}
		if _, ok := (*seededTargets)[target]; ok {
			continue
		}
		if !dis.validateCodeSequence(target) {
			continue
		}
		dis.AddAddressToParse(target, target, fromAddr, nil, false)
		(*seededTargets)[target] = struct{}{}
		seeded++
	}
	return seeded
}

func hasLikelyPointerTableShape(targets []uint16, minDistinct int) bool {
	if len(targets) == 0 {
		return false
	}

	distinctTargets := map[uint16]struct{}{}
	closeNeighborPairs := 0
	for i := range targets {
		distinctTargets[targets[i]] = struct{}{}
		if i == 0 {
			continue
		}
		prev := targets[i-1]
		curr := targets[i]
		var delta uint16
		if curr > prev {
			delta = curr - prev
		} else {
			delta = prev - curr
		}
		// Typical callback/jump-table targets are often grouped in nearby regions.
		if delta <= 0x0100 {
			closeNeighborPairs++
		}
	}

	return len(distinctTargets) >= minDistinct && closeNeighborPairs > 0
}

// seedLikelyMappedBankSplitPointerTableTargets looks for nearby pairs of indexed
// absolute table loads (tbl_lo,tbl_hi style) and seeds routine targets derived
// from combining low/high byte tables. Detection is heavily bounded and shape
// guarded to avoid aggressive data-to-code promotion.
func (dis *Disasm) seedLikelyMappedBankSplitPointerTableTargets() {
	const (
		maxSeedsPerBank      = 768
		maxPairsPerBank      = 256
		loadPairLookahead    = 16
		correlationLookahead = 80
		maxRunEntries        = 64
		minRunEntries        = 1
		minDistinctTargets   = 1
		maxTableByteDistance = 0x0200
	)

	start := dis.codeBaseAddress
	end := dis.arch.LastCodeAddress()
	if end <= start+5 {
		return
	}

	seeded := 0
	pairs := 0
	seededTargets := map[uint16]struct{}{}

	for pc := start; pc < end-5 && seeded < maxSeedsPerBank && pairs < maxPairsPerBank; pc++ {
		firstOp, firstTable, ok := dis.readSplitLoadOpAndTable(pc, end)
		if !ok {
			continue
		}

		paired, count := dis.trySplitPointerPair(pc, firstOp, firstTable, end,
			loadPairLookahead, correlationLookahead, maxTableByteDistance,
			maxRunEntries, minRunEntries, minDistinctTargets,
			maxSeedsPerBank-seeded, &seededTargets)

		if paired {
			seeded += count
			pairs++
			pc += 2
		}
	}
}

// readSplitLoadOpAndTable reads the opcode and table address at pc, returning false if not a valid split load.
func (dis *Disasm) readSplitLoadOpAndTable(pc, end uint16) (byte, uint16, bool) {
	op, err := dis.ReadMemory(pc)
	if err != nil || !isLikelySplitPointerLoadOpcode(op) {
		return 0, 0, false
	}
	table, ok := dis.readWordAt(pc + 1)
	if !ok || !dis.isLikelySplitTableAddress(table, end) {
		return 0, 0, false
	}
	return op, table, true
}

// trySplitPointerPair searches for a compatible second load instruction and attempts to extract targets.
// Returns (paired, seededCount).
func (dis *Disasm) trySplitPointerPair(
	pc uint16, firstOp byte, firstTable, end uint16,
	loadPairLookahead, correlationLookahead, maxTableByteDistance uint16,
	maxRunEntries, minRunEntries, minDistinctTargets, budget int,
	seededTargets *map[uint16]struct{},
) (bool, int) {

	for delta := uint16(3); delta <= loadPairLookahead && pc+delta+2 <= end; delta++ {
		secondPC := pc + delta
		secondTable, ok := dis.readCompatibleSecondLoad(pc, secondPC, firstOp, firstTable, end, maxTableByteDistance)
		if !ok {
			continue
		}

		targets, extracted := dis.tryExtractSplitPairTargets(pc, secondPC, firstTable, secondTable, end,
			correlationLookahead, maxRunEntries, minRunEntries, minDistinctTargets)
		if !extracted {
			continue
		}

		count := dis.seedSplitPairTargets(targets, pc, seededTargets, budget)
		return true, count
	}
	return false, 0
}

// readCompatibleSecondLoad reads the second instruction of a split pointer load pair.
// Returns the second table address and false if invalid or incompatible.
func (dis *Disasm) readCompatibleSecondLoad(
	firstPC, secondPC uint16, firstOp byte, firstTable, end uint16, maxTableByteDistance uint16,
) (uint16, bool) {

	secondOp, err := dis.ReadMemory(secondPC)
	if err != nil || !isCompatibleSplitPointerLoadPair(firstOp, secondOp) {
		return 0, false
	}
	secondTable, ok := dis.readWordAt(secondPC + 1)
	if !ok || !dis.isLikelySplitTableAddress(secondTable, end) || secondTable == firstTable {
		return 0, false
	}
	if tableByteDistance(firstTable, secondTable) > maxTableByteDistance {
		return 0, false
	}
	_ = firstPC // used by caller for correlation check
	return secondTable, true
}

// tryExtractSplitPairTargets checks coherence and correlation then extracts targets from the pair.
func (dis *Disasm) tryExtractSplitPairTargets(
	firstPC, secondPC uint16, firstTable, secondTable, end uint16,
	correlationLookahead uint16, maxRunEntries, minRunEntries, minDistinctTargets int,
) ([]uint16, bool) {

	plausibleForward := dis.hasSplitTableTargetWindowCoherence(firstTable, secondTable, end)
	plausibleReverse := dis.hasSplitTableTargetWindowCoherence(secondTable, firstTable, end)
	if !plausibleForward && !plausibleReverse {
		dis.stats.splitSeedRejectedPlaus++
		return nil, false
	}

	dis.stats.splitSeedCandidatePairs++
	if !dis.hasSplitPointerRuntimeCorrelation(firstPC, secondPC, end, correlationLookahead) {
		dis.stats.splitSeedRejectedByCorr++
		return nil, false
	}

	var targets []uint16
	extracted := false
	if plausibleForward {
		targets, extracted = dis.extractSplitPointerTargets(
			firstTable, secondTable, end, maxRunEntries, minRunEntries, minDistinctTargets)
	}
	if !extracted && plausibleReverse {
		targets, extracted = dis.extractSplitPointerTargets(
			secondTable, firstTable, end, maxRunEntries, minRunEntries, minDistinctTargets)
	}
	if !extracted {
		dis.stats.splitSeedRejectedExtract++
		return nil, false
	}
	return targets, true
}

// seedSplitPairTargets queues unique targets from an extracted split pointer table.
// Returns the number of newly seeded targets.
func (dis *Disasm) seedSplitPairTargets(
	targets []uint16, pc uint16, seededTargets *map[uint16]struct{}, budget int,
) int {

	seeded := 0
	for _, target := range targets {
		if seeded >= budget {
			break
		}
		if _, exists := (*seededTargets)[target]; exists {
			continue
		}
		if !dis.validateCodeSequence(target) {
			continue
		}
		dis.AddAddressToParse(target, target, pc, nil, false)
		(*seededTargets)[target] = struct{}{}
		seeded++
		dis.stats.splitSeedAcceptedTargets++
	}
	return seeded
}

// hasSplitTableTargetWindowCoherence performs a cheap prefilter on split table
// pairs before opcode checks: keep pairs where sampled pointer words mostly land
// in plausible code windows with limited page spread.
func (dis *Disasm) hasSplitTableTargetWindowCoherence(lowTable, highTable, end uint16) bool {
	const (
		sampleEntries = 16
		minValid      = 1
		maxPages      = 16
	)

	pages := map[byte]struct{}{}
	valid := 0

	for i := range sampleEntries {
		lowAddr32 := uint32(lowTable) + uint32(i)
		highAddr32 := uint32(highTable) + uint32(i)
		if lowAddr32 > uint32(end) || highAddr32 > uint32(end) {
			break
		}

		low, err := dis.ReadMemory(uint16(lowAddr32))
		if err != nil {
			break
		}
		high, err := dis.ReadMemory(uint16(highAddr32))
		if err != nil {
			break
		}

		target := uint16(high)<<8 | uint16(low)
		if !dis.isValidCodeAddress(target) || target > end {
			continue
		}

		valid++
		pages[byte(target>>8)] = struct{}{}
		if len(pages) > maxPages {
			return false
		}
	}

	return valid >= minValid
}

// correlationState collects address sets during a split pointer correlation window scan.
type correlationState struct {
	stores                   map[uint16]struct{}
	indexedStores            map[uint16]struct{}
	pointerByteOps           map[uint16]struct{}
	jumpPointers             map[uint16]struct{}
	indirectZPRefs           map[uint16]struct{}
	transferArithmeticSignal bool
	phaCount                 int
	stackDispatch            bool
}

func newCorrelationState() correlationState {
	return correlationState{
		stores:         map[uint16]struct{}{},
		indexedStores:  map[uint16]struct{}{},
		pointerByteOps: map[uint16]struct{}{},
		jumpPointers:   map[uint16]struct{}{},
		indirectZPRefs: map[uint16]struct{}{},
	}
}

func (dis *Disasm) hasSplitPointerRuntimeCorrelation(firstPC, secondPC, end, lookahead uint16) bool {
	windowStart := firstPC
	if secondPC < firstPC {
		windowStart = secondPC
	}
	windowEnd := secondPC + lookahead
	if windowEnd > end {
		windowEnd = end
	}

	state := newCorrelationState()
	dis.scanCorrelationWindow(windowStart, windowEnd, &state)
	if !checkCorrelationSignals(&state) {
		return false
	}
	if state.stackDispatch {
		dis.stats.splitSeedAcceptedStackDisp++
	}
	return true
}

// scanCorrelationWindow iterates instructions in the correlation window and populates state.
func (dis *Disasm) scanCorrelationWindow(windowStart, windowEnd uint16, state *correlationState) {
	for pc := windowStart; pc <= windowEnd; {
		opByte, err := dis.ReadMemory(pc)
		if err != nil {
			break
		}
		opcode := cpum6502.Opcodes[opByte]
		size := opcodeSizeBytes(opcode)
		if size <= 0 {
			size = 1
		}
		if pc+uint16(size)-1 > windowEnd {
			break
		}
		dis.collectCorrelationOp(pc, opByte, opcode, state)
		pc += uint16(size)
	}
}

// collectCorrelationOp records address-set membership for a single instruction.
func (dis *Disasm) collectCorrelationOp(pc uint16, opByte byte, opcode cpum6502.Opcode, state *correlationState) {
	dis.collectCorrelationStoreOp(pc, opByte, opcode, state)
	if isPointerArithmeticZPOpcode(opByte) {
		if addr, ok := dis.readByteWordOperand(pc+1, 1); ok {
			state.pointerByteOps[addr] = struct{}{}
		}
	}
	if isPointerTransferArithmeticSignalOpcode(opByte) {
		state.transferArithmeticSignal = true
	}
	switch opByte {
	case 0x48: // PHA
		state.phaCount++
	case 0x60: // RTS
		if state.phaCount >= 2 {
			state.stackDispatch = true
		}
	}
}

// collectCorrelationStoreOp records store, jump pointer, and indirect ZP reference addresses.
func (dis *Disasm) collectCorrelationStoreOp(pc uint16, opByte byte, opcode cpum6502.Opcode, state *correlationState) {
	switch opByte {
	case 0x85, // STA zp
		0x86, // STX zp
		0x84: // STY zp
		if addr, ok := dis.readByteWordOperand(pc+1, 1); ok {
			state.stores[addr] = struct{}{}
		}
	case 0x95, // STA zp,X
		0x94, // STY zp,X
		0x96: // STX zp,Y
		if addr, ok := dis.readByteWordOperand(pc+1, 1); ok {
			state.indexedStores[addr] = struct{}{}
		}
	case 0x8D, // STA abs
		0x8E, // STX abs
		0x8C: // STY abs
		if addr, ok := dis.readByteWordOperand(pc+1, 2); ok {
			state.stores[addr] = struct{}{}
		}
	case 0x6C: // JMP (abs)
		if addr, ok := dis.readByteWordOperand(pc+1, 2); ok {
			state.jumpPointers[addr] = struct{}{}
		}
	default:
		dis.collectIndirectZPRef(pc, opcode, state)
	}
}

// collectIndirectZPRef records indirect zero-page references from indirect-X or indirect-Y addressing.
func (dis *Disasm) collectIndirectZPRef(pc uint16, opcode cpum6502.Opcode, state *correlationState) {
	if opcode.Instruction == nil {
		return
	}
	if opcode.Addressing != cpum6502.IndirectXAddressing &&
		opcode.Addressing != cpum6502.IndirectYAddressing {

		return
	}
	if addr, ok := dis.readByteWordOperand(pc+1, 1); ok {
		state.indirectZPRefs[addr] = struct{}{}
	}
}

// checkCorrelationSignals evaluates the collected address sets for split pointer correlation evidence.
func checkCorrelationSignals(state *correlationState) bool {
	// Stack dispatch: LDA tbl_lo,X / PHA / LDA tbl_hi,X / PHA / RTS
	// is a common NES dispatch pattern that builds the target address on
	// the stack and returns to it. No stores or indirect refs are needed.
	if state.stackDispatch {
		return true
	}

	for base := range state.stores {
		if base == 0xFFFF {
			continue
		}
		if _, ok := state.stores[base+1]; !ok {
			continue
		}
		if _, ok := state.jumpPointers[base]; ok {
			return true
		}
		if base <= 0x00FF {
			if _, ok := state.indirectZPRefs[base]; ok {
				return true
			}
		}
	}

	// Variant path: pointer bytes can be built via indexed stores and/or
	// arithmetic/transfer sequences. Keep strict indirect-use confirmation.
	if len(state.indirectZPRefs) == 0 && len(state.jumpPointers) == 0 {
		return false
	}

	switch {
	case hasConsecutiveAddressPair(state.indexedStores):
		return true
	case state.transferArithmeticSignal && hasConsecutiveAddressPair(state.pointerByteOps):
		return true
	default:
		return false
	}
}

// splitEntryResult describes what happened when evaluating a split table entry.
type splitEntryResult int

const (
	splitEntryAccept      splitEntryResult = iota // append target and continue
	splitEntryAcceptBreak                         // append target and stop
	splitEntrySkip                                // entry rejected, track skip counters
	splitEntryStop                                // stop immediately (bounds/read error)
)

func (dis *Disasm) extractSplitPointerTargets(lowTable, highTable, end uint16,
	maxRunEntries, minRunEntries, minDistinctTargets int) ([]uint16, bool) {

	const maxLeadingSkips = 8
	const maxTrailingSkips = 2

	targets := make([]uint16, 0, minRunEntries)
	leadingSkips := 0
	trailingSkips := 0
	started := false

	for i := range maxRunEntries {
		target, result := dis.evalSplitTableEntry(lowTable, highTable, end, i, started, len(targets) == 0)
		if result == splitEntryStop {
			break
		}
		if result == splitEntryAcceptBreak {
			targets = append(targets, target)
			break
		}
		if result == splitEntrySkip {
			stop := dis.updateSplitSkipCounters(&leadingSkips, &trailingSkips, started,
				maxLeadingSkips, maxTrailingSkips)
			if stop {
				break
			}
			continue
		}
		// splitEntryAccept
		targets = append(targets, target)
		started = true
		trailingSkips = 0
	}

	if len(targets) < minRunEntries {
		return nil, false
	}
	if len(targets) == 1 {
		return targets, true
	}
	if hasLikelyPointerTableShape(targets, minDistinctTargets) {
		return targets, true
	}
	if hasSplitTargetCodeEvidence(dis, targets) {
		return targets, true
	}
	dis.stats.splitSeedRejectShape++
	return nil, false
}

// evalSplitTableEntry reads and classifies a single split table entry at index i.
func (dis *Disasm) evalSplitTableEntry(
	lowTable, highTable, end uint16, i int, started, isEmpty bool,
) (uint16, splitEntryResult) {

	lowAddr32 := uint32(lowTable) + uint32(i)
	highAddr32 := uint32(highTable) + uint32(i)
	if lowAddr32 > uint32(end) || highAddr32 > uint32(end) {
		return 0, splitEntryStop
	}
	lowAddr := uint16(lowAddr32)
	highAddr := uint16(highAddr32)

	sourceLooksLikeCode := dis.mapper.OffsetInfo(lowAddr).IsType(program.CodeOffset) ||
		dis.mapper.OffsetInfo(highAddr).IsType(program.CodeOffset)

	low, err := dis.ReadMemory(lowAddr)
	if err != nil {
		return 0, splitEntryStop
	}
	high, err := dis.ReadMemory(highAddr)
	if err != nil {
		return 0, splitEntryStop
	}

	target := uint16(high)<<8 | uint16(low)
	if !dis.isValidCodeAddress(target) || target > end || sourceLooksLikeCode {
		dis.stats.splitSeedRejectInvalid++
		return 0, splitEntrySkip
	}

	return target, dis.classifySplitTarget(target, end, started, isEmpty)
}

// classifySplitTarget determines whether a valid target address should be accepted or rejected.
func (dis *Disasm) classifySplitTarget(target, end uint16, started, isEmpty bool) splitEntryResult {
	targetOp, err := dis.ReadMemory(target)
	if err != nil {
		dis.stats.splitSeedRejectOpcode++
		dis.stats.splitSeedRejectOpcodeRead++
		return splitEntrySkip
	}

	if isLikelyM6502RoutineStartOpcode(targetOp) {
		return splitEntryAccept
	}

	if started && isWeakSplitEntryTerminatorOpcode(targetOp) {
		dis.stats.splitSeedAcceptedMidRunRT++
		return splitEntryAccept
	}

	if isEmpty && !started && dis.isWeakSplitEntryCandidate(target) {
		dis.stats.splitSeedAcceptedWeak++
		return splitEntryAcceptBreak
	}
	if isEmpty && !started && isWeakSplitEntryTerminatorOpcode(targetOp) &&
		dis.hasAdjacentSplitEntryEvidence(target, end) {

		dis.stats.splitSeedAcceptedWeak++
		dis.stats.splitSeedAcceptedWeakRT++
		return splitEntryAcceptBreak
	}

	dis.stats.splitSeedRejectOpcode++
	dis.recordSplitOpcodeGateReject(targetOp)
	return splitEntrySkip
}

// updateSplitSkipCounters increments the appropriate skip counter and returns true if the loop should stop.
func (dis *Disasm) updateSplitSkipCounters(
	leadingSkips, trailingSkips *int, started bool, maxLeading, maxTrailing int,
) bool {

	if started {
		*trailingSkips++
		return *trailingSkips > maxTrailing
	}
	*leadingSkips++
	return *leadingSkips > maxLeading
}

func hasSplitTargetCodeEvidence(dis *Disasm, targets []uint16) bool {
	for _, target := range targets {
		offsetInfo := dis.mapper.OffsetInfo(target)
		if offsetInfo.IsType(program.CodeOffset) || offsetInfo.IsType(program.CallDestination) {
			return true
		}
	}
	return false
}

func (dis *Disasm) isWeakSplitEntryCandidate(target uint16) bool {
	offsetInfo := dis.mapper.OffsetInfo(target)
	return offsetInfo.IsType(
		program.CodeOffset |
			program.CallDestination |
			program.FunctionReference |
			program.JumpEngine,
	)
}

func (dis *Disasm) hasAdjacentSplitEntryEvidence(target, end uint16) bool {
	const radius = 16

	start := int(target) - radius
	if start < int(dis.codeBaseAddress) {
		start = int(dis.codeBaseAddress)
	}
	stop := int(target) + radius
	if stop > int(end) {
		stop = int(end)
	}
	minDelta := radius + 1
	for addr := start; addr <= stop; addr++ {
		if uint16(addr) == target {
			continue
		}
		offsetInfo := dis.mapper.OffsetInfo(uint16(addr))
		if !offsetInfo.IsType(
			program.CodeOffset |
				program.CallDestination |
				program.FunctionReference |
				program.JumpEngine,
		) {

			continue
		}
		delta := addr - int(target)
		if delta < 0 {
			delta = -delta
		}
		if delta < minDelta {
			minDelta = delta
		}
	}

	if minDelta > radius {
		dis.stats.splitSeedAdjacentEvidenceMiss++
		return false
	}

	dis.stats.splitSeedAdjacentEvidenceFound++
	d := uint64(minDelta)
	if dis.stats.splitSeedAdjacentEvidenceMin == 0 || d < dis.stats.splitSeedAdjacentEvidenceMin {
		dis.stats.splitSeedAdjacentEvidenceMin = d
	}
	return true
}

func (dis *Disasm) recordSplitOpcodeGateReject(op byte) {
	opcode := cpum6502.Opcodes[op]
	if opcode.Instruction == nil {
		dis.stats.splitSeedRejectOpcodeInv++
		return
	}
	if opcode.Instruction.Unofficial {
		dis.stats.splitSeedRejectOpcodeUno++
		return
	}
	switch opcode.Instruction.Name {
	case cpum6502.BrkName:
		dis.stats.splitSeedRejectOpcodeBRK++
	case cpum6502.RtsName:
		dis.stats.splitSeedRejectOpcodeRTS++
	case cpum6502.RtiName:
		dis.stats.splitSeedRejectOpcodeRTI++
	default:
		dis.stats.splitSeedRejectOpcodeOth++
	}
}

func (dis *Disasm) readWordAt(address uint16) (uint16, bool) {
	low, err := dis.ReadMemory(address)
	if err != nil {
		return 0, false
	}
	high, err := dis.ReadMemory(address + 1)
	if err != nil {
		return 0, false
	}
	return uint16(high)<<8 | uint16(low), true
}

func (dis *Disasm) readByteWordOperand(address uint16, size int) (uint16, bool) {
	switch size {
	case 1:
		value, err := dis.ReadMemory(address)
		if err != nil {
			return 0, false
		}
		return uint16(value), true
	case 2:
		return dis.readWordAt(address)
	default:
		return 0, false
	}
}

func (dis *Disasm) isLikelySplitTableAddress(address, end uint16) bool {
	if !dis.isValidCodeAddress(address) || address > end {
		return false
	}
	return !dis.mapper.OffsetInfo(address).IsType(program.CodeOffset)
}

func isLikelySplitPointerLoadOpcode(op byte) bool {
	return splitPointerIndexMode(op) != 0
}

func isCompatibleSplitPointerLoadPair(first, second byte) bool {
	firstIndexMode := splitPointerIndexMode(first)
	secondIndexMode := splitPointerIndexMode(second)
	return firstIndexMode != 0 && firstIndexMode == secondIndexMode
}

func splitPointerIndexMode(op byte) byte {
	switch op {
	case 0xBD, // LDA abs,X
		0xBC: // LDY abs,X
		return 'X'
	case 0xB9, // LDA abs,Y
		0xBE: // LDX abs,Y
		return 'Y'
	default:
		return 0
	}
}

func opcodeSizeBytes(opcode cpum6502.Opcode) int {
	if opcode.Instruction == nil {
		return 1
	}
	opcodeInfo, ok := opcode.Instruction.Addressing[opcode.Addressing]
	if !ok || opcodeInfo.Size == 0 {
		return 1
	}
	return int(opcodeInfo.Size)
}

func hasConsecutiveAddressPair(addrs map[uint16]struct{}) bool {
	for base := range addrs {
		if base == 0xFFFF {
			continue
		}
		if _, ok := addrs[base+1]; ok {
			return true
		}
	}
	return false
}

func isPointerArithmeticZPOpcode(op byte) bool {
	switch op {
	case 0xE6, // INC zp
		0xF6, // INC zp,X
		0xC6, // DEC zp
		0xD6: // DEC zp,X
		return true
	default:
		return false
	}
}

func isPointerTransferArithmeticSignalOpcode(op byte) bool {
	switch op {
	case 0xAA, // TAX
		0xA8, // TAY
		0x8A, // TXA
		0x98, // TYA
		0xE8, // INX
		0xC8, // INY
		0xCA, // DEX
		0x88, // DEY
		0x18, // CLC
		0x38, // SEC
		0x69, // ADC #
		0xE9, // SBC #
		0x65, // ADC zp
		0x75, // ADC zp,X
		0xE5, // SBC zp
		0xF5: // SBC zp,X
		return true
	default:
		return false
	}
}

func tableByteDistance(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

func isLikelyM6502RoutineStartOpcode(op byte) bool {
	opcode := cpum6502.Opcodes[op]
	if opcode.Instruction == nil || opcode.Instruction.Unofficial {
		return false
	}
	switch opcode.Instruction.Name {
	case cpum6502.BrkName, cpum6502.RtiName, cpum6502.RtsName:
		return false
	default:
		return true
	}
}

// validateCodeSequence checks if the bytes at the given address form a plausible
// code sequence by decoding the first several instructions. Returns false if any
// decoded instruction is unofficial, invalid, or the sequence is too short.
// This catches data regions where byte values quickly decode to unofficial opcodes.
// BRK (0x00) is explicitly rejected because it is the most common byte in data
// regions and is virtually never used intentionally in NES game code.
func (dis *Disasm) validateCodeSequence(addr uint16) bool {
	return dis.validateCodeSequenceN(addr, 3)
}

// validateCodeSequenceStrict performs a stricter validation requiring more
// consecutive valid instructions. Used for cross-bank call targets where
// the risk of data-as-code false positives is higher.
func (dis *Disasm) validateCodeSequenceStrict(addr uint16) bool {
	return dis.validateCodeSequenceN(addr, 6)
}

func (dis *Disasm) validateCodeSequenceN(addr uint16, minInstructions int) bool {
	end := dis.arch.LastCodeAddress()
	validCount := 0

	for pc := addr; validCount < minInstructions; {
		if pc > end {
			return false
		}

		op, err := dis.ReadMemory(pc)
		if err != nil {
			return false
		}

		opcode := cpum6502.Opcodes[op]
		if opcode.Instruction == nil || opcode.Instruction.Unofficial {
			return false
		}

		// BRK (0x00) is extremely common in data regions (null bytes) and
		// virtually never used intentionally in NES game code. Reject any
		// sequence containing BRK to avoid false code classification.
		if opcode.Instruction.Name == cpum6502.BrkName {
			return false
		}

		size := opcodeSizeBytes(opcode)
		if size <= 0 {
			return false
		}

		validCount++

		// An unconditional control flow instruction (JMP/RTS/RTI) is a valid
		// end for a short trampoline or stub routine.
		if _, ok := cpum6502.NotExecutingFollowingOpcodeInstructions[opcode.Instruction.Name]; ok {
			break
		}

		pc += uint16(size)
	}

	return validCount >= minInstructions
}

func isWeakSplitEntryTerminatorOpcode(op byte) bool {
	opcode := cpum6502.Opcodes[op]
	if opcode.Instruction == nil {
		return false
	}
	switch opcode.Instruction.Name {
	case cpum6502.RtsName, cpum6502.RtiName:
		return true
	default:
		return false
	}
}
