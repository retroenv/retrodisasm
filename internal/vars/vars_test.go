package vars

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/assert"
)

type mockArch struct {
	indexed bool
}

func (m *mockArch) IsAddressingIndexed(opcode instruction.Opcode) bool {
	return m.indexed
}

func (m *mockArch) ProcessVariableUsage(offsetInfo *offset.DisasmOffset, reference string) error {
	return nil
}

type mockMapper struct {
	offsets map[uint16]*offset.DisasmOffset
}

type mockBank struct {
	id int
}

func (m *mockBank) ID() int { return m.id }

func (m *mockBank) OffsetInfo(index uint16) *offset.DisasmOffset {
	return &offset.DisasmOffset{}
}

func (m *mockMapper) MappedBank(address uint16) offset.MappedBank {
	return &mockBank{id: 0}
}

func (m *mockMapper) MappedBankIndex(address uint16) uint16 {
	return 0
}

func (m *mockMapper) OffsetInfo(address uint16) *offset.DisasmOffset {
	if off, ok := m.offsets[address]; ok {
		return off
	}
	off := &offset.DisasmOffset{}
	off.Data = []byte{}
	return off
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

func setup() *Vars {
	vars := New(&mockArch{})
	mapper := &mockMapper{offsets: make(map[uint16]*offset.DisasmOffset)}
	vars.InjectDependencies(Dependencies{Mapper: mapper})
	return vars
}

func TestNew(t *testing.T) {
	vars := New(&mockArch{})

	assert.NotNil(t, vars)
	assert.NotNil(t, vars.Manager)
}

func TestInjectDependencies(t *testing.T) {
	vars := New(&mockArch{})
	mapper := &mockMapper{}

	vars.InjectDependencies(Dependencies{Mapper: mapper})

	assert.Equal(t, mapper, vars.mapper)
}

func TestAddReference(t *testing.T) {
	tests := []struct {
		name       string
		opcode     *mockOpcode
		force      bool
		wantAdded  bool
		wantReads  bool
		wantWrites bool
	}{
		{name: "read operation", opcode: &mockOpcode{reads: true}, wantAdded: true, wantReads: true},
		{name: "write operation", opcode: &mockOpcode{writes: true}, wantAdded: true, wantWrites: true},
		{name: "non-memory without force", opcode: &mockOpcode{}, force: false, wantAdded: false},
		{name: "forced", opcode: &mockOpcode{}, force: true, wantAdded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := setup()

			vars.AddReference(0x0010, 0x8000, tt.opcode, tt.force)

			varInfo, ok := vars.Get(0x0010)
			assert.Equal(t, tt.wantAdded, ok)
			if ok {
				assert.Equal(t, tt.wantReads, varInfo.reads)
				assert.Equal(t, tt.wantWrites, varInfo.writes)
			}
		})
	}

	t.Run("marks indexed usage", func(t *testing.T) {
		vars := New(&mockArch{indexed: true})
		mapper := &mockMapper{offsets: make(map[uint16]*offset.DisasmOffset)}
		vars.InjectDependencies(Dependencies{Mapper: mapper})

		vars.AddReference(0x0010, 0x8000, &mockOpcode{reads: true}, false)

		varInfo, _ := vars.Get(0x0010)
		assert.True(t, varInfo.indexedUsage)
	})

	t.Run("accumulates multiple usages", func(t *testing.T) {
		vars := setup()
		opcode := &mockOpcode{reads: true}

		vars.AddReference(0x0010, 0x8000, opcode, false)
		vars.AddReference(0x0010, 0x8001, opcode, false)

		varInfo, _ := vars.Get(0x0010)
		assert.Len(t, varInfo.usageAt, 2)
	})
}

