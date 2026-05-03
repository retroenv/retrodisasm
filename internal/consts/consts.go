// Package consts manages constants in the disassembled program.
package consts

import (
	"fmt"
	"strings"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/symbols"
)

// Constant represents a constant translation from a read and write operation to a name.
// This is used to replace the parameter of an instruction by a constant name.
type Constant struct {
	Address uint16

	Read  string
	Write string
}

// Consts manages constants in the disassembled program.
type Consts struct {
	*symbols.Manager[Constant]
}

// New creates a new constants manager.
func New(ar architecture) (*Consts, error) {
	constants, err := ar.Constants()
	if err != nil {
		return nil, fmt.Errorf("getting constants: %w", err)
	}

	mgr := symbols.New[Constant]()
	// Initialize the manager with the architecture's constants
	for address, constant := range constants {
		mgr.Set(address, constant)
	}

	return &Consts{
		Manager: mgr,
	}, nil
}

// ReplaceParameter replaces the parameter of an instruction by a constant name
// if the address of the instruction is found in the constants map.
func (c *Consts) ReplaceParameter(address uint16, opcode instruction.Opcode, paramAsString string) (string, bool) {
	constantInfo, ok := c.Get(address)
	if !ok {
		return "", false
	}

	// split parameter string in case of x/y indexing, only the first part will be replaced by a const name
	paramParts := strings.Split(paramAsString, ",")

	if constantInfo.Read != "" && opcode.ReadsMemory() {
		c.MarkUsed(address)
		paramParts[0] = constantInfo.Read
		return strings.Join(paramParts, ","), true
	}
	if constantInfo.Write != "" && opcode.WritesMemory() {
		c.MarkUsed(address)
		paramParts[0] = constantInfo.Write
		return strings.Join(paramParts, ","), true
	}

	return paramAsString, true
}

// Process processes all constants and updates all banks with the used ones. There is currently no tracking
// for in which bank a constant is used, it will be added to all banks for now.
// TODO fix constants to only output in used banks
func (c *Consts) Process() {
	constants := c.SortedByUint16(func(c Constant) uint16 { return c.Address })

	for _, constInfo := range constants {
		if !c.IsUsed(constInfo.Address) {
			continue
		}

		for _, bnk := range c.Banks() {
			c.AddBankItem(bnk, constInfo.Address, constInfo)
		}
	}
}

// AddBankItem is a helper to add a constant to a bank.
func (c *Consts) AddBankItem(bnk *symbols.Bank[Constant], address uint16, constInfo Constant) {
	bnk.Set(address, constInfo)
	bnk.Used().Add(address)
}

// SetToProgram sets the used constants in the program for outputting.
func (c *Consts) SetToProgram(app *program.Program) {
	for address := range c.Used() {
		constantInfo, _ := c.Get(address)
		if constantInfo.Read != "" {
			app.Constants[constantInfo.Read] = address
		}
		if constantInfo.Write != "" {
			app.Constants[constantInfo.Write] = address
		}
	}
}

// AssignBankConstants assigns the used constants to the bank for outputting.
func (c *Consts) AssignBankConstants(bankID int, prgBank *program.PRGBank) {
	bank := c.Bank(bankID)
	for address := range bank.Used() {
		constantInfo, _ := bank.Get(address)
		if constantInfo.Read != "" {
			prgBank.Constants[constantInfo.Read] = address
		}
		if constantInfo.Write != "" {
			prgBank.Constants[constantInfo.Write] = address
		}
	}
}

type architecture interface {
	Constants() (map[uint16]Constant, error)
}
