package m6502

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/assert"
)

func TestIsValidVectorAddress(t *testing.T) {
	ar := &Arch6502{}

	tests := []struct {
		name     string
		addr     uint16
		expected bool
	}{
		{"zero address", 0x0000, false},
		{"all ones", 0xFFFF, false},
		{"zero page", 0x00FF, false},
		{"stack area", 0x01FF, false},
		{"RAM area", 0x07FF, false},
		{"just below code base", 0x7FFF, false},
		{"valid code base", 0x8000, true},
		{"valid mid code", 0xC000, true},
		{"valid high code", 0xF000, true},
		{"just before vectors", 0xFFF9, true},
		{"vector table start", 0xFFFA, false},
		{"vector table mid", 0xFFFC, false},
		{"vector table end", 0xFFFE, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ar.isValidVectorAddress(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockMapperForOpcode is a minimal mock for testing isValidOpcodeAt.
type mockMapperForOpcode struct {
	memory map[uint16]byte
}

func (m *mockMapperForOpcode) ReadMemory(addr uint16) byte {
	return m.memory[addr]
}

func (m *mockMapperForOpcode) BankCount() int                           { return 1 }
func (m *mockMapperForOpcode) BankVectors(_ int) [3]uint16              { return [3]uint16{} }
func (m *mockMapperForOpcode) IsAddressFixed(_ uint16) bool             { return false }
func (m *mockMapperForOpcode) MapBank(_ int)                            {}
func (m *mockMapperForOpcode) MappedBank(_ uint16) offset.MappedBank    { return nil }
func (m *mockMapperForOpcode) MappedBankIndex(_ uint16) uint16          { return 0 }
func (m *mockMapperForOpcode) OffsetInfo(_ uint16) *offset.DisasmOffset { return nil }
func (m *mockMapperForOpcode) RestoreDefaultMapping()                   {}

func TestIsValidOpcodeAt(t *testing.T) {
	mock := &mockMapperForOpcode{
		memory: map[uint16]byte{
			0x8000: 0xA9, // LDA immediate - valid opcode
			0x8001: 0x00, // BRK - valid opcode
			0x8002: 0x02, // Invalid/unofficial opcode (JAM)
			0x8003: 0x4C, // JMP absolute - valid opcode
			0x8004: 0xEA, // NOP - valid opcode
		},
	}

	ar := &Arch6502{mapper: mock}

	tests := []struct {
		name     string
		addr     uint16
		expected bool
	}{
		{"LDA immediate", 0x8000, true},
		{"BRK", 0x8001, true},
		{"JAM (invalid)", 0x8002, false},
		{"JMP absolute", 0x8003, true},
		{"NOP", 0x8004, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ar.isValidOpcodeAt(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
