// Package offset provides types for representing program offsets and memory banks.
package offset

import (
	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/program"
)

// DisasmOffset defines the content of an offset in a program that can represent data or code.
// It extends program.Offset with disassembler-specific fields.
type DisasmOffset struct {
	program.Offset

	Opcode instruction.Opcode // opcode this offset represents

	BranchFrom  []BankReference // list of all addresses that branch to this offset
	BranchingTo string          // label to jump to if instruction branches
	Context     uint16          // function or interrupt context that the offset is part of
}

// MappedBank represents a mapped memory bank.
type MappedBank interface {
	ID() int
	OffsetInfo(address uint16) *DisasmOffset
}

// BankReference represents a reference to an address in a bank.
type BankReference struct {
	Mapped  MappedBank // bank reference
	Address uint16     // address in the bank
	ID      int        // bank ID
	Index   uint16     // index in the bank
}

// Mapper provides a mapper manager interface for architecture code.
type Mapper interface {
	// BankCount returns the number of PRG banks.
	BankCount() int
	// BankVectors reads the three interrupt vectors from the specified bank's raw PRG data.
	BankVectors(bankIndex int) [3]uint16
	// IsAddressFixed returns true if the address is in a fixed (non-switchable) region.
	IsAddressFixed(addr uint16) bool
	// MapBank maps the switchable windows of the specified bank to the address space.
	MapBank(bankIndex int)
	// MappedBank returns the mapped bank for the given address.
	MappedBank(address uint16) MappedBank
	// MappedBankIndex returns the bank index for the given address.
	MappedBankIndex(address uint16) uint16
	// OffsetInfo returns the offset information for the given address.
	OffsetInfo(address uint16) *DisasmOffset
	// ReadMemory reads a byte from memory at the given address.
	ReadMemory(address uint16) byte
	// RestoreDefaultMapping restores the default bank mapping.
	RestoreDefaultMapping()
}
