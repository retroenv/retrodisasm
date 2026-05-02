// Package offset provides types for representing program offsets and memory banks.
package offset

import (
	"fmt"

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

// FormatBranchReference formats an instruction with a branch destination label.
func FormatBranchReference(offsetInfo *DisasmOffset, label string) string {
	if offsetInfo.Opcode == nil {
		return fmt.Sprintf("%s %s", offsetInfo.Code, label)
	}

	instr := offsetInfo.Opcode.Instruction()
	if instr == nil || instr.IsNil() {
		return fmt.Sprintf("%s %s", offsetInfo.Code, label)
	}

	return fmt.Sprintf("%s %s", instr.Name(), label)
}

// Mapper provides a mapper manager interface for architecture code.
type Mapper interface {
	MappedBank(address uint16) MappedBank
	MappedBankIndex(address uint16) uint16
	// OffsetInfo returns the offset information for the given address.
	OffsetInfo(address uint16) *DisasmOffset
	// ReadMemory reads a byte from memory at the given address.
	ReadMemory(address uint16) byte
}
