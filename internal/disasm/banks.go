package disasm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/set"
)

// analyze runs the full analysis for the mapping state of this disassembler.
func (dis *Disasm) analyze(ctx context.Context) (*program.Program, error) {
	if err := dis.followExecutionFlow(ctx); err != nil {
		return nil, err
	}

	if err := dis.traceConfiguredCode(ctx); err != nil {
		return nil, err
	}

	// Post-process architecture-specific patterns after all branch destinations are known
	if err := dis.arch.PostProcessCode(); err != nil {
		return nil, fmt.Errorf("post-processing code: %w", err)
	}

	dis.mapper.ClassifyRemainingAsData()
	dis.applyAnnotations()

	if err := dis.vars.Process(dis.codeBaseAddress); err != nil {
		return nil, fmt.Errorf("processing variables: %w", err)
	}
	dis.constants.Process()
	dis.processJumpDestinations()

	if err := dis.applyConfiguredOperands(); err != nil {
		return nil, err
	}
	if err := dis.applyConfiguredData(); err != nil {
		return nil, err
	}

	app, err := dis.convertToProgram()
	if err != nil {
		return nil, err
	}

	if dis.config != nil {
		if err := dis.assignConfiguredSymbols(app); err != nil {
			return nil, err
		}
		dis.pruneUnusedConfigSymbols(app)
		if err := validateConfiguredSymbols(app); err != nil {
			return nil, err
		}
	}
	return app, nil
}

// analyzeMappings traces every explored mapping and merges the results into one program.
func (dis *Disasm) analyzeMappings(ctx context.Context) (*program.Program, error) {
	if dis.mapper.StateCount() == 1 {
		return dis.analyze(ctx)
	}
	explorer, ok := dis.arch.(mappingExplorer)
	if !ok || dis.newArchitecture == nil {
		return nil, errors.New("architecture does not support mapping exploration")
	}

	hints := mapper.ExplorationHints{
		CodeDataLog: dis.cdlFlags,
	}
	if dis.config != nil {
		hints.CodeHint = dis.mappingCodeHint
	}

	exploration, err := explorer.ExploreMappings(ctx, dis.mapper, hints)
	if err != nil {
		return nil, fmt.Errorf("exploring mappings: %w", err)
	}
	if exploration.Limited {
		dis.logger.Warn("Mapper exploration limit reached; retaining static bank traces")
	}

	var app *program.Program
	for index, trace := range exploration.Traces {
		next, err := dis.analyzeTrace(ctx, trace, index == 0)
		if err != nil {
			return nil, fmt.Errorf("analyzing mapping %d: %w", index, err)
		}

		if app == nil {
			app = next
			continue
		}
		for i, bank := range next.PRG {
			mergeBankAnalysis(app.PRG[i], bank)
		}
		maps.Copy(app.Constants, next.Constants)
		maps.Copy(app.Variables, next.Variables)
	}

	if err := validateConfiguredSymbols(app); err != nil {
		return nil, err
	}
	return app, nil
}

// analyzeTrace analyzes one mapping. The first trace reuses the receiver, later
// traces use a new disassembler with an independent mapper state.
func (dis *Disasm) analyzeTrace(ctx context.Context, trace mapper.Trace, first bool) (*program.Program, error) {
	next := dis
	if !first {
		opts := dis.options
		opts.CodeDataLog = nil
		var err error
		next, err = newWithMapper(dis.logger, dis.newArchitecture(), dis.cart, opts,
			dis.fileWriterConstructor, dis.mapper.NewTrace(trace.State))
		if err != nil {
			return nil, err
		}

		next.mapper.ApplyCodeDataLog(dis.cdlFlags)
		if dis.annotationsFilename != "" {
			if err := next.ApplyAnnotations(dis.annotationsFilename); err != nil {
				return nil, err
			}
		}
	}

	next.mappingStops = set.NewFromSlice(trace.Stops)
	for _, address := range trace.Entries {
		next.AddAddressToParse(address, address, 0, nil, false)
	}

	app, err := next.analyze(ctx)
	if err != nil {
		return nil, err
	}
	if err := qualifyBankSymbols(app); err != nil {
		return nil, err
	}

	mergeAlternateViews(app)
	return app, nil
}

func (dis *Disasm) mappingCodeHint(address uint16) program.OffsetType {
	for _, rng := range dis.config.Data {
		if address >= rng.Start && address <= rng.End {
			return program.DataOffset
		}
	}

	for _, rng := range dis.config.Code {
		if address >= rng.Start && address <= rng.End {
			return program.CodeOffset
		}
	}

	return program.UnknownOffset
}

