package disasm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/trace/m6502emu"
	"github.com/retroenv/retrogolib/log"
)

const (
	mapperWriteHotspotNone = "none"

	envEmuTraceEnable     = "RETRODISASM_EMU_TRACE"
	envEmuTraceMaxInstr   = "RETRODISASM_EMU_TRACE_MAX_INSTR"
	envEmuTraceMaxVisits  = "RETRODISASM_EMU_TRACE_MAX_VISITS"
	envEmuTraceMaxBranch  = "RETRODISASM_EMU_TRACE_MAX_BRANCH_STATES"
	envEmuTraceJoypad1    = "RETRODISASM_EMU_TRACE_JOYPAD1"
	envEmuTraceJoypad2    = "RETRODISASM_EMU_TRACE_JOYPAD2"
	envEmuTraceJoypad1Seq = "RETRODISASM_EMU_TRACE_JOYPAD1_SEQ"
	envEmuTraceJoypad2Seq = "RETRODISASM_EMU_TRACE_JOYPAD2_SEQ"

	mapper1BranchCapLowBudget  = 512
	mapper1BranchCapMidBudget  = 128
	mapper1BranchCapHighBudget = 64
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

	cfg := dis.buildEmuTraceConfig()

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

	dis.logEmuTraceResult(result, cfg)
	return result
}

func (dis *Disasm) buildEmuTraceConfig() m6502emu.Config {
	cfg := m6502emu.Config{
		MaxInstructions: intSetting(dis.options.TraceMaxInstructions, envEmuTraceMaxInstr, 100000),
		MaxVisitsPerPC:  intSetting(dis.options.TraceMaxVisitsPerPC, envEmuTraceMaxVisits, 8),
		MaxBranchStates: intSetting(dis.options.TraceMaxBranchStates, envEmuTraceMaxBranch, 0),
		Joypad1State:    byte(intSetting(dis.options.TraceJoypad1, envEmuTraceJoypad1, 0) & 0xFF),
		Joypad2State:    byte(intSetting(dis.options.TraceJoypad2, envEmuTraceJoypad2, 0) & 0xFF),
	}
	joypad1Sequence, err := parseJoypadSequenceSetting(dis.options.TraceJoypad1Sequence, envEmuTraceJoypad1Seq)
	if err != nil {
		dis.logger.Warn("Ignoring invalid joypad1 trace sequence", log.Err(err))
	}
	joypad2Sequence, err := parseJoypadSequenceSetting(dis.options.TraceJoypad2Sequence, envEmuTraceJoypad2Seq)
	if err != nil {
		dis.logger.Warn("Ignoring invalid joypad2 trace sequence", log.Err(err))
	}
	cfg.Joypad1Sequence = joypad1Sequence
	cfg.Joypad2Sequence = joypad2Sequence
	if dis.cart != nil {
		tuned := tunedBranchStateBudgetForMapper(dis.cart.Mapper,
			cfg.MaxBranchStates, cfg.MaxInstructions, cfg.MaxVisitsPerPC)
		if tuned != cfg.MaxBranchStates {
			dis.logger.Debug("Adjusting branch alternate budget for mapper policy",
				log.Int("mapper", int(dis.cart.Mapper)),
				log.Int("requested_max_branch_states", cfg.MaxBranchStates),
				log.Int("effective_max_branch_states", tuned),
				log.Int("max_instructions", cfg.MaxInstructions),
				log.Int("max_visits_per_state", cfg.MaxVisitsPerPC))
			cfg.MaxBranchStates = tuned
		}
	}
	return cfg
}

func (dis *Disasm) logEmuTraceResult(result *m6502emu.Result, cfg m6502emu.Config) {
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
		log.Int("mapper_write_unique_addresses", result.MapperWriteUniqueAddresses),
		log.Int("mapper_write_unique_pcs", result.MapperWriteUniquePCs),
		log.Int("mapper_write_unique_transitions", result.MapperWriteUniqueTransitions),
		log.String("mapper_write_top_addresses", formatMapperWriteAddressHotspots(result.MapperWriteAddressHotspots)),
		log.String("mapper_write_top_pcs", formatMapperWritePCHotspots(result.MapperWritePCHotspots)),
		log.String("mapper_write_top_transitions", formatMapperWriteTransitionHotspots(result.MapperWriteTransitionHotspots)),
		log.Int("conditional_branches", result.ConditionalBranchCount),
		log.Int("branch_alternates", result.BranchAlternateCount),
		log.Int("branch_alternate_budget_drops", result.BranchAlternateBudgetDrops),
		log.Int("branch_states_executed", result.BranchStatesExecuted),
		log.Int("joypad1_state", int(cfg.Joypad1State)),
		log.Int("joypad2_state", int(cfg.Joypad2State)),
		log.Int("joypad1_seq_len", len(cfg.Joypad1Sequence)),
		log.Int("joypad2_seq_len", len(cfg.Joypad2Sequence)),
		log.String("halt_reason", result.HaltReason),
		log.Duration("elapsed", result.Duration),
	)
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
		dis.AddAddressToParse(alt.Address, alt.Address, alt.FromPC, nil, false)
	}
	dis.seedVectorsForAdvisoryMappings(result)

	dis.mapper.RestoreDefaultMapping()
}

