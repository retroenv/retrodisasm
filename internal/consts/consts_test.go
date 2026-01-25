package consts

import (
	"errors"
	"testing"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

type mockArch struct {
	constants map[uint16]Constant
	err       error
}

func (m *mockArch) Constants() (map[uint16]Constant, error) {
	return m.constants, m.err
}

type mockOpcode struct {
	reads  bool
	writes bool
}

func (m *mockOpcode) Addressing() int                      { return 0 }
func (m *mockOpcode) Instruction() instruction.Instruction { return nil }
func (m *mockOpcode) ReadsMemory() bool                    { return m.reads }
func (m *mockOpcode) WritesMemory() bool                   { return m.writes }
func (m *mockOpcode) ReadWritesMemory() bool               { return false }

func TestNew(t *testing.T) {
	t.Run("creates with constants", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL"},
			},
		}

		consts, err := New(arch)

		assert.NoError(t, err)
		assert.NotNil(t, consts)
		ctrl, ok := consts.Get(0x2000)
		assert.True(t, ok)
		assert.Equal(t, "PPU_CTRL", ctrl.Read)
	})

	t.Run("returns error from architecture", func(t *testing.T) {
		arch := &mockArch{err: errors.New("test error")} //nolint:err113 // test error

		consts, err := New(arch)

		assert.Error(t, err)
		assert.Nil(t, consts)
	})
}

//nolint:funlen // test functions can be long
func TestReplaceParameter(t *testing.T) {
	tests := []struct {
		name       string
		constant   Constant
		opcode     *mockOpcode
		param      string
		wantResult string
		wantOK     bool
		shouldMark bool
	}{
		{
			name:       "replaces read parameter",
			constant:   Constant{Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"},
			opcode:     &mockOpcode{reads: true},
			param:      "$2000",
			wantResult: "PPU_CTRL",
			wantOK:     true,
			shouldMark: true,
		},
		{
			name:       "replaces write parameter",
			constant:   Constant{Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"},
			opcode:     &mockOpcode{writes: true},
			param:      "$2000",
			wantResult: "PPU_CTRL_W",
			wantOK:     true,
			shouldMark: true,
		},
		{
			name:       "replaces indexed parameter",
			constant:   Constant{Address: 0x2000, Read: "PPU_CTRL", Write: ""},
			opcode:     &mockOpcode{reads: true},
			param:      "$2000,X",
			wantResult: "PPU_CTRL,X",
			wantOK:     true,
			shouldMark: true,
		},
		{
			name:       "no replacement when name missing",
			constant:   Constant{Address: 0x2000, Read: "PPU_CTRL", Write: ""},
			opcode:     &mockOpcode{writes: true},
			param:      "$2000",
			wantResult: "$2000",
			wantOK:     true,
			shouldMark: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arch := &mockArch{
				constants: map[uint16]Constant{
					tt.constant.Address: tt.constant,
				},
			}
			consts, _ := New(arch)

			result, ok := consts.ReplaceParameter(tt.constant.Address, tt.opcode, tt.param)

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantResult, result)
			assert.Equal(t, tt.shouldMark, consts.IsUsed(tt.constant.Address))
		})
	}

	t.Run("non-existent constant", func(t *testing.T) {
		consts, _ := New(&mockArch{constants: map[uint16]Constant{}})

		result, ok := consts.ReplaceParameter(0x8000, &mockOpcode{reads: true}, "$8000")

		assert.False(t, ok)
		assert.Equal(t, "", result)
	})
}

func TestProcess(t *testing.T) {
	t.Run("processes used constants to all banks", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL"},
				0x2001: {Address: 0x2001, Read: "PPU_MASK", Write: "PPU_MASK"},
			},
		}
		consts, _ := New(arch)
		consts.AddBank()
		consts.AddBank()

		consts.MarkUsed(0x2000)
		consts.Process()

		bank0 := consts.Bank(0)
		bank1 := consts.Bank(1)

		assert.True(t, bank0.Used().Contains(0x2000))
		assert.False(t, bank0.Used().Contains(0x2001))
		assert.True(t, bank1.Used().Contains(0x2000))
	})
}