// mappingExplorer is implemented by architectures that find the bank mappings to trace.
type mappingExplorer interface {
	ExploreMappings(context.Context, *mapper.Mapper, mapper.ExplorationHints) (mapper.Exploration, error)
}

// qualifyBankSymbols adds the physical bank and CPU address to every label, so
// the same label name in different banks stays unique.
func qualifyBankSymbols(app *program.Program) error {
	names := make(map[string]string)

	for _, bank := range app.PRG {
		for _, info := range bank.Offsets {
			for _, name := range append(slices.Clone(info.Aliases), info.Label) {
				if name == "" {
					continue
				}
				// Vector-bank changes can assign one handler name to different
				// offsets in the same physical bank and at the same CPU origin.
				qualified := fmt.Sprintf("%s_bank%d_at%04x", name, bank.ID, info.Address)
				if previous, ok := names[name]; ok && previous != qualified {
					return fmt.Errorf("ambiguous ROM symbol %q within one mapping", name)
				}
				names[name] = qualified
			}
		}
	}

	for _, bank := range app.PRG {
		for i := range bank.Offsets {
			info := &bank.Offsets[i]
			info.Label = names[info.Label]
			for j, alias := range info.Aliases {
				info.Aliases[j] = names[alias]
			}
			info.Code = rewriteSymbols(info.Code, names)
		}
	}

	app.Handlers.NMI = rewriteSymbols(app.Handlers.NMI, names)
	app.Handlers.Reset = rewriteSymbols(app.Handlers.Reset, names)
	app.Handlers.IRQ = rewriteSymbols(app.Handlers.IRQ, names)

	for i := range app.SymbolGroups {
		for j, name := range app.SymbolGroups[i].Names {
			app.SymbolGroups[i].Names[j] = rewriteSymbols(name, names)
		}
	}
	return nil
}

// rewriteSymbols replaces every symbol in the expression that has an entry in names.
func rewriteSymbols(expression string, names map[string]string) string {
	var out strings.Builder

	for start := 0; start < len(expression); {
		if isSymbolSeparator(rune(expression[start])) {
			out.WriteByte(expression[start])
			start++
			continue
		}
		end := start + 1
		for end < len(expression) && !isSymbolSeparator(rune(expression[end])) {
			end++
		}
		token := expression[start:end]
		if replacement, ok := names[token]; ok && (start == 0 || expression[start-1] != '$') {
			token = replacement
		}
		out.WriteString(token)
		start = end
	}
	return out.String()
}

// mergeBankAnalysis merges the analysis of src into dst. Both banks hold the same
// physical ROM bytes, seen through different mappings.
func mergeBankAnalysis(dst, src *program.PRGBank) {
	if src.BaseAddress != dst.BaseAddress {
		convertOriginLabels(src)
	}

	maps.Copy(dst.Constants, src.Constants)
	maps.Copy(dst.Variables, src.Variables)

	if len(src.Offsets) == 0 {
		return
	}
	if len(dst.Offsets) == 0 {
		if src.BaseAddress == dst.BaseAddress {
			dst.Offsets = src.Offsets
			return
		}

		dst.Offsets = rawBankOffsets(src)
		for i := range dst.Offsets {
			dst.Offsets[i].Address = dst.BaseAddress + uint16(i)
		}
	}

	conflicts := mappingConflicts(dst, src)
	merged := rawBankOffsets(dst)
	overlayRecords(merged, dst, conflicts)
	overlayRecords(merged, src, conflicts)

	for i := range merged {
		mergeOffsetLabels(&merged[i], dst.Offsets[i], src.Offsets[i])

		if conflicts[i] {
			merged[i].SetType(program.CodeAsData)

			if !strings.Contains(merged[i].Comment, "mapping-dependent interpretation") {
				if merged[i].Comment != "" {
					merged[i].Comment += " | "
				}
				merged[i].Comment += "mapping-dependent interpretation"
			}
		}
	}

	dst.Offsets = merged
	splitLabeledRecords(dst)
}

// mappingConflicts reports for each offset if the two banks interpret it differently.
func mappingConflicts(dst, src *program.PRGBank) []bool {
	left, right := recordOwners(dst), recordOwners(src)
	conflicts := make([]bool, len(dst.Offsets))

	for i, owner := range left {
		other := right[i]
		if src.BaseAddress != dst.BaseAddress && other >= 0 {
			conflicts[i] = true
		}
		if owner < 0 || other < 0 {
			continue
		}

		a, b := dst.Offsets[owner], src.Offsets[other]
		if owner != other || len(a.Data) != len(b.Data) || a.Code != b.Code {
			for j := min(owner, other); j < max(owner+len(a.Data), other+len(b.Data)); j++ {
				conflicts[j] = true
			}
		}
	}

	return conflicts
}

