package m6502

import (
	"errors"
	"testing"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
	"github.com/retroenv/retrogolib/set"
)

func TestDetectComplementaryBranchSequence(t *testing.T) {
	tests := []struct {
		name             string
		instruction      byte
		nextInstruction  byte
		expectedDetected bool
		expectedComment  string
	}{
		{"BNE followed by BEQ", 0xD0, 0xF0, true, "unconditional branch pattern (complementary branches)"},
		{"BEQ followed by BNE", 0xF0, 0xD0, true, "unconditional branch pattern (complementary branches)"},
		{"BCC followed by BCS", 0x90, 0xB0, true, "unconditional branch pattern (complementary branches)"},
		{"BCS followed by BCC", 0xB0, 0x90, true, "unconditional branch pattern (complementary branches)"},
		{"BPL followed by BMI", 0x10, 0x30, true, "unconditional branch pattern (complementary branches)"},
		{"BMI followed by BPL", 0x30, 0x10, true, "unconditional branch pattern (complementary branches)"},
		{"BVC followed by BVS", 0x50, 0x70, true, "unconditional branch pattern (complementary branches)"},
		{"BVS followed by BVC", 0x70, 0x50, true, "unconditional branch pattern (complementary branches)"},
		{"BNE followed by BNE (same branch)", 0xD0, 0xD0, false, ""},
		{"BNE followed by LDA", 0xD0, 0xA9, false, ""},
		{"LDA followed by BEQ", 0xA9, 0xF0, false, ""},
		{"BNE followed by BCC (different flags)", 0xD0, 0x90, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected, _ := testBranchSequence(t, tt.instruction, tt.nextInstruction)

			// Detection phase now only detects and logs, doesn't add comments
			// Comments are added during post-processing
			assert.Equal(t, tt.expectedDetected, detected)
		})
	}
}

func TestDetectComplementaryBranchSequenceAtEndOfROM(t *testing.T) {
	rom := []byte{0xD0, 0x02}
	arch, _ := createMockArch(t, rom)

	offsetInfo := &offset.DisasmOffset{}
	offsetInfo.Data = []byte{0xD0, 0x02}
	offsetInfo.Opcode = &Opcode{op: cpu6502.Opcodes[0xD0]}

	detected := arch.DetectComplementaryBranchSequence(0x8000, offsetInfo)
	assert.False(t, detected)
}

func TestProcessComplementaryBranches(t *testing.T) {
	rom := []byte{0xD0, 0x02, 0xF0, 0x00}
	arch, mockDis := createMockArch(t, rom)

	// Record a complementary branch pair
	arch.complementaryBranchPairs = []ComplementaryBranchPair{
		{
			FirstAddress:  0x8000,
			SecondAddress: 0x8002,
			FirstBranch:   "bne",
			SecondBranch:  "beq",
		},
	}

	// Create mock mapper
	mockMapper := &mockMapper{
		offsets: make(map[uint16]*offset.DisasmOffset),
	}
	mockMapper.offsets[0x8000] = &offset.DisasmOffset{}
	mockMapper.offsets[0x8002] = &offset.DisasmOffset{}
	arch.mapper = mockMapper

	// Process without incoming jumps - second should be marked unreachable
	arch.ProcessComplementaryBranches()

	assert.Contains(t, mockMapper.offsets[0x8000].Comment, "unconditional branch pattern")
	assert.True(t, mockDis.unreachableAddresses.Contains(0x8002))
}

func TestProcessComplementaryBranchesWithIncomingJump(t *testing.T) {
	rom := []byte{0xD0, 0x02, 0xF0, 0x00}
	arch, mockDis := createMockArch(t, rom)

	// Mark the second instruction as a branch destination
	mockDis.branchDestinations = set.New[uint16]()
	mockDis.branchDestinations.Add(0x8002)

	// Record a complementary branch pair
	arch.complementaryBranchPairs = []ComplementaryBranchPair{
		{
			FirstAddress:  0x8000,
			SecondAddress: 0x8002,
			FirstBranch:   "bne",
			SecondBranch:  "beq",
		},
	}

	// Create mock mapper
	mockMapper := &mockMapper{
		offsets: make(map[uint16]*offset.DisasmOffset),
	}
	mockMapper.offsets[0x8000] = &offset.DisasmOffset{}
	mockMapper.offsets[0x8002] = &offset.DisasmOffset{}
	arch.mapper = mockMapper

	// Process with incoming jump - second should NOT be marked unreachable
	arch.ProcessComplementaryBranches()

	assert.Contains(t, mockMapper.offsets[0x8000].Comment, "unconditional branch pattern")
	assert.Contains(t, mockMapper.offsets[0x8002].Comment, "reachable from other code")
	assert.False(t, mockDis.unreachableAddresses.Contains(0x8002))
}

