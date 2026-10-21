package m6502

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
	"github.com/retroenv/retrogolib/set"
)

const (
	mappingCallDepth      = 16
	mappingLocationLimit  = 100000
	mappingDiscoveryLimit = 64

	prgStartAddress = 0x8000
	windowSize      = 0x2000
)

var interruptVectors = []uint16{
	cpu6502.NMIAddress,
	cpu6502.ResetAddress,
	cpu6502.IrqAddress,
}

// flowLocation is a program counter in one mapper state, including the call stack.
type flowLocation struct {
	state   mapper.Snapshot
	pc      uint16
	returns [mappingCallDepth]uint16
	depth   int
	branch  bool
}

type flowWork struct {
	location  flowLocation
	registers registers
}

// mappingTrace collects the results for all states that share one CPU mapping.
type mappingTrace struct {
	state        mapper.Snapshot
	entries      set.Set[uint16]
	stops        set.Set[uint16]
	staticStores set.Set[uint16]
}

type mappingExplorer struct {
	mem              *mapper.Mapper
	hints            mapper.ExplorationHints
	stopAtUnofficial bool

	seen         map[flowLocation]registers
	traces       map[mapper.Mapping]*mappingTrace
	order        []mapper.Mapping
	pending      []flowWork
	limited      bool
	mappingLimit int
}

func (ex *mappingExplorer) explore(ctx context.Context) error {
	for len(ex.pending) > 0 && !ex.limited {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("exploring mapper states: %w", err)
		}

		work := ex.pending[len(ex.pending)-1]
		ex.pending = ex.pending[:len(ex.pending)-1]
		ex.step(work)
	}

	return nil
}

func (ex *mappingExplorer) step(work flowWork) {
	loc := work.location

	ins, ok := readFlowInstruction(ex.mem, loc.state, loc.pc)
	if !ok {
		return
	}

	hint := ex.codeHint(loc.pc)
	if ex.stopAtUnofficial && stopAtUnofficialInstruction(ins.op.Instruction, loc.branch, hint) {
		return
	}

	next := loc
	next.pc += uint16(ins.size)
	next.branch = false

	if address, value, known := ins.store(work.registers); known {
		if _, handled := ex.mem.Write(loc.state, address, value.value); handled {
			ex.followWrite(loc, next, work.registers, address, value)
			return
		}
	}

	switch ins.op.Instruction.Name {
	case cpu6502.StaName, cpu6502.StxName, cpu6502.StyName:
		trace := ex.trace(loc.state)
		trace.staticStores.Add(loc.pc)

		if trace.stops.Contains(loc.pc) {
			trace.entries.Add(next.pc)
		}
	}

	regs := ins.apply(ex.mem, loc.state, work.registers)
	ex.followInstruction(loc, next, regs, ins)
}

func (ex *mappingExplorer) followInstruction(loc, next flowLocation, regs registers, ins flowInstruction) {
	switch ins.op.Instruction.Name {
	case cpu6502.JsrName:
		if loc.depth == mappingCallDepth {
			// The ordinary tracer still decodes the recursive function body.
			// Its caller's continuation has no proven register values.
			ex.enqueue(next, registers{})
			return
		}

		next.returns[next.depth] = next.pc
		next.depth++
		next.pc = ins.operand
		next.branch = true
		ex.enqueue(next, regs)

	case cpu6502.RtsName:
		if next.depth > 0 {
			next.depth--
			next.pc = next.returns[next.depth]
			next.returns[next.depth] = 0
			ex.trace(next.state).entries.Add(next.pc)
			ex.enqueue(next, regs)
		}

	case cpu6502.JmpName:
		if ins.op.Addressing == cpu6502.AbsoluteAddressing {
			next.pc = ins.operand
			next.branch = true
			ex.enqueue(next, regs)
		}

	case cpu6502.RtiName, cpu6502.BrkName, cpu6502.KilName:
		return

	default:
		if ins.op.Addressing == cpu6502.RelativeAddressing {
			branch := next
			branch.pc += uint16(int16(int8(ins.operand)))
			branch.branch = true
			ex.enqueue(branch, regs)
		}

		ex.enqueue(next, regs)
	}
}

func (ex *mappingExplorer) followWrite(loc, next flowLocation, regs registers, address uint16, value knownByte) {
	trace := ex.trace(loc.state)
	trace.stops.Add(loc.pc)

	// An indexed store can reach RAM on another path. Keep that continuation
	// in the current mapping as well as the mapper write's successors.
	if trace.staticStores.Contains(loc.pc) {
		trace.entries.Add(next.pc)
	}

	start, end := 0, 256
	if value.known {
		start, end = int(value.value), int(value.value)+1
	}

	seen := set.New[mapper.Snapshot]()

	for candidate := start; candidate < end; candidate++ {
		for state := range ex.mem.WriteCandidates(loc.state, address, byte(candidate)) {
			if seen.Contains(state) {
				continue
			}
			seen.Add(state)

			target := ex.trace(state)
			if target == nil {
				return
			}

			next.state = state
			target.entries.Add(next.pc)
			ex.enqueue(next, regs)
		}
	}
}

