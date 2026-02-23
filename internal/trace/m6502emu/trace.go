// Package m6502emu provides an advisory NES CPU trace using the 6502 emulator.
package m6502emu

import (
	"context"
	"fmt"
	"strings"
	"time"

	cpu6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

const (
	defaultMaxInstructions = 100000
	defaultMaxVisitsPerPC  = 8
	defaultMaxBranchStates = 0
)

// Mapper defines the mapper functions needed by the emulator trace.
type Mapper interface {
	ReadMemory(address uint16) byte
	MappingSignature() uint64
	ResolveAddress(address uint16) (bankID int, physicalOffset uint32, ok bool)
	ApplyMapperWrite(address uint16, value byte) bool
}

// Config controls advisory trace execution limits.
type Config struct {
	MaxInstructions int
	MaxVisitsPerPC  int
	MaxBranchStates int
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
	Instructions               int
	HaltReason                 string
	Duration                   time.Duration
}

// Run executes a bounded advisory 6502 trace over a NES cartridge using mapper-backed PRG reads.
func Run(ctx context.Context, cart *cartridge.Cartridge, mapper Mapper, cfg Config) (*Result, error) {
	cfg = normalizeConfig(cfg)

	res := &Result{}
	start := time.Now()
	defer func() {
		res.Duration = time.Since(start)
	}()

	bus := newNesBus(cart, mapper)
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

	visits := make(map[uint16]int, 4096)
	uniquePCs := make(map[uint16]struct{}, 4096)
	uniqueMappings := make(map[uint64]struct{}, 64)

	for i := 0; i < cfg.MaxInstructions; i++ {
		select {
		case <-ctx.Done():
			res.HaltReason = "context cancelled"
			finalizeResult(res, cfg, uniquePCs, uniqueMappings)
			return res, nil
		default:
		}

		pc := cpu.PC
		visits[pc]++
		if visits[pc] > cfg.MaxVisitsPerPC {
			res.HaltReason = fmt.Sprintf("pc visit limit exceeded at $%04X", pc)
			break
		}

		if err := cpu.Step(); err != nil {
			res.HaltReason = err.Error()
			break
		}

		ts := cpu.TraceStep
		signature := mapper.MappingSignature()
		uniquePCs[ts.PC] = struct{}{}
		uniqueMappings[signature] = struct{}{}

		bankID, physicalOffset, hasPhysical := mapper.ResolveAddress(ts.PC)

		operands := make([]byte, len(ts.OpcodeOperands))
		copy(operands, ts.OpcodeOperands)

		step := TraceStep{
			PC:               ts.PC,
			OpcodeName:       ts.Opcode.Instruction.Name,
			OpcodeOperands:   operands,
			MappingSignature: signature,
			BankID:           bankID,
			PhysicalOffset:   physicalOffset,
			HasPhysical:      hasPhysical,
		}
		res.Steps = append(res.Steps, step)
	}

	if res.HaltReason == "" {
		res.HaltReason = "instruction budget exhausted"
	}

	finalizeResult(res, cfg, uniquePCs, uniqueMappings)
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
	return cfg
}

func finalizeResult(res *Result, cfg Config, uniquePCs map[uint16]struct{}, uniqueMappings map[uint64]struct{}) {
	res.Instructions = len(res.Steps)
	res.UniquePCCount = len(uniquePCs)
	res.UniqueMappingCount = len(uniqueMappings)

	alternates, conditionalCount, budgetDrops := collectBranchAlternates(res.Steps, cfg.MaxBranchStates)
	res.ConditionalBranchCount = conditionalCount
	res.BranchAlternates = alternates
	res.BranchAlternateCount = len(alternates)
	res.BranchAlternateBudgetDrops = budgetDrops
}

func collectBranchAlternates(steps []TraceStep, maxBranchStates int) ([]BranchAlternate, int, int) {
	var (
		alternates []BranchAlternate
		budgetDrop int
	)

	if len(steps) < 2 {
		return alternates, 0, 0
	}

	type alternateKey struct {
		PC        uint16
		MappingID uint64
	}
	seen := map[alternateKey]struct{}{}

	conditionalCount := 0
	for i := 0; i < len(steps)-1; i++ {
		step := steps[i]
		if !isConditionalBranch(step.OpcodeName) || len(step.OpcodeOperands) < 1 {
			continue
		}
		conditionalCount++
		if maxBranchStates <= 0 {
			continue
		}

		operand := step.OpcodeOperands[len(step.OpcodeOperands)-1]
		fallthroughPC := step.PC + 2
		target := uint16(int32(fallthroughPC) + int32(int8(operand)))
		nextPC := steps[i+1].PC

		var (
			alternate uint16
			ok        bool
			taken     bool
		)
		switch nextPC {
		case target:
			alternate = fallthroughPC
			ok = true
			taken = true
		case fallthroughPC:
			alternate = target
			ok = true
		}
		if !ok || alternate == nextPC {
			continue
		}

		key := alternateKey{
			PC:        alternate,
			MappingID: step.MappingSignature,
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if len(alternates) >= maxBranchStates {
			budgetDrop++
			continue
		}

		alternates = append(alternates, BranchAlternate{
			FromPC:            step.PC,
			Address:           alternate,
			MappingSignature:  step.MappingSignature,
			BranchTarget:      target,
			FallthroughTarget: fallthroughPC,
			Taken:             taken,
		})
	}

	return alternates, conditionalCount, budgetDrop
}

func isConditionalBranch(opName string) bool {
	switch strings.ToUpper(opName) {
	case "BCC", "BCS", "BEQ", "BMI", "BNE", "BPL", "BVC", "BVS":
		return true
	default:
		return false
	}
}