// mergeAlternateViews merges banks that are alternate CPU views of a physical bank
// into that bank.
func mergeAlternateViews(app *program.Program) {
	count := 0

	for i, bank := range app.PRG {
		if i == bank.ID {
			count++
			continue
		}
		mergeBankAnalysis(app.PRG[bank.ID], bank)
	}

	app.PRG = app.PRG[:count]
}

// convertOriginLabels turns the labels of a bank into address constants.
func convertOriginLabels(bank *program.PRGBank) {
	// A label at an alternate CPU origin cannot be defined at the canonical
	// output location. Preserve references as explicit address constants.
	for i := range bank.Offsets {
		info := &bank.Offsets[i]
		for _, name := range append(slices.Clone(info.Aliases), info.Label) {
			if name != "" {
				bank.Constants[name] = info.Address
			}
		}
		info.Label = ""
		info.Aliases = nil
	}
}

// recordOwners returns the index of the record that covers each offset, or -1.
func recordOwners(bank *program.PRGBank) []int {
	owners := make([]int, len(bank.Offsets))
	for i := range owners {
		owners[i] = -1
	}

	for i, info := range bank.Offsets {
		if !info.IsType(program.CodeOffset | program.CodeAsData | program.FunctionReference | program.ExpressionData) {
			continue
		}
		for j := i; j < min(i+len(info.Data), len(owners)); j++ {
			owners[j] = i
		}
	}

	return owners
}

// rawBankOffsets returns the bank as one data record per byte.
func rawBankOffsets(bank *program.PRGBank) []program.Offset {
	result := make([]program.Offset, len(bank.Offsets))
	data := make([]byte, len(bank.Offsets))

	for i, info := range bank.Offsets {
		for j, value := range info.Data {
			index := i + j
			data[index] = value
			result[index] = bank.Offsets[index]
			result[index].Data = data[index : index+1]
			result[index].Type = program.DataOffset
			result[index].Code = ""
		}
	}

	return result
}

// overlayRecords copies the records of bank that have no conflict into result.
func overlayRecords(result []program.Offset, bank *program.PRGBank, conflicts []bool) {
	for i, info := range bank.Offsets {
		if len(info.Data) == 0 || !info.IsType(program.CodeOffset|program.CodeAsData|program.FunctionReference|program.ExpressionData) {
			continue
		}

		end := i + len(info.Data)
		if slices.Contains(conflicts[i:end], true) {
			continue
		}

		result[i] = info
		for j := i + 1; j < end; j++ {
			result[j].Data = nil
			result[j].Type = info.Type
		}
	}
}

// mergeOffsetLabels combines comments and labels of both offsets into dst.
func mergeOffsetLabels(dst *program.Offset, left, right program.Offset) {
	if dst.Comment == "" {
		dst.Comment = left.Comment
		dst.HasAddressComment = left.HasAddressComment
		if dst.Comment == "" {
			dst.Comment = right.Comment
			dst.HasAddressComment = right.HasAddressComment
		}
	}

	if dst.CommentBefore == "" {
		dst.CommentBefore = left.CommentBefore
		if dst.CommentBefore == "" {
			dst.CommentBefore = right.CommentBefore
		}
	}

	if dst.BlankLines == nil {
		dst.BlankLines = left.BlankLines
		if dst.BlankLines == nil {
			dst.BlankLines = right.BlankLines
		}
	}

	for _, info := range []program.Offset{left, right} {
		for _, name := range append(slices.Clone(info.Aliases), info.Label) {
			if name == "" || name == dst.Label || slices.Contains(dst.Aliases, name) {
				continue
			}
			if dst.Label == "" {
				dst.Label = name
			} else {
				dst.Aliases = append(dst.Aliases, name)
			}
		}
	}
}

// splitLabeledRecords splits records that contain a label in their middle into
// single byte records.
func splitLabeledRecords(bank *program.PRGBank) {
	for i, info := range bank.Offsets {
		end := i + len(info.Data)

		for j := i + 1; j < end; j++ {
			if bank.Offsets[j].Label == "" && len(bank.Offsets[j].Aliases) == 0 {
				continue
			}

			for k, value := range info.Data {
				record := &bank.Offsets[i+k]
				record.Data = []byte{value}
				record.Type = program.CodeAsData | program.DataOffset
				record.Code = ""
			}
			break
		}
	}
}
