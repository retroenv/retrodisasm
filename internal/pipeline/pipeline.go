// Package pipeline orchestrates the disassembly workflow stages.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/retroenv/retrodisasm/internal/arch/chip8"
	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/assembler/asm6"
	"github.com/retroenv/retrodisasm/internal/assembler/ca65"
	"github.com/retroenv/retrodisasm/internal/assembler/nesasm"
	"github.com/retroenv/retrodisasm/internal/assembler/retroasm"
	"github.com/retroenv/retrodisasm/internal/detector"
	"github.com/retroenv/retrodisasm/internal/disasm"
	"github.com/retroenv/retrodisasm/internal/loader"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/verification"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/log"
)

// Pipeline orchestrates the complete disassembly workflow.
type Pipeline struct {
	logger   *log.Logger
	detector *detector.Detector
	loader   *loader.Loader
}

// New creates a new disassembly pipeline.
func New(logger *log.Logger) *Pipeline {
	return &Pipeline{
		logger:   logger,
		detector: detector.New(logger),
		loader:   loader.New(),
	}
}

// Execute runs the complete disassembly pipeline.
func (p *Pipeline) Execute(ctx context.Context, opts options.Program, disasmOpts options.Disassembler, writer io.Writer) (*program.Program, error) {
	// Detect system architecture
	system := p.detector.Detect(opts)

	// Load cartridge
	cart, cdlReader, err := p.loader.Load(opts, system)
	if err != nil {
		return nil, fmt.Errorf("loading cartridge: %w", err)
	}
	defer func() {
		if cdlReader != nil {
			_ = cdlReader.Close()
		}
	}()

	// Set CDL reader in disasm options if provided
	if cdlReader != nil {
		disasmOpts.CodeDataLog = cdlReader
	}

	return p.ExecuteWithCartridge(ctx, cart, opts, disasmOpts, writer, system)
}

// ExecuteWithCartridge runs the disassembly pipeline with a pre-loaded cartridge.
// This is useful for testing and programmatic usage where the cartridge is already in memory.
func (p *Pipeline) ExecuteWithCartridge(ctx context.Context, cart *cartridge.Cartridge, opts options.Program,
	disasmOpts options.Disassembler, writer io.Writer, system arch.System) (*program.Program, error) {

	// Update disasm options with system
	disasmOpts.System = system
	disasmOpts.Binary = opts.Binary
	disasmOpts.OutputFilename = filepath.Base(opts.Output)

	// When using binary mode, only output code without NES-specific segments
	if opts.Binary {
		disasmOpts.CodeOnly = true
	}

	// Validate assembler compatibility
	if err := assembler.ValidateSystemAssembler(system, opts.Assembler); err != nil {
		return nil, fmt.Errorf("incompatible assembler: %w", err)
	}

	// Create disassembler
	dis, err := p.createDisassembler(opts, disasmOpts, cart, system)
	if err != nil {
		return nil, fmt.Errorf("creating disassembler: %w", err)
	}

	// Print info before processing
	p.printInfo(opts, cart, system)

	// Run disassembly
	result, err := p.runDisassembly(ctx, dis, writer, disasmOpts, opts.Output)
	if err != nil {
		return nil, fmt.Errorf("disassembling: %w", err)
	}

	// Verify output (if requested)
	if opts.AssembleTest {
		if err := verification.VerifyOutput(ctx, p.logger, opts, cart, result); err != nil {
			return nil, fmt.Errorf("verification failed: %w", err)
		}
		p.logger.Info("Verification successful")
	}

	return result, nil
}

// createDisassembler creates and configures the disassembler for the detected system.
func (p *Pipeline) createDisassembler(opts options.Program, disasmOpts options.Disassembler,
	cart *cartridge.Cartridge, system arch.System) (*disasm.Disasm, error) {

	fileWriterConstructor, paramConverter, err := p.initializeAssembler(opts.Assembler)
	if err != nil {
		return nil, fmt.Errorf("initializing assembler: %w", err)
	}

	dis, err := p.createDisassemblerForSystem(system, paramConverter, cart, disasmOpts, fileWriterConstructor)
	if err != nil {
		return nil, fmt.Errorf("creating disassembler: %w", err)
	}
	return dis, nil
}