func (dis *Disasm) seedVectorsForAdvisoryMappings(result *m6502emu.Result) {
	if len(result.Steps) == 0 {
		return
	}

	signatureSet := map[uint64]struct{}{}
	for _, step := range result.Steps {
		signatureSet[step.MappingSignature] = struct{}{}
	}
	signatures := make([]uint64, 0, len(signatureSet))
	for signature := range signatureSet {
		signatures = append(signatures, signature)
	}
	slices.Sort(signatures)

	vectorAddresses := []uint16{0xFFFA, 0xFFFC, 0xFFFE}

	for _, signature := range signatures {
		if !dis.mapper.RestoreMappingSignature(signature) {
			continue
		}
		for _, vectorAddress := range vectorAddresses {
			handler, err := dis.ReadMemoryWord(vectorAddress)
			if err != nil || handler == 0 {
				continue
			}
			dis.AddAddressToParse(handler, handler, 0, nil, false)
		}
	}
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
	switch dis.traceMode() {
	case "emu", "hybrid":
		return true
	}

	// Backward compatibility for existing env-based flows.
	return envBoolEnabled(envEmuTraceEnable)
}

func (dis *Disasm) isHybridTraceMode() bool {
	return dis.traceMode() == "hybrid"
}

func (dis *Disasm) traceMode() string {
	return strings.ToLower(strings.TrimSpace(dis.options.TraceMode))
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

func tunedBranchStateBudgetForMapper(mapper uint16, requested, maxInstructions, maxVisitsPerPC int) int {
	if requested <= 0 {
		return requested
	}
	if mapper != 1 {
		return requested
	}

	capBudget := mapper1BranchCapLowBudget
	if maxInstructions >= 1_000_000 || maxVisitsPerPC >= 512 {
		capBudget = mapper1BranchCapHighBudget
	} else if maxInstructions >= 500_000 || maxVisitsPerPC >= 256 {
		capBudget = mapper1BranchCapMidBudget
	}
	if requested <= capBudget {
		return requested
	}
	return capBudget
}

func intSetting(cliValue int, envKey string, defaultValue int) int {
	if cliValue > 0 {
		return cliValue
	}
	return envIntOrDefault(envKey, defaultValue)
}

func parseJoypadSequenceSetting(cliValue, envKey string) ([]byte, error) {
	value := strings.TrimSpace(cliValue)
	if value == "" {
		value = strings.TrimSpace(os.Getenv(envKey))
	}
	if value == "" {
		return nil, nil
	}
	return parseJoypadSequence(value)
}

func parseJoypadSequence(value string) ([]byte, error) {
	tokens := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ':' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(tokens) == 0 {
		return nil, errors.New("empty joypad sequence")
	}
	sequence := make([]byte, 0, len(tokens))
	for _, token := range tokens {
		parsed, err := parseJoypadToken(token)
		if err != nil {
			return nil, fmt.Errorf("invalid joypad token %q: %w", token, err)
		}
		sequence = append(sequence, parsed)
	}
	return sequence, nil
}

func parseJoypadToken(token string) (byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, errors.New("empty token")
	}
	if strings.HasPrefix(token, "$") {
		token = "0x" + token[1:]
	}
	base := 10
	if strings.HasPrefix(token, "0x") || strings.HasPrefix(token, "0X") {
		token = token[2:]
		base = 16
	}
	value, err := strconv.ParseUint(token, base, 8)
	if err != nil {
		return 0, fmt.Errorf("parsing joypad sequence token: %w", err)
	}
	return byte(value), nil
}

func formatMapperWriteAddressHotspots(hotspots []m6502emu.MapperWriteAddressHotspot) string {
	if len(hotspots) == 0 {
		return mapperWriteHotspotNone
	}
	parts := make([]string, 0, len(hotspots))
	for _, hotspot := range hotspots {
		parts = append(parts, fmt.Sprintf("$%04X:%d(chg=%d)",
			hotspot.Address, hotspot.Count, hotspot.ChangedCount))
	}
	return strings.Join(parts, ",")
}

func formatMapperWritePCHotspots(hotspots []m6502emu.MapperWritePCHotspot) string {
	if len(hotspots) == 0 {
		return mapperWriteHotspotNone
	}
	parts := make([]string, 0, len(hotspots))
	for _, hotspot := range hotspots {
		parts = append(parts, fmt.Sprintf("$%04X:%d(chg=%d)",
			hotspot.PC, hotspot.Count, hotspot.ChangedCount))
	}
	return strings.Join(parts, ",")
}

func formatMapperWriteTransitionHotspots(hotspots []m6502emu.MapperWriteTransitionHotspot) string {
	if len(hotspots) == 0 {
		return mapperWriteHotspotNone
	}
	parts := make([]string, 0, len(hotspots))
	for _, hotspot := range hotspots {
		parts = append(parts, fmt.Sprintf("%d->%d:%d",
			hotspot.BeforeMapping, hotspot.AfterMapping, hotspot.Count))
	}
	return strings.Join(parts, ",")
}