func TestGenerateVariableName(t *testing.T) {
	vars := New(&mockArch{})

	tests := []struct {
		name         string
		offsetInfo   *offset.DisasmOffset
		indexedUsage bool
		address      uint16
		want         string
	}{
		{
			name: "uses existing label",
			offsetInfo: func() *offset.DisasmOffset {
				off := &offset.DisasmOffset{}
				off.Label = "ExistingLabel"
				return off
			}(),
			address: 0x8000,
			want:    "ExistingLabel",
		},
		{
			name: "jump table",
			offsetInfo: func() *offset.DisasmOffset {
				off := &offset.DisasmOffset{}
				off.Type = program.JumpTable
				return off
			}(),
			address: 0x8000,
			want:    "_jump_table_8000",
		},
		{name: "indexed data", offsetInfo: &offset.DisasmOffset{}, indexedUsage: true, address: 0x8000, want: "_data_8000_indexed"},
		{name: "data", offsetInfo: &offset.DisasmOffset{}, indexedUsage: false, address: 0x8000, want: "_data_8000"},
		{name: "indexed var", offsetInfo: nil, indexedUsage: true, address: 0x0010, want: "_var_0010_indexed"},
		{name: "var", offsetInfo: nil, indexedUsage: false, address: 0x0010, want: "_var_0010"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vars.generateVariableName(tt.offsetInfo, tt.indexedUsage, tt.address)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetToProgram(t *testing.T) {
	t.Run("sets used variables", func(t *testing.T) {
		vars := setup()
		vars.AddReference(0x0010, 0x8000, &mockOpcode{reads: true}, false)
		vars.MarkUsed(0x0010)

		varInfo, _ := vars.Get(0x0010)
		varInfo.name = "_var_0010"

		app := &program.Program{Variables: map[string]uint16{}}
		vars.SetToProgram(app)

		assert.Equal(t, uint16(0x0010), app.Variables["_var_0010"])
	})
}

func TestAssignBankVariables(t *testing.T) {
	t.Run("assigns single variable", func(t *testing.T) {
		vars := New(&mockArch{})
		vars.AddBank()
		bank := vars.Bank(0)

		varInfo := &variable{address: 0x0010, name: "_var_0010"}
		bank.Set(0x0010, varInfo)
		bank.Used().Add(0x0010)

		prgBank := program.NewPRGBank(0x4000)
		vars.AssignBankVariables(0, prgBank)

		assert.Equal(t, uint16(0x0010), prgBank.Variables["_var_0010"])
	})

	t.Run("handles empty bank", func(t *testing.T) {
		vars := New(&mockArch{})
		vars.AddBank()

		prgBank := program.NewPRGBank(0x4000)
		vars.AssignBankVariables(0, prgBank)

		assert.Len(t, prgBank.Variables, 0)
	})

	t.Run("assigns multiple variables", func(t *testing.T) {
		vars := New(&mockArch{})
		vars.AddBank()
		bank := vars.Bank(0)

		var1 := &variable{address: 0x0010, name: "_var_0010"}
		var2 := &variable{address: 0x0020, name: "_var_0020"}
		var3 := &variable{address: 0x0030, name: "_var_0030"}

		bank.Set(0x0010, var1)
		bank.Set(0x0020, var2)
		bank.Set(0x0030, var3)
		bank.Used().Add(0x0010)
		bank.Used().Add(0x0020)
		bank.Used().Add(0x0030)

		prgBank := program.NewPRGBank(0x4000)
		vars.AssignBankVariables(0, prgBank)

		assert.Len(t, prgBank.Variables, 3)
		assert.Equal(t, uint16(0x0010), prgBank.Variables["_var_0010"])
		assert.Equal(t, uint16(0x0020), prgBank.Variables["_var_0020"])
		assert.Equal(t, uint16(0x0030), prgBank.Variables["_var_0030"])
	})
}

func TestAddUsage(t *testing.T) {
	vars := New(&mockArch{})
	vars.AddBank()

	varInfo := &variable{address: 0x0010, name: "_var_0010"}
	vars.AddUsage(0, varInfo)

	bank := vars.Bank(0)
	got, ok := bank.Get(0x0010)
	assert.True(t, ok)
	assert.Equal(t, "_var_0010", got.name)
	assert.True(t, bank.Used().Contains(0x0010))
}