// initializeAssembler creates the file writer constructor and parameter converter for the specified assembler.
func (p *Pipeline) initializeAssembler(assemblerName string) (disasm.FileWriterConstructor, parameter.Converter, error) {
	var fileWriterConstructor disasm.FileWriterConstructor
	var paramCfg parameter.Config

	switch strings.ToLower(assemblerName) {
	case assembler.Asm6:
		fileWriterConstructor = asm6.New
		paramCfg = asm6.ParamConfig

	case assembler.Ca65:
		fileWriterConstructor = ca65.New
		paramCfg = ca65.ParamConfig

	case assembler.Nesasm:
		fileWriterConstructor = nesasm.New
		paramCfg = nesasm.ParamConfig

	case assembler.Retroasm:
		fileWriterConstructor = retroasm.New
		paramCfg = retroasm.ParamConfig

	default:
		return nil, parameter.Converter{}, fmt.Errorf("unsupported assembler '%s'", assemblerName)
	}

	return fileWriterConstructor, parameter.New(paramCfg), nil
}

// createDisassemblerForSystem creates a disassembler for the specified system architecture.
func (p *Pipeline) createDisassemblerForSystem(system arch.System, paramConverter parameter.Converter,
	cart *cartridge.Cartridge, disasmOpts options.Disassembler, fileWriterConstructor disasm.FileWriterConstructor) (*disasm.Disasm, error) {

	switch system {
	case arch.NES:
		archImpl := m6502.New(p.logger, paramConverter)
		archImpl.SetOptions(disasmOpts) // Set options before disasm.New for BankWindowSize
		dis, err := disasm.New(p.logger, archImpl, cart, disasmOpts, fileWriterConstructor)
		if err != nil {
			return nil, fmt.Errorf("creating m6502 disassembler: %w", err)
		}
		return dis, nil
	case arch.CHIP8System:
		archImpl := chip8.New()
		dis, err := disasm.New(p.logger, archImpl, cart, disasmOpts, fileWriterConstructor)
		if err != nil {
			return nil, fmt.Errorf("creating chip8 disassembler: %w", err)
		}
		return dis, nil
	default:
		return nil, fmt.Errorf("unsupported system '%s'", system)
	}
}

// runDisassembly executes the disassembly process.
func (p *Pipeline) runDisassembly(ctx context.Context, dis *disasm.Disasm, writer io.Writer,
	opts options.Disassembler, outputPath string) (*program.Program, error) {

	newBankWriter := func(bankName string) (io.WriteCloser, error) {
		if !opts.SplitBanks {
			return &nopCloser{writer}, nil
		}
		bankFile := generateBankFilename(outputPath, bankName)
		return os.Create(bankFile)
	}

	result, err := dis.Process(ctx, writer, newBankWriter)
	if err != nil {
		return nil, fmt.Errorf("processing disassembly: %w", err)
	}
	return result, nil
}

// printInfo prints information about the ROM being processed.
func (p *Pipeline) printInfo(opts options.Program, cart *cartridge.Cartridge, system arch.System) {
	if opts.Quiet {
		return
	}

	switch system {
	case arch.NES:
		p.logger.Info("Processing NES ROM",
			log.String("file", opts.Input),
			log.Uint16("mapper", cart.Mapper),
			log.String("assembler", opts.Assembler),
		)
		if cart.Mapper != 0 && cart.Mapper != 3 {
			p.logger.Warn("Support for this mapper is experimental, multi bank mapper support is still in development")
		}

	case arch.CHIP8System:
		p.logger.Info("Processing Chip-8 ROM",
			log.String("file", opts.Input),
			log.String("assembler", opts.Assembler),
		)
	}
}

// nopCloser wraps an io.Writer to add a no-op Close method.
type nopCloser struct {
	io.Writer
}

func (nc *nopCloser) Close() error {
	return nil
}

// generateBankFilename creates a per-bank output filename from the main output path
// and the bank name. For example, ("output.asm", "PRG_BANK_3") -> "output_bank_3.asm".
func generateBankFilename(outputPath, bankName string) string {
	ext := filepath.Ext(outputPath)
	base := outputPath[:len(outputPath)-len(ext)]

	// Convert bank name to lowercase filename suffix: "PRG_BANK_3" -> "bank_3"
	suffix := strings.TrimPrefix(strings.ToLower(bankName), "prg_")

	return base + "_" + suffix + ext
}
