//nolint:goconst // test labels don't need to be constants
package mapper

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

//nolint:funlen // test functions can be long
func TestClassifyRemainingAsData(t *testing.T) {
	t.Run("classifies empty unowned offsets as data", func(t *testing.T) {
		cart := &cartridge.Cartridge{
			PRG: []byte{0x10, 0x20, 0x30, 0x40, 0x50},
		}
		arch := &mockArchitecture{bankWindowSize: 0}

		mapper, err := New(arch, cart)
		assert.NoError(t, err)

		mapper.banks[0].offsets[0].SetType(program.CodeOffset)
		mapper.banks[0].offsets[2].SetType(program.DataOffset)
		mapper.banks[0].offsets[4].SetType(program.FunctionReference)

		mapper.ClassifyRemainingAsData()

		for i, value := range cart.PRG {
			offsetInfo := mapper.banks[0].offsets[i]
			assert.Equal(t, []byte{value}, offsetInfo.Data)
			assert.True(t, offsetInfo.IsType(program.DataOffset))
			assert.False(t, offsetInfo.IsType(program.CodeOffset|program.FunctionReference|program.JumpTable))
		}
	})

	t.Run("preserves operands owned by an instruction", func(t *testing.T) {
		cart := &cartridge.Cartridge{
			PRG: []byte{0x4c, 0x34, 0x12, 0xea},
		}
		arch := &mockArchitecture{bankWindowSize: 0}

		mapper, err := New(arch, cart)
		assert.NoError(t, err)

		owner := mapper.banks[0].offsets[0]
		owner.Data = cart.PRG[:3]
		owner.SetType(program.CodeOffset)
		mapper.banks[0].offsets[1].SetType(program.CodeOffset)
		mapper.banks[0].offsets[2].SetType(program.CodeOffset)

		mapper.ClassifyRemainingAsData()

		assert.Equal(t, cart.PRG[:3], owner.Data)
		assert.Empty(t, mapper.banks[0].offsets[1].Data)
		assert.Empty(t, mapper.banks[0].offsets[2].Data)
		assert.Equal(t, []byte{0xea}, mapper.banks[0].offsets[3].Data)
	})

	t.Run("handles all unclassified bank", func(t *testing.T) {
		cart := &cartridge.Cartridge{
			PRG: []byte{0x00, 0x01, 0x02},
		}
		arch := &mockArchitecture{bankWindowSize: 0}

		mapper, err := New(arch, cart)
		assert.NoError(t, err)

		mapper.ClassifyRemainingAsData()

		for i := range mapper.banks[0].offsets {
			assert.Equal(t, []byte{cart.PRG[i]}, mapper.banks[0].offsets[i].Data)
		}
	})

	t.Run("handles multiple banks", func(t *testing.T) {
		cart := &cartridge.Cartridge{
			PRG: make([]byte, 0x10000), // Large enough for 2 banks
		}
		arch := &mockArchitecture{bankWindowSize: 0x4000}

		mapper, err := New(arch, cart)
		assert.NoError(t, err)

		mapper.banks[0].offsets[0].Data = []byte{0xea}
		mapper.banks[0].offsets[0].SetType(program.CodeOffset)
		mapper.banks[1].offsets[0].Data = []byte{0xea}
		mapper.banks[1].offsets[0].SetType(program.CodeOffset)

		mapper.ClassifyRemainingAsData()

		assert.Equal(t, []byte{0xea}, mapper.banks[0].offsets[0].Data)
		assert.Equal(t, []byte{0}, mapper.banks[0].offsets[1].Data)
		assert.Equal(t, []byte{0xea}, mapper.banks[1].offsets[0].Data)
		assert.Equal(t, []byte{0}, mapper.banks[1].offsets[1].Data)
	})
}

func TestSetProgramBanks_SingleBank(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x100),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockVars := &mockVariableManager{}
	mockConsts := &mockConstantManager{}
	mockDis := &mockDisasm{
		opts: options.Disassembler{},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   mockVars,
		Consts: mockConsts,
	})
	mapper.InitializeDependencyBanks()

	// Set some test data
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	mapper.banks[0].offsets[0].Code = "LDA #$00"
	mapper.banks[0].offsets[0].Data = []byte{0xA9, 0x00}

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify program bank was created
	assert.Len(t, app.PRG, 1)
	assert.Equal(t, "CODE", app.PRG[0].Name)
	assert.Equal(t, uint16(0x8000), app.PRG[0].BaseAddress)
	assert.Len(t, app.PRG[0].Offsets, 0x100)

	// Verify AssignBankVariables and AssignBankConstants were called
	assert.Equal(t, 1, mockVars.setBankCalls)
	assert.Equal(t, 1, mockConsts.setBankCalls)
}

