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
	envEmuTraceMaxBranch = "RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES"
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
		MaxInstructions: intSetting(dis.options.TraceMaxInstructions, envEmuTraceMaxInstr, 100000),
		MaxVisitsPerPC:  intSetting(dis.options.TraceMaxVisitsPerPC, envEmuTraceMaxVisits, 8),
		MaxBranchStates: intSetting(dis.options.TraceMaxBranchStates, envEmuTraceMaxBranch, 0),
	}

	startSignature := dis.mapper.MappingSignature()
	defer func() {
		if !dis.mapper.RestoreMappingSignature(startSignature) {
			dis.mapper.RestoreDefaultMapping()
		}
	}()

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
		log.Int("conditional_branches", result.ConditionalBranchCount),
		log.Int("branch_alternates", result.BranchAlternateCount),
		log.Int("branch_alternate_budget_drops", result.BranchAlternateBudgetDrops),
		log.Int("branch_states_executed", result.BranchStatesExecuted),
		log.String("halt_reason", result.HaltReason),
		log.Duration("elapsed", result.Duration),
	)

	return result
}

func (dis *Disasm) seedFromAdvisoryEmuTrace(result *m6502emu.Result) {
	if result == nil {
		return
	}

	seen := map[ParseKey]struct{}{}
	for _, step := range result.Steps {
		key := ParseKey{
			PC:        step.PC,
			MappingID: step.MappingSignature,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if !dis.mapper.RestoreMappingSignature(step.MappingSignature) {
			continue
		}
		dis.AddAddressToParse(step.PC, step.PC, 0, nil, false)
	}

	for _, alt := range result.BranchAlternates {
		key := ParseKey{
			PC:        alt.Address,
			MappingID: alt.MappingSignature,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if !dis.mapper.RestoreMappingSignature(alt.MappingSignature) {
			continue
		}
		// Alternate branch states are exploratory roots. They intentionally do not
		// register as authoritative branch destinations to avoid rewriting the
		// original branch operand target during jump-destination post-processing.
		dis.AddAddressToParse(alt.Address, alt.FromPC, 0, nil, false)
	}

	dis.mapper.RestoreDefaultMapping()
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
		if !dis.hasParsedPC(pc) {
			emuOnly++
		}
	}

	staticPCs := dis.staticParsedPCSet()
	staticOnly := 0
	for pc := range staticPCs {
		if _, ok := emuPCs[pc]; !ok {
			staticOnly++
		}
	}

	dis.logger.Debug("Advisory emu trace comparison",
		log.Int("emu_unique_pc", len(emuPCs)),
		log.Int("static_unique_pc", len(staticPCs)),
		log.Int("emu_only_pc", emuOnly),
		log.Int("static_only_pc", staticOnly),
	)
}

func (dis *Disasm) hasParsedPC(pc uint16) bool {
	for key := range dis.offsetsParsed {
		if key.PC == pc {
			return true
		}
	}
	return false
}

func (dis *Disasm) staticParsedPCSet() map[uint16]struct{} {
	pcs := make(map[uint16]struct{}, len(dis.offsetsParsed))
	for key := range dis.offsetsParsed {
		pcs[key.PC] = struct{}{}
	}
	return pcs
}

func (dis *Disasm) shouldRunAdvisoryEmuTrace() bool {
	switch strings.ToLower(strings.TrimSpace(dis.options.TraceMode)) {
	case "emu", "hybrid":
		return true
	}

	// Backward compatibility for existing env-based flows.
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

func intSetting(cliValue int, envKey string, defaultValue int) int {
	if cliValue > 0 {
		return cliValue
	}
	return envIntOrDefault(envKey, defaultValue)
}
