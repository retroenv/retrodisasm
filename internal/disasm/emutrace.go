package disasm

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/trace/m6502emu"
	"github.com/retroenv/retrogolib/log"
)

const (
	envEmuTraceEnable    = "RETRODISASM_EMU_TRACE"
	envEmuTraceMaxInstr  = "RETRODISASM_EMU_TRACE_MAX_INSTR"
	envEmuTraceMaxVisits = "RETRODISASM_EMU_TRACE_MAX_VISITS"
)

func (dis *Disasm) runAdvisoryEmuTrace(ctx context.Context) *m6502emu.Result {
	if !dis.shouldRunAdvisoryEmuTrace() {
		return nil
	}
	if dis.options.Binary || dis.cart == nil {
		return nil
	}
	if _, ok := dis.arch.(*m6502.Arch6502); !ok {
		return nil
	}

	cfg := m6502emu.Config{
		MaxInstructions: envIntOrDefault(envEmuTraceMaxInstr, 100000),
		MaxVisitsPerPC:  envIntOrDefault(envEmuTraceMaxVisits, 8),
	}
	result, err := m6502emu.Run(ctx, dis.cart, dis.mapper, cfg)
	if err != nil {
		dis.logger.Warn("Advisory emu trace failed", log.Err(err))
		return nil
	}

	changed := 0
	for _, event := range result.BankSwitchWrites {
		if event.Changed {
			changed++
		}
	}

	dis.logger.Debug("Advisory emu trace",
		log.Int("instructions", result.Instructions),
		log.Int("unique_pc", result.UniquePCCount),
		log.Int("unique_mapping", result.UniqueMappingCount),
		log.Int("mapper_writes", len(result.BankSwitchWrites)),
		log.Int("mapping_changes", changed),
		log.String("halt_reason", result.HaltReason),
		log.Duration("elapsed", result.Duration),
	)

	return result
}

func (dis *Disasm) logAdvisoryEmuTraceComparison(result *m6502emu.Result) {
	if result == nil {
		return
	}

	emuPCs := map[uint16]struct{}{}
	for _, step := range result.Steps {
		emuPCs[step.PC] = struct{}{}
	}

	emuOnly := 0
	for pc := range emuPCs {
		if !dis.offsetsParsed.Contains(pc) {
			emuOnly++
		}
	}

	staticOnly := 0
	for pc := range dis.offsetsParsed {
		if _, ok := emuPCs[pc]; !ok {
			staticOnly++
		}
	}

	dis.logger.Debug("Advisory emu trace comparison",
		log.Int("emu_unique_pc", len(emuPCs)),
		log.Int("static_unique_pc", len(dis.offsetsParsed)),
		log.Int("emu_only_pc", emuOnly),
		log.Int("static_only_pc", staticOnly),
	)
}

func (dis *Disasm) shouldRunAdvisoryEmuTrace() bool {
	return envBoolEnabled(envEmuTraceEnable)
}

func envBoolEnabled(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return false
	}
	switch value {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func envIntOrDefault(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}
