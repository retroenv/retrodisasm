// Package disasm implements a multi retro system disassembler
package disasm

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/retroenv/retrodisasm/internal/arch/chip8"
	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/jumpengine"
	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/vars"
	"github.com/retroenv/retrodisasm/internal/writer"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
	"github.com/retroenv/retrogolib/log"
	"github.com/retroenv/retrogolib/set"
)

type FileWriterConstructor func(app *program.Program, options options.Disassembler,
	mainWriter io.Writer, newBankWriter assembler.NewBankWriter) writer.AssemblerWriter

// architecture defines the minimal interface needed from the architecture.
type architecture interface {
	// AddressingParam returns the address of the param if it references an address.
	AddressingParam(param any) (uint16, bool)
	// BankWindowSize returns the bank window size.
	BankWindowSize(cart *cartridge.Cartridge) int
	// Constants returns the constants translation map.
	Constants() (map[uint16]consts.Constant, error)
	// FormatBranchReference formats an instruction that references a branch or data label.
	FormatBranchReference(offsetInfo *offset.DisasmOffset, label string) string
	// HandleDisambiguousInstructions translates disambiguous instructions into data bytes.
	HandleDisambiguousInstructions(address uint16, offsetInfo *offset.DisasmOffset) bool
	// Initialize the architecture.
	Initialize() error
	// IsAddressingIndexed returns if the opcode is using indexed addressing.
	IsAddressingIndexed(opcode instruction.Opcode) bool
	// LastCodeAddress returns the last possible address of code.
	LastCodeAddress() uint16
	// PostProcessCode performs architecture-specific post-processing after all code is disassembled.
	PostProcessCode() error
	// ProcessOffset processes an offset and returns if the offset was processed and an error if any.
	ProcessOffset(address uint16, offsetInfo *offset.DisasmOffset) (bool, error)
	// ProcessVariableUsage processes the variable usage of an offset.
	ProcessVariableUsage(offsetInfo *offset.DisasmOffset, reference string) error
	// ReadMemory reads a byte from memory at the given address using architecture-specific logic.
	ReadMemory(address uint16) (byte, error)
	// ReadOpParam reads the parameter of an opcode.
	ReadOpParam(addressing int, address uint16) (any, []byte, error)
}

// Disasm implements a disassembler.
type Disasm struct {
	arch    architecture
	logger  *log.Logger
	options options.Disassembler

	pc uint16 // program counter

	cart                  *cartridge.Cartridge
	fileWriterConstructor FileWriterConstructor
	handlers              program.Handlers

	codeBaseAddress     uint16 // codebase address of the cartridge, it is not always 0x8000
	vectorsStartAddress uint16

	constants  *consts.Consts
	jumpEngine *jumpengine.JumpEngine
	vars       *vars.Vars

	branchDestinations   set.Set[uint16] // set of all addresses that are branched to
	unreachableAddresses set.Set[uint16] // set of addresses marked as unreachable (dead code)

	// TODO handle bank switch
	offsetsToParse      []uint16
	offsetsToParseAdded set.Set[uint16]
	offsetsParsed       set.Set[uint16]

	functionReturnsToParse      []uint16
	functionReturnsToParseAdded set.Set[uint16]

	mapper *mapper.Mapper
}

// New creates a new disassembler that uses the passed architecture to implement system
// specific disassembly logic.
func New(logger *log.Logger, ar architecture, cart *cartridge.Cartridge,
	options options.Disassembler, fileWriterConstructor FileWriterConstructor) (*Disasm, error) {

	dis := &Disasm{
		arch:                        ar,
		logger:                      logger,
		options:                     options,
		cart:                        cart,
		fileWriterConstructor:       fileWriterConstructor,
		branchDestinations:          set.New[uint16](),
		unreachableAddresses:        set.New[uint16](),
		offsetsToParseAdded:         set.New[uint16](),
		offsetsParsed:               set.New[uint16](),
		functionReturnsToParseAdded: set.New[uint16](),
	}

	var err error
	dis.constants, err = consts.New(ar)
	if err != nil {
		return nil, fmt.Errorf("creating constants: %w", err)
	}

	if err := dis.initializeComponents(ar, logger, cart); err != nil {
		return nil, err
	}

	if options.CodeDataLog != nil {
		if err = dis.loadCodeDataLog(); err != nil {
			return nil, err
		}
	}

	return dis, nil
}

// Process disassembles the cartridge.
func (dis *Disasm) Process(ctx context.Context, mainWriter io.Writer, newBankWriter assembler.NewBankWriter) (*program.Program, error) {
	if err := dis.followExecutionFlow(ctx); err != nil {
		return nil, err
	}

	// Post-process architecture-specific patterns after all branch destinations are known
	if err := dis.arch.PostProcessCode(); err != nil {
		return nil, fmt.Errorf("post-processing code: %w", err)
	}

	dis.mapper.ClassifyRemainingAsData()
	if err := dis.vars.Process(dis.codeBaseAddress); err != nil {
		return nil, fmt.Errorf("processing variables: %w", err)
	}
	dis.constants.Process()
	dis.processJumpDestinations()

	app, err := dis.convertToProgram()
	if err != nil {
		return nil, err
	}
	fileWriter := dis.fileWriterConstructor(app, dis.options, mainWriter, newBankWriter)
	if err = fileWriter.Write(); err != nil {
		return nil, fmt.Errorf("writing app to file: %w", err)
	}
	return app, nil
}

