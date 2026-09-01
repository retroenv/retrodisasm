// Package m6502 provides a 6502 architecture specific disassembler code.
package m6502

import (
	"errors"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/consts"
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/jumpengine"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/vars"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/log"
	"github.com/retroenv/retrogolib/set"
)

// disasm defines the minimal interface needed from the disassembler.
type disasm interface {
	// AddAddressToParse adds an address to the list to be processed.
	AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool)
	// Cart returns the loaded cartridge.
	Cart() *cartridge.Cartridge
	// ChangeAddressRangeToCodeAsData sets a range of code address to code as data types.
	ChangeAddressRangeToCodeAsData(address uint16, data []byte)
	// IsBranchDestination checks if an address is a branch destination.
	IsBranchDestination(address uint16) bool
	// MarkAddressAsUnreachable marks an address as unreachable code.
	MarkAddressAsUnreachable(address uint16)
	// Options returns the disassembler options.
	Options() options.Disassembler
	// ProgramCounter returns the current program counter of the execution tracer.
	ProgramCounter() uint16
	// ReadMemory reads a byte from the memory at the given address.
	ReadMemory(address uint16) (byte, error)
	// ReadMemoryWord reads a word from the memory at the given address.
	ReadMemoryWord(address uint16) (uint16, error)
	// SetCodeBaseAddress sets the code base address.
	SetCodeBaseAddress(address uint16)
	// SetHandlers sets the program vector handlers.
	SetHandlers(handlers program.Handlers)
	// SetVectorsStartAddress sets the start address of the vectors.
	SetVectorsStartAddress(address uint16)
}

// Dependencies contains the dependencies needed by Arch6502.
type Dependencies struct {
	Disasm     disasm
	Mapper     offset.Mapper
	JumpEngine *jumpengine.JumpEngine
	Vars       *vars.Vars
	Consts     *consts.Consts
}

type Arch6502 struct {
	converter                      parameter.Converter
	dis                            disasm
	jumpEngine                     *jumpengine.JumpEngine
	logger                         *log.Logger
	mapper                         offset.Mapper
	vars                           *vars.Vars
	consts                         *consts.Consts
	codeBaseAddress                uint16
	complementaryBranchPairs       []ComplementaryBranchPair
	complementaryBranchSecondAddrs set.Set[uint16]      // addresses of second branches in complementary pairs
	options                        options.Disassembler // Stored options for early access (before dependency injection)
}

// New returns a new 6502 architecture configuration.
func New(logger *log.Logger, converter parameter.Converter) *Arch6502 {
	return &Arch6502{
		converter:                      converter,
		logger:                         logger,
		complementaryBranchSecondAddrs: set.New[uint16](),
	}
}

// InjectDependencies sets the required dependencies for this architecture.
func (ar *Arch6502) InjectDependencies(deps Dependencies) {
	ar.dis = deps.Disasm
	ar.mapper = deps.Mapper
	ar.jumpEngine = deps.JumpEngine
	ar.vars = deps.Vars
	ar.consts = deps.Consts
}

// SetOptions sets disassembler options before dependency injection.
// This is needed because BankWindowSize is called before InjectDependencies.
func (ar *Arch6502) SetOptions(opts options.Disassembler) {
	ar.options = opts
}

// SetCodeBaseAddress sets the code base address for this architecture.
func (ar *Arch6502) SetCodeBaseAddress(address uint16) {
	ar.codeBaseAddress = address
}

// LastCodeAddress returns the last possible address of code.
// This is used in systems where the last address is reserved for
// the interrupt vector table.
func (ar *Arch6502) LastCodeAddress() uint16 {
	return cpu6502.InterruptVectorStartAddress
}

