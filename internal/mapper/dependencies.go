package mapper

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
)

// architecture defines the minimal interface needed from arch.Architecture
type architecture interface {
	// BankWindowSize returns the bank window size.
	BankWindowSize(cart *cartridge.Cartridge) int
}

// variableManager defines the minimal interface needed for variable management
type variableManager interface {
	// AddBank adds a new bank to the variable manager.
	AddBank()
	// AssignBankVariables assigns the used variables to the bank for outputting.
	AssignBankVariables(bankID int, prgBank *program.PRGBank)
}

// constantManager defines the minimal interface needed for constant management
type constantManager interface {
	// AddBank adds a new bank to the constant manager.
	AddBank()
	// AssignBankConstants assigns the used constants to the bank for outputting.
	AssignBankConstants(bankID int, prgBank *program.PRGBank)
}

// disasm defines the minimal interface needed from the disassembler.
type disasm interface {
	// AddAddressToParse adds an address to the list to be processed.
	AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool)
	// Options returns the disassembler options.
	Options() options.Disassembler
}

// Dependencies contains the dependencies needed by Mapper.
type Dependencies struct {
	Disasm disasm
	Vars   variableManager
	Consts constantManager
}

// InjectDependencies sets the required dependencies for this mapper.
func (m *Mapper) InjectDependencies(deps Dependencies) {
	m.dis = deps.Disasm
	m.vars = deps.Vars
	m.consts = deps.Consts
}

// InitializeDependencyBanks initializes the bank structures in the injected
// vars and consts managers to match this mapper's bank configuration.
// This must be called after InjectDependencies.
func (m *Mapper) InitializeDependencyBanks() {
	for range m.banks {
		m.vars.AddBank()
		m.consts.AddBank()
	}
}