func (ex *mappingExplorer) enqueue(loc flowLocation, regs registers) {
	if !ex.isExecutable(loc.pc) {
		return
	}

	if previous, ok := ex.seen[loc]; ok {
		regs = joinRegisters(previous, regs)
		if regs == previous {
			return
		}
	}

	if len(ex.seen) >= mappingLocationLimit {
		ex.limited = true
		return
	}

	ex.seen[loc] = regs
	ex.pending = append(ex.pending, flowWork{
		location:  loc,
		registers: regs,
	})
}

// isExecutable reports whether an address can hold code that the exploration follows.
func (ex *mappingExplorer) isExecutable(address uint16) bool {
	if address < prgStartAddress || address >= cpu6502.InterruptVectorStartAddress {
		return false
	}

	return ex.codeHint(address) != program.DataOffset
}

// trace returns the trace of the mapping that the state belongs to. It returns
// nil and marks the exploration as limited when the mapping limit is reached.
func (ex *mappingExplorer) trace(state mapper.Snapshot) *mappingTrace {
	key := ex.mem.Mapping(state)
	if trace, ok := ex.traces[key]; ok {
		return trace
	}

	if ex.mappingLimit > 0 && len(ex.traces) >= ex.mappingLimit {
		ex.limited = true
		return nil
	}

	trace := &mappingTrace{
		state:        state,
		entries:      set.New[uint16](),
		stops:        set.New[uint16](),
		staticStores: set.New[uint16](),
	}
	ex.traces[key] = trace
	ex.order = append(ex.order, key)
	ex.seedHints(state, key)

	return trace
}

func (ex *mappingExplorer) codeHint(address uint16) program.OffsetType {
	if ex.hints.CodeHint == nil {
		return program.UnknownOffset
	}

	return ex.hints.CodeHint(address)
}

// seedInterruptVectors adds the interrupt handlers of the state as entry points.
func (ex *mappingExplorer) seedInterruptVectors(state mapper.Snapshot) {
	trace := ex.trace(state)

	for _, vector := range interruptVectors {
		low, _ := ex.mem.ReadSnapshot(state, vector)
		high, _ := ex.mem.ReadSnapshot(state, vector+1)
		entry := uint16(low) | uint16(high)<<8

		trace.entries.Add(entry)
		ex.enqueue(flowLocation{
			state: state,
			pc:    entry,
		}, registers{})
	}
}

// seedHints enqueues the start of every code run that the CPU annotations or
// the code data log mark as code.
func (ex *mappingExplorer) seedHints(state mapper.Snapshot, key mapper.Mapping) {
	if ex.hints.CodeHint == nil && len(ex.hints.CodeDataLog) == 0 {
		return
	}

	continuation := false

	for address := uint16(prgStartAddress); address < cpu6502.InterruptVectorStartAddress; address++ {
		hint := ex.codeHint(address)
		if !ex.isCodeCandidate(hint, physicalOffset(key, address)) {
			continuation = false
			continue
		}

		ins, ok := readFlowInstruction(ex.mem, state, address)
		if !ok {
			continuation = false
			continue
		}

		if !continuation {
			ex.enqueue(flowLocation{
				state: state,
				pc:    address,
			}, registers{})
		}

		_, terminal := cpu6502.NotExecutingFollowingOpcodeInstructions[ins.op.Instruction.Name]
		continuation = !terminal && ins.op.Instruction.Name != cpu6502.KilName

		if ex.stopAtUnofficial && stopAtUnofficialInstruction(ins.op.Instruction, false, hint) {
			continuation = false
		}

		// Match sequential code decoding so operands do not become new roots.
		address += uint16(ins.size - 1)
	}
}

func (ex *mappingExplorer) isCodeCandidate(hint program.OffsetType, physical int) bool {
	if hint == program.DataOffset {
		return false
	}

	return hint == program.CodeOffset || ex.cdlCode(physical)
}

func (ex *mappingExplorer) cdlCode(physical int) bool {
	if physical < 0 || physical >= len(ex.hints.CodeDataLog) {
		return false
	}

	return ex.hints.CodeDataLog[physical]&codedatalog.Code != 0
}

func (ex *mappingExplorer) results() mapper.Exploration {
	result := make([]mapper.Trace, 0, len(ex.order))

	for _, key := range ex.order {
		trace := ex.traces[key]
		item := mapper.Trace{
			State:   trace.state,
			Entries: slices.Sorted(maps.Keys(trace.entries)),
		}

		// Stops are only safe when every successor was explored.
		if !ex.limited {
			item.Stops = slices.Sorted(maps.Keys(trace.stops))
		}

		result = append(result, item)
	}

	return mapper.Exploration{
		Traces:  result,
		Limited: ex.limited,
	}
}

// physicalOffset returns the ROM offset behind a CPU address, or -1 if the window is unmapped.
func physicalOffset(key mapper.Mapping, address uint16) int {
	base := key[address/windowSize]
	if base < 0 {
		return -1
	}

	return base + int(address%windowSize)
}