func (ar *Arch6502) ProcessOffset(address uint16, offsetInfo *offset.DisasmOffset) (bool, error) {
	inspectCode, err := ar.initializeOffsetInfo(offsetInfo)
	if err != nil {
		return false, err
	}
	if !inspectCode {
		return false, nil
	}

	op := offsetInfo.Opcode
	instruction := op.Instruction()
	name := instruction.Name()
	pc := ar.dis.ProgramCounter()

	if op.Addressing() == int(cpu6502.ImpliedAddressing) {
		offsetInfo.Code = name
	} else {
		params, err := ar.processParamInstruction(pc, offsetInfo)
		if err != nil {
			if errors.Is(err, errInstructionOverlapsIRQHandlers) {
				ar.handleInstructionIRQOverlap(address, offsetInfo)
				return true, nil
			}
			return false, err
		}
		if ar.truncateInstructionAtMappedBankBoundary(address, offsetInfo) {
			return true, nil
		}
		offsetInfo.Code = fmt.Sprintf("%s %s", name, params)
	}

	// Check for complementary branch sequences (BNE+BEQ, etc.) that create unconditional branches
	// Record the pair for post-processing after all branch destinations are known
	if ar.DetectComplementaryBranchSequence(pc, offsetInfo) {
		nextAddress := pc + uint16(len(offsetInfo.Data))
		nextByte, _ := ar.dis.ReadMemory(nextAddress)
		nextOpcode := cpu6502.Opcodes[nextByte]

		ar.complementaryBranchPairs = append(ar.complementaryBranchPairs, ComplementaryBranchPair{
			FirstAddress:  pc,
			SecondAddress: nextAddress,
			FirstBranch:   instruction.Name(),
			SecondBranch:  nextOpcode.Instruction.Name,
		})
		// Track the second branch address so we don't continue parsing past it
		ar.complementaryBranchSecondAddrs.Add(nextAddress)
	}

	// Check if this is the second instruction of a complementary branch pair
	// If so, don't add the following address to parse (it's unreachable code)
	isSecondComplementaryBranch := ar.complementaryBranchSecondAddrs.Contains(pc)

	if _, ok := cpu6502.NotExecutingFollowingOpcodeInstructions[name]; ok {
		if err := ar.checkForJumpEngineJmp(pc, offsetInfo); err != nil {
			return false, err
		}
	} else if !isSecondComplementaryBranch {
		// Only add following address if this is not a second complementary branch
		opcodeLength := uint16(len(offsetInfo.Data))
		followingOpcodeAddress := pc + opcodeLength
		ar.dis.AddAddressToParse(followingOpcodeAddress, offsetInfo.Context, address, instruction, false)
		if err := ar.checkForJumpEngineCall(pc, offsetInfo); err != nil {
			return false, err
		}
	}

	return true, nil
}

// PostProcessCode performs architecture-specific post-processing after all code is disassembled.
func (ar *Arch6502) PostProcessCode() error {
	ar.ProcessComplementaryBranches()
	return nil
}

// BankWindowSize returns the bank window size.
// Returns 0 for binary mode to use single-bank mode (no bank windowing).
func (ar *Arch6502) BankWindowSize(_ *cartridge.Cartridge) int {
	// Debug: check if options.Binary is set
	if ar.options.Binary {
		return 0 // Single-bank mode for binary files
	}
	return 0x2000 // Multi-bank mode for NES ROMs
}

func (ar *Arch6502) truncateInstructionAtMappedBankBoundary(address uint16, offsetInfo *offset.DisasmOffset) bool {
	ownedBytes := ar.instructionBytesInMappedBank(address, len(offsetInfo.Data))
	if ownedBytes == len(offsetInfo.Data) {
		return false
	}

	// A CPU instruction may cross from a switchable window into a fixed bank,
	// but its bytes are not contiguous in the cartridge image.
	offsetInfo.Data = offsetInfo.Data[:ownedBytes]
	offsetInfo.SetType(program.CodeAsData)
	ar.dis.ChangeAddressRangeToCodeAsData(address, offsetInfo.Data)
	return true
}

func (ar *Arch6502) instructionBytesInMappedBank(address uint16, length int) int {
	startBank := ar.mapper.MappedBank(address)
	for i := 1; i < length; i++ {
		if ar.mapper.MappedBank(address+uint16(i)).ID() != startBank.ID() {
			return i
		}
	}
	return length
}