func testBranchSequence(t *testing.T, instruction, nextInstruction byte) (bool, *offset.DisasmOffset) {
	t.Helper()
	rom := []byte{instruction, 0x02, nextInstruction, 0x00}
	arch, _ := createMockArch(t, rom)

	offsetInfo := &offset.DisasmOffset{}
	offsetInfo.Data = []byte{instruction, 0x02}
	offsetInfo.Opcode = &Opcode{op: cpu6502.Opcodes[instruction]}

	detected := arch.DetectComplementaryBranchSequence(0x8000, offsetInfo)
	return detected, offsetInfo
}

// mockDisasm is a minimal mock implementation for testing
type mockDisasm struct {
	rom                  []byte
	pc                   uint16
	branchDestinations   set.Set[uint16]
	unreachableAddresses set.Set[uint16]
}

func (m *mockDisasm) ReadMemory(address uint16) (byte, error) {
	offset := int(address - 0x8000)
	if offset < 0 || offset >= len(m.rom) {
		return 0, errors.New("address out of range")
	}
	return m.rom[offset], nil
}

func (m *mockDisasm) AddAddressToParse(address, context, from uint16, currentInstruction instruction.Instruction, isABranchDestination bool) {
}

func (m *mockDisasm) Cart() *cartridge.Cartridge {
	return nil
}

func (m *mockDisasm) ChangeAddressRangeToCodeAsData(address uint16, data []byte) {
}

func (m *mockDisasm) Options() options.Disassembler {
	return options.Disassembler{}
}

func (m *mockDisasm) ReadMemoryWord(address uint16) (uint16, error) {
	low, err := m.ReadMemory(address)
	if err != nil {
		return 0, err
	}
	high, err := m.ReadMemory(address + 1)
	if err != nil {
		return 0, err
	}
	return (uint16(high) << 8) | uint16(low), nil
}

func (m *mockDisasm) SetCodeBaseAddress(address uint16) {
}

func (m *mockDisasm) SetHandlers(handlers program.Handlers) {
}

func (m *mockDisasm) SetVectorsStartAddress(address uint16) {
}

func (m *mockDisasm) IsBranchDestination(address uint16) bool {
	if m.branchDestinations == nil {
		return false
	}
	return m.branchDestinations.Contains(address)
}

func (m *mockDisasm) MarkAddressAsUnreachable(address uint16) {
	if m.unreachableAddresses == nil {
		m.unreachableAddresses = set.New[uint16]()
	}
	m.unreachableAddresses.Add(address)
}

func (m *mockDisasm) ProgramCounter() uint16 {
	return m.pc
}

// mockMapper is a simple mock implementation for testing.
type mockMapper struct {
	offsets map[uint16]*offset.DisasmOffset
}

func (m *mockMapper) OffsetInfo(address uint16) *offset.DisasmOffset {
	return m.offsets[address]
}

func (m *mockMapper) MappedBank(_ uint16) offset.MappedBank {
	return nil
}

func (m *mockMapper) MappedBankIndex(_ uint16) uint16 {
	return 0
}

func (m *mockMapper) ReadMemory(_ uint16) byte {
	return 0
}

func createMockArch(t *testing.T, rom []byte) (*Arch6502, *mockDisasm) {
	t.Helper()

	mockDis := &mockDisasm{
		rom: rom,
	}

	arch := &Arch6502{
		dis:    mockDis,
		logger: log.NewTestLogger(t),
	}

	return arch, mockDis
}