func TestSetToProgram(t *testing.T) {
	t.Run("sets used constants", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"},
			},
		}
		consts, _ := New(arch)
		consts.MarkUsed(0x2000)

		app := &program.Program{Constants: map[string]uint16{}}
		consts.SetToProgram(app)

		assert.Equal(t, uint16(0x2000), app.Constants["PPU_CTRL"])
		assert.Equal(t, uint16(0x2000), app.Constants["PPU_CTRL_W"])
	})

	t.Run("only sets if name defined", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: ""},
			},
		}
		consts, _ := New(arch)
		consts.MarkUsed(0x2000)

		app := &program.Program{Constants: map[string]uint16{}}
		consts.SetToProgram(app)

		assert.Len(t, app.Constants, 1)
		assert.Equal(t, uint16(0x2000), app.Constants["PPU_CTRL"])
	})
}

//nolint:funlen // test functions can be long
func TestAssignBankConstants(t *testing.T) {
	t.Run("assigns constants with read and write names", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"},
			},
		}
		consts, _ := New(arch)
		consts.AddBank()

		bank := consts.Bank(0)
		consts.AddBankItem(bank, 0x2000, Constant{Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"})

		prgBank := program.NewPRGBank(0x4000)
		consts.AssignBankConstants(0, prgBank)

		assert.Equal(t, uint16(0x2000), prgBank.Constants["PPU_CTRL"])
		assert.Equal(t, uint16(0x2000), prgBank.Constants["PPU_CTRL_W"])
	})

	t.Run("handles empty bank", func(t *testing.T) {
		arch := &mockArch{constants: map[uint16]Constant{}}
		consts, _ := New(arch)
		consts.AddBank()

		prgBank := program.NewPRGBank(0x4000)
		consts.AssignBankConstants(0, prgBank)

		assert.Len(t, prgBank.Constants, 0)
	})

	t.Run("assigns multiple constants", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"},
				0x2001: {Address: 0x2001, Read: "PPU_MASK", Write: "PPU_MASK_W"},
			},
		}
		consts, _ := New(arch)
		consts.AddBank()

		bank := consts.Bank(0)
		consts.AddBankItem(bank, 0x2000, Constant{Address: 0x2000, Read: "PPU_CTRL", Write: "PPU_CTRL_W"})
		consts.AddBankItem(bank, 0x2001, Constant{Address: 0x2001, Read: "PPU_MASK", Write: "PPU_MASK_W"})

		prgBank := program.NewPRGBank(0x4000)
		consts.AssignBankConstants(0, prgBank)

		assert.Len(t, prgBank.Constants, 4)
		assert.Equal(t, uint16(0x2000), prgBank.Constants["PPU_CTRL"])
		assert.Equal(t, uint16(0x2000), prgBank.Constants["PPU_CTRL_W"])
		assert.Equal(t, uint16(0x2001), prgBank.Constants["PPU_MASK"])
		assert.Equal(t, uint16(0x2001), prgBank.Constants["PPU_MASK_W"])
	})

	t.Run("skips empty constant names", func(t *testing.T) {
		arch := &mockArch{
			constants: map[uint16]Constant{
				0x2000: {Address: 0x2000, Read: "PPU_CTRL", Write: ""},
			},
		}
		consts, _ := New(arch)
		consts.AddBank()

		bank := consts.Bank(0)
		consts.AddBankItem(bank, 0x2000, Constant{Address: 0x2000, Read: "PPU_CTRL", Write: ""})

		prgBank := program.NewPRGBank(0x4000)
		consts.AssignBankConstants(0, prgBank)

		assert.Len(t, prgBank.Constants, 1)
		assert.Equal(t, uint16(0x2000), prgBank.Constants["PPU_CTRL"])
	})
}
