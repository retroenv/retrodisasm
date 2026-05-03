// Package vars manages variables in the disassembled program.
package vars

import (
	"fmt"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrodisasm/internal/symbols"
	"github.com/retroenv/retrogolib/arch/system/nes"
)

const (
	dataNaming            = "_data_%04x"
	dataNamingIndexed     = "_data_%04x_indexed"
	jumpTableNaming       = "_jump_table_%04x"
	variableNaming        = "_var_%04x"
	variableNamingIndexed = "_var_%04x_indexed"
)

// Dependencies contains the dependencies needed by Vars.
type Dependencies struct {
	Mapper mapper
}

// Vars manages variables in the disassembled program.
type Vars struct {
	*symbols.Manager[*variable]

	arch   architecture
	mapper mapper
}

// New creates a new variables manager.
func New(arch architecture) *Vars {
	return &Vars{
		Manager: symbols.New[*variable](),
		arch:    arch,
	}
}

// InjectDependencies sets the required dependencies for this vars manager.
func (v *Vars) InjectDependencies(deps Dependencies) {
	v.mapper = deps.Mapper
}

// AddReference adds a variable reference if the opcode is accessing
// the given address directly by reading or writing. In a special case like
// branching into a zeropage address the variable usage can be forced.
func (v *Vars) AddReference(addressReference,
	usageAddress uint16, opcode instruction.Opcode, forceVariableUsage bool) {

	var reads, writes bool
	if opcode.ReadWritesMemory() {
		reads = true
		writes = true
	} else {
		reads = opcode.ReadsMemory()
		writes = opcode.WritesMemory()
	}
	if !reads && !writes && !forceVariableUsage {
		return
	}

	varInfo, ok := v.Get(addressReference)
	if !ok {
		varInfo = &variable{
			address: addressReference,
		}
		v.Set(addressReference, varInfo)
	}

	bankRef := offset.BankReference{
		Mapped:  v.mapper.MappedBank(usageAddress),
		Address: usageAddress,
		Index:   v.mapper.MappedBankIndex(usageAddress),
	}
	bankRef.ID = bankRef.Mapped.ID()
	varInfo.usageAt = append(varInfo.usageAt, bankRef)

	if reads {
		varInfo.reads = true
	}
	if writes {
		varInfo.writes = true
	}

	if v.arch.IsAddressingIndexed(opcode) {
		varInfo.indexedUsage = true
	}
}

// Process processes all variables and updates the instructions that use them
// with a generated alias name.
func (v *Vars) Process(codeBaseAddress uint16) error {
	variables := v.SortedByUint16(func(v *variable) uint16 { return v.address })

	for _, varInfo := range variables {
		if len(varInfo.usageAt) == 1 && !varInfo.indexedUsage && varInfo.address < nes.CodeBaseAddress {
			if !varInfo.reads || !varInfo.writes {
				continue // ignore only once usages or ones that are not read and write
			}
		}

		var dataOffsetInfo *offset.DisasmOffset
		var addressAdjustment uint16
		if varInfo.address >= codeBaseAddress {
			// if the referenced address is inside the code, a label will be created for it
			dataOffsetInfo, varInfo.address, addressAdjustment = v.getOpcodeStart(varInfo.address)
		} else {
			// if the address is outside the code bank, a variable will be created
			v.MarkUsed(varInfo.address)

			for _, bankRef := range varInfo.usageAt {
				v.AddUsage(bankRef.ID, varInfo)
			}
		}

		var reference string
		varInfo.name, reference = v.dataName(dataOffsetInfo, varInfo.indexedUsage, varInfo.address, addressAdjustment)

		for _, bankRef := range varInfo.usageAt {
			offsetInfo := bankRef.Mapped.OffsetInfo(bankRef.Index)

			if err := v.arch.ProcessVariableUsage(offsetInfo, reference); err != nil {
				return fmt.Errorf("processing variable usage: %w", err)
			}
		}
	}
	return nil
}

