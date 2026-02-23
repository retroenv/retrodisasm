// Package m6502emu provides an advisory NES CPU trace using the 6502 emulator.
package m6502emu

import (
	"context"
	"fmt"
	"time"

	cpu6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

const (
	defaultMaxInstructions = 100000
	defaultMaxVisitsPerPC  = 8
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

	UniquePCCount      int
	UniqueMappingCount int
	Instructions       int
	HaltReason         string
	Duration           time.Duration
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
			res.Instructions = len(res.Steps)
			res.UniquePCCount = len(uniquePCs)
			res.UniqueMappingCount = len(uniqueMappings)
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

	res.Instructions = len(res.Steps)
	res.UniquePCCount = len(uniquePCs)
	res.UniqueMappingCount = len(uniqueMappings)
	return res, nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.MaxInstructions <= 0 {
		cfg.MaxInstructions = defaultMaxInstructions
	}
	if cfg.MaxVisitsPerPC <= 0 {
		cfg.MaxVisitsPerPC = defaultMaxVisitsPerPC
	}
	return cfg
}
