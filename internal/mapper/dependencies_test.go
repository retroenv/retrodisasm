package mapper

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

// mockDisasm is a mock implementation of the disasm interface
type mockDisasm struct {
	addedAddresses []uint16
	opts           options.Disassembler
}

func (m *mockDisasm) AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction,
	isABranchDestination bool) {

	m.addedAddresses = append(m.addedAddresses, address)
}

func (m *mockDisasm) Options() options.Disassembler {
	return m.opts
}

// mockVariableManager is a mock implementation of the variableManager interface
type mockVariableManager struct {
	bankCount    int
	setBankCalls int
	lastBankID   int
	lastPrgBank  *program.PRGBank
}

func (m *mockVariableManager) AddBank() {
	m.bankCount++
}

func (m *mockVariableManager) AssignBankVariables(bankID int, prgBank *program.PRGBank) {
	m.setBankCalls++
	m.lastBankID = bankID
	m.lastPrgBank = prgBank
}

// mockConstantManager is a mock implementation of the constantManager interface
type mockConstantManager struct {
	bankCount    int
	setBankCalls int
	lastBankID   int
	lastPrgBank  *program.PRGBank
}

func (m *mockConstantManager) AddBank() {
	m.bankCount++
}

func (m *mockConstantManager) AssignBankConstants(bankID int, prgBank *program.PRGBank) {
	m.setBankCalls++
	m.lastBankID = bankID
	m.lastPrgBank = prgBank
}

func TestInjectDependencies(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	// Create mock dependencies
	mockDis := &mockDisasm{}
	mockVars := &mockVariableManager{}
	mockConsts := &mockConstantManager{}

	deps := Dependencies{
		Disasm: mockDis,
		Vars:   mockVars,
		Consts: mockConsts,
	}

	// Inject dependencies
	mapper.InjectDependencies(deps)

	// Verify dependencies were set
	assert.NotNil(t, mapper.dis)
	assert.NotNil(t, mapper.vars)
	assert.NotNil(t, mapper.consts)
}

func TestInitializeDependencyBanks_SingleBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x1000),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	mockVars := &mockVariableManager{}
	mockConsts := &mockConstantManager{}

	mapper.InjectDependencies(Dependencies{
		Vars:   mockVars,
		Consts: mockConsts,
	})

	// Initialize dependency banks
	mapper.InitializeDependencyBanks()

	// Should have added 1 bank to each manager
	assert.Equal(t, 1, mockVars.bankCount)
	assert.Equal(t, 1, mockConsts.bankCount)
}

func TestInitializeDependencyBanks_MultiBanks(t *testing.T) {
	// Create large cartridge to generate multiple banks
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10000), // 64KB = 2 banks of 32KB each
	}
	arch := &mockArchitecture{bankWindowSize: 0x4000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	mockVars := &mockVariableManager{}
	mockConsts := &mockConstantManager{}

	mapper.InjectDependencies(Dependencies{
		Vars:   mockVars,
		Consts: mockConsts,
	})

	mapper.InitializeDependencyBanks()

	// Should have added 2 banks to each manager
	assert.Equal(t, 2, mockVars.bankCount)
	assert.Equal(t, 2, mockConsts.bankCount)
}