func TestSetProgramBanks_MultiBanks(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10000), // Large enough for 2 banks
	}
	arch := &mockArchitecture{bankWindowSize: 0x4000}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockVars := &mockVariableManager{}
	mockConsts := &mockConstantManager{}
	mockDis := &mockDisasm{
		opts: options.Disassembler{},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   mockVars,
		Consts: mockConsts,
	})
	mapper.InitializeDependencyBanks()

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify multiple program banks were created
	assert.Len(t, app.PRG, 2)
	assert.Equal(t, "PRG_BANK_0", app.PRG[0].Name)
	assert.Equal(t, "PRG_BANK_1", app.PRG[1].Name)
	assert.Equal(t, uint16(0x8000), app.PRG[0].BaseAddress)
	assert.Equal(t, uint16(0x8000), app.PRG[1].BaseAddress)

	// Verify AssignBankVariables and AssignBankConstants were called for each bank
	assert.Equal(t, 2, mockVars.setBankCalls)
	assert.Equal(t, 2, mockConsts.setBankCalls)
}

func TestSetProgramBanks_WithOffsetComments(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{
		opts: options.Disassembler{
			OffsetComments: true,
		},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   &mockVariableManager{},
		Consts: &mockConstantManager{},
	})
	mapper.InitializeDependencyBanks()

	// Set code offset
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	mapper.banks[0].offsets[0].Code = "NOP"
	mapper.banks[0].offsets[0].Data = []byte{0xEA}
	mapper.banks[0].offsets[0].Label = "start"

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify offset comment was added
	assert.Equal(t, "$8000", app.PRG[0].Offsets[0].Comment)
	assert.True(t, app.PRG[0].Offsets[0].HasAddressComment)
}

func TestSetProgramBanks_WithHexComments(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{
		opts: options.Disassembler{
			HexComments: true,
		},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   &mockVariableManager{},
		Consts: &mockConstantManager{},
	})
	mapper.InitializeDependencyBanks()

	// Set code offset with data
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	mapper.banks[0].offsets[0].Code = "LDA #$FF"
	mapper.banks[0].offsets[0].Data = []byte{0xA9, 0xFF}
	mapper.banks[0].offsets[0].Label = "start"

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify hex comment was added
	assert.Equal(t, "A9 FF", app.PRG[0].Offsets[0].Comment)
}

func TestSetProgramBanks_WithBothComments(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{
		opts: options.Disassembler{
			OffsetComments: true,
			HexComments:    true,
		},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   &mockVariableManager{},
		Consts: &mockConstantManager{},
	})
	mapper.InitializeDependencyBanks()

	// Set code offset
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	mapper.banks[0].offsets[0].Code = "LDA #$FF"
	mapper.banks[0].offsets[0].Data = []byte{0xA9, 0xFF}
	mapper.banks[0].offsets[0].Label = "start"

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify both comments were added with proper spacing
	assert.Equal(t, "$8000  A9 FF", app.PRG[0].Offsets[0].Comment)
}

func TestSetProgramBanks_WithBranchTarget(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{
		opts: options.Disassembler{},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   &mockVariableManager{},
		Consts: &mockConstantManager{},
	})
	mapper.InitializeDependencyBanks()

	// Set code offset with branching target
	mapper.banks[0].offsets[0].SetType(program.CodeOffset)
	mapper.banks[0].offsets[0].Code = "JMP"
	mapper.banks[0].offsets[0].BranchingTo = "label_8003"
	mapper.banks[0].offsets[0].Data = []byte{0x4C, 0x03, 0x80}
	mapper.banks[0].offsets[0].Label = "start"

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify branch target was added to code
	assert.Equal(t, "JMP label_8003", app.PRG[0].Offsets[0].Code)
}

func TestSetProgramBanks_FunctionReference(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{
		opts: options.Disassembler{},
	}

	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
		Vars:   &mockVariableManager{},
		Consts: &mockConstantManager{},
	})
	mapper.InitializeDependencyBanks()

	// Set function reference
	mapper.banks[0].offsets[0].SetType(program.FunctionReference)
	mapper.banks[0].offsets[0].BranchingTo = "func_8100"
	mapper.banks[0].offsets[0].Data = []byte{0x00, 0x81}
	mapper.banks[0].offsets[0].Label = "func_ptr"

	app := &program.Program{}
	err = mapper.SetProgramBanks(app)
	assert.NoError(t, err)

	// Verify function reference was formatted as .word
	assert.Equal(t, ".word func_8100", app.PRG[0].Offsets[0].Code)
}
