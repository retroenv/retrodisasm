package disasm

import (
	"context"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/program"
	cpum6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
)

// processAdditionalBanks traces unique vectors of non-last PRG banks by temporarily remapping
// the full $8000-$FFFF range to each bank.
func (dis *Disasm) processAdditionalBanks(ctx context.Context) error {
	if dis.options.Binary {
		return nil
	}

	bankCount := dis.mapper.BankCount()
	if bankCount <= 1 {
		return nil
	}

	dis.stats.additionalBanksConsidered += uint64(bankCount - 1)
	defer dis.mapper.RestoreDefaultMapping()

	for bankIndex := 0; bankIndex < bankCount-1; bankIndex++ {
		dis.mapper.MapBank(bankIndex)

		dis.seedLikelyMappedBankEntryPoints()
		dis.seedLikelyMappedBankCallTargets()
		dis.seedLikelyMappedBankPointerTableTargets()

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

// seedLikelyMappedBankCallTargets scans the currently mapped bank for absolute
// JSR/JMP targets and queues targets that look like routine entry points.
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

		op, err := dis.ReadMemory(addr)
		if err != nil {
			continue
		}
		if op != 0x20 && op != 0x4C { // JSR abs / JMP abs
			continue
		}

		low, err := dis.ReadMemory(addr + 1)
		if err != nil {
			continue
		}
		high, err := dis.ReadMemory(addr + 2)
		if err != nil {
			continue
		}
		target := uint16(high)<<8 | uint16(low)
		if !dis.isValidCodeAddress(target) || target > end {
			continue
		}

		targetOp, err := dis.ReadMemory(target)
		if err != nil || !isLikelyM6502RoutineStartOpcode(targetOp) {
			continue
		}

		dis.AddAddressToParse(target, target, addr, nil, false)
		queued++
	}
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
		if dis.mapper.OffsetInfo(addr).IsType(program.CodeOffset) ||
			dis.mapper.OffsetInfo(addr+1).IsType(program.CodeOffset) {
			continue
		}

		targets := make([]uint16, 0, minRunEntries)
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

		if len(targets) < minRunEntries || !hasLikelyPointerTableShape(targets, minDistinctTargets) {
			continue
		}

		for _, target := range targets {
			if seeded >= maxSeedsPerBank {
				break
			}
			if _, ok := seededTargets[target]; ok {
				continue
			}
			dis.AddAddressToParse(target, target, addr, nil, false)
			seededTargets[target] = struct{}{}
			seeded++
		}

		// Skip across the accepted table run.
		addr += uint16(len(targets)*2 - 1)
	}
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