// AddUsage adds a usage info of a variable to a bank.
func (v *Vars) AddUsage(bankIndex int, varInfo *variable) {
	bank := v.Bank(bankIndex)
	bank.Set(varInfo.address, varInfo)
	bank.Used().Add(varInfo.address)
}

// AssignBankVariables assigns the used variables to the bank for outputting.
func (v *Vars) AssignBankVariables(bankID int, prgBank *program.PRGBank) {
	bank := v.Bank(bankID)
	for address := range bank.Used() {
		varInfo, _ := bank.Get(address)
		prgBank.Variables[varInfo.name] = address
	}
}

// SetToProgram sets the used variables in the program for outputting.
func (v *Vars) SetToProgram(app *program.Program) {
	for address := range v.Used() {
		varInfo, _ := v.Get(address)
		app.Variables[varInfo.name] = address
	}
}

// getOpcodeStart returns a reference to the opcode start of the given address.
// In case it's in the first or second byte of an instruction, referencing the middle of an instruction will be
// converted to a reference to the beginning of the instruction and optional address adjustment like +1 or +2.
func (v *Vars) getOpcodeStart(address uint16) (*offset.DisasmOffset, uint16, uint16) {
	var addressAdjustment uint16

	for {
		offsetInfo := v.mapper.OffsetInfo(address)
		if offsetInfo == nil || len(offsetInfo.Data) == 0 {
			address--
			addressAdjustment++
			continue
		}
		return offsetInfo, address, addressAdjustment
	}
}

// dataName calculates the name of a variable based on its address and optional address adjustment.
// It returns the name of the variable and a string to reference it, it is possible that the reference
// is using an adjuster like +1 or +2.
func (v *Vars) dataName(offsetInfo *offset.DisasmOffset, indexedUsage bool, address, addressAdjustment uint16) (string, string) {
	name := v.generateVariableName(offsetInfo, indexedUsage, address)

	reference := name
	if addressAdjustment > 0 {
		reference = fmt.Sprintf("%s+%d", reference, addressAdjustment)
	}
	if offsetInfo != nil && offsetInfo.Label == "" {
		offsetInfo.Label = name
	}
	return name, reference
}

func (v *Vars) generateVariableName(offsetInfo *offset.DisasmOffset, indexedUsage bool, address uint16) string {
	if offsetInfo != nil && offsetInfo.Label != "" {
		// if destination has an existing label, reuse it
		return offsetInfo.Label
	}

	prgAccess := offsetInfo != nil
	var jumpTable bool
	if prgAccess {
		jumpTable = offsetInfo.IsType(program.JumpTable)
	}

	switch {
	case jumpTable:
		return fmt.Sprintf(jumpTableNaming, address)
	case prgAccess && indexedUsage:
		return fmt.Sprintf(dataNamingIndexed, address)
	case prgAccess && !indexedUsage:
		return fmt.Sprintf(dataNaming, address)
	case !prgAccess && indexedUsage:
		return fmt.Sprintf(variableNamingIndexed, address)
	default:
		return fmt.Sprintf(variableNaming, address)
	}
}

// architecture defines the minimal interface needed from arch.Architecture
type architecture interface {
	// IsAddressingIndexed returns if the opcode is using indexed addressing.
	IsAddressingIndexed(opcode instruction.Opcode) bool
	// ProcessVariableUsage processes the variable usage of an offset.
	ProcessVariableUsage(offsetInfo *offset.DisasmOffset, reference string) error
}

// mapper defines the minimal interface needed from the mapper
type mapper interface {
	// MappedBank returns the mapped bank for the given address.
	MappedBank(address uint16) offset.MappedBank
	// MappedBankIndex returns the mapped bank index for the given address.
	MappedBankIndex(address uint16) uint16
	// OffsetInfo returns the offset info for the given address.
	OffsetInfo(address uint16) *offset.DisasmOffset
}

type variable struct {
	reads  bool
	writes bool

	address      uint16
	name         string
	indexedUsage bool                   // access with X/Y registers indicates table
	usageAt      []offset.BankReference // list of all indexes that use this offset
}