// Cart returns the loaded cartridge.
func (dis *Disasm) Cart() *cartridge.Cartridge {
	return dis.cart
}

func (dis *Disasm) ProgramCounter() uint16 {
	return dis.pc
}

func (dis *Disasm) SetHandlers(handlers program.Handlers) {
	dis.handlers = handlers
}

func (dis *Disasm) SetCodeBaseAddress(address uint16) {
	dis.codeBaseAddress = address
	dis.mapper.SetCodeBaseAddress(address)

	// Set code base address in architecture if it supports it (e.g., m6502)
	if setter, ok := dis.arch.(interface{ SetCodeBaseAddress(uint16) }); ok {
		setter.SetCodeBaseAddress(address)
	}

	dis.logger.Debug("Code base address",
		log.Hex("address", dis.codeBaseAddress))
}

func (dis *Disasm) SetVectorsStartAddress(address uint16) {
	dis.vectorsStartAddress = address
}

func (dis *Disasm) Options() options.Disassembler {
	return dis.options
}

// ReadMemory delegates to the architecture-specific implementation.
func (dis *Disasm) ReadMemory(address uint16) (byte, error) {
	value, err := dis.arch.ReadMemory(address)
	if err != nil {
		return 0, fmt.Errorf("reading memory at address %04x: %w", address, err)
	}
	return value, nil
}

// ReadMemoryWord reads a word from memory using the architecture-specific ReadMemory method.
func (dis *Disasm) ReadMemoryWord(address uint16) (uint16, error) {
	b, err := dis.ReadMemory(address)
	if err != nil {
		return 0, err
	}
	low := uint16(b)

	b, err = dis.ReadMemory(address + 1)
	if err != nil {
		return 0, err
	}

	high := uint16(b)
	return (high << 8) | low, nil
}

// IsBranchDestination checks if an address is a branch destination.
func (dis *Disasm) IsBranchDestination(address uint16) bool {
	return dis.branchDestinations.Contains(address)
}

// MarkAddressAsUnreachable marks an address as unreachable code.
// This prevents the disassembler from following branches to this address.
func (dis *Disasm) MarkAddressAsUnreachable(address uint16) {
	dis.unreachableAddresses.Add(address)
}

// initializeComponents creates and wires up all the disassembler components.
func (dis *Disasm) initializeComponents(ar architecture, logger *log.Logger, cart *cartridge.Cartridge) error {
	// Create all components with simple constructors
	je := jumpengine.New(logger, ar)
	m, err := mapper.New(ar, cart)
	if err != nil {
		return fmt.Errorf("creating mapper: %w", err)
	}
	v := vars.New(ar)

	// Inject dependencies using InjectDependencies with Dependencies structs
	je.InjectDependencies(jumpengine.Dependencies{
		Disasm: dis,
		Mapper: m,
	})

	m.InjectDependencies(mapper.Dependencies{
		Disasm: dis,
		Vars:   v,
		Consts: dis.constants,
	})
	m.InitializeDependencyBanks()

	v.InjectDependencies(vars.Dependencies{
		Mapper: m,
	})

	// Inject dependencies into architecture
	switch a := ar.(type) {
	case *m6502.Arch6502:
		a.InjectDependencies(m6502.Dependencies{
			Disasm:     dis,
			Mapper:     m,
			JumpEngine: je,
			Vars:       v,
			Consts:     dis.constants,
		})
	case *chip8.Chip8:
		a.InjectDependencies(chip8.Dependencies{
			Disasm: dis,
			Mapper: m,
		})
	}

	// Assign to disassembler fields
	dis.jumpEngine = je
	dis.mapper = m
	dis.vars = v

	// Call initialization after all dependencies are wired
	if err := ar.Initialize(); err != nil {
		return fmt.Errorf("initializing architecture: %w", err)
	}

	return nil
}

// converts the internal disassembly representation to a program type that will be used by
// the chosen assembler output instance to generate the asm file.
func (dis *Disasm) convertToProgram() (*program.Program, error) {
	app := program.New(dis.cart)
	app.CodeBaseAddress = dis.codeBaseAddress
	app.VectorsStartAddress = dis.vectorsStartAddress
	app.Handlers = dis.handlers

	if err := dis.mapper.SetProgramBanks(app); err != nil {
		return nil, fmt.Errorf("setting program banks: %w", err)
	}

	dis.constants.SetToProgram(app)
	dis.vars.SetToProgram(app)

	crc32q := crc32.MakeTable(crc32.IEEE)
	app.Checksums.PRG = crc32.Checksum(dis.cart.PRG, crc32q)
	app.Checksums.CHR = crc32.Checksum(dis.cart.CHR, crc32q)
	app.Checksums.Overall = crc32.Checksum(append(dis.cart.PRG, dis.cart.CHR...), crc32q)

	return app, nil
}

func (dis *Disasm) loadCodeDataLog() error {
	prgFlags, err := codedatalog.LoadFile(dis.cart, dis.options.CodeDataLog)
	if err != nil {
		return fmt.Errorf("loading code/data log file: %w", err)
	}

	dis.mapper.ApplyCodeDataLog(prgFlags)
	return nil
}
