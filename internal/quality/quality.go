// Package quality measures heuristic signals of a disassembled NES program.
// Heuristic signals do not prove an error.
package quality

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/retroenv/retrodisasm/internal/mapper"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/arch/system/nes"
	"github.com/retroenv/retrogolib/arch/system/nes/register"
)

// Reason tells why an instruction looks like data.
type Reason string

// Reasons for suspicious instructions.
const (
	// Brk is a brk instruction. Few programs use it.
	Brk Reason = "brk"
	// Unofficial is an undocumented opcode.
	Unofficial Reason = "unofficial"
	// Decimal is a sed instruction. The NES CPU has no decimal mode.
	Decimal Reason = "decimal"
	// UnusedAddress is an access to the unused CPU test and expansion area.
	UnusedAddress Reason = "unused-address"
	// WriteOnlyRead is a read of a register that only accepts writes.
	WriteOnlyRead Reason = "write-only-read"
	// ReadOnlyWrite is a write to the PPU status register.
	ReadOnlyWrite Reason = "read-only-write"
	// ROMWrite is a write to ROM on a cartridge without mapper registers.
	ROMWrite Reason = "rom-write"
	// BadTarget is a branch or jump in the bank to an address that is not
	// the start of an instruction.
	BadTarget Reason = "bad-target"
	// CodeAsData is an official instruction that the output writes as data
	// bytes, for example because a branch goes into its operand. A bit
	// instruction is not part of this reason, because programs use its
	// operand to skip an instruction.
	CodeAsData Reason = "code-as-data"
)

const (
	// minMissedCodeRun is the smallest data run that the report checks for
	// missed code.
	minMissedCodeRun = 8
	// minMissedCodeInstructions is the smallest number of instructions that
	// a data run must decode to before it counts as possible missed code.
	minMissedCodeInstructions = 4
	// maxExamples is the number of suspicious locations in the report.
	maxExamples = 20
)

// tracedCode matches the output records of traced code. The output writes
// these records as instructions or as raw bytes.
const tracedCode = program.CodeOffset | program.CodeAsData

// NES CPU memory map values that the access checks use.
const (
	ppuRegisterEnd    = 0x4000 // the PPU registers repeat up to this address
	ppuRegisterMask   = 7      // the PPU registers repeat every 8 bytes
	cpuTestStart      = 0x4018 // the unused CPU test area starts here
	expansionStart    = 0x4020 // the cartridge expansion area starts here
	cartridgeRAMStart = 0x6000 // the cartridge RAM area starts here
)

// Report contains the quality signals at physical PRG ROM locations.
// Aliases do not add duplicate bytes, instructions, or warnings.
type Report struct {
	// CodeBytes and Instructions include the traced instructions that the
	// output writes as data bytes.
	CodeBytes    int
	DataBytes    int
	Instructions int

	// Reasons counts each reason. Suspicious is the number of instructions
	// that have one or more reasons.
	Reasons    map[Reason]int
	Suspicious int

	// Examples are the first suspicious instructions, ordered by bank and
	// address. The list is bounded.
	Examples []Example

	// MissedCodeBytes is the size of the data runs that decode to plausible
	// code: valid instructions that access hardware registers or call code,
	// and that end with a return or jump.
	MissedCodeBytes int
	MissedCodeRuns  int
	MissedExamples  []MissedExample
}

// Example is a suspicious instruction.
type Example struct {
	Address   uint16
	Bank      int
	ROMOffset int

	Reasons []Reason
}

// MissedExample identifies a data run that can contain code.
type MissedExample struct {
	Address   uint16
	Bank      int
	ROMOffset int

	Bytes        int
	Instructions int
}

// SuspiciousPercent returns the percentage of suspicious instructions.
func (r Report) SuspiciousPercent() float64 {
	if r.Instructions == 0 {
		return 0
	}
	return 100 * float64(r.Suspicious) / float64(r.Instructions)
}

// SortedReasons returns the reasons ordered by count, the most frequent first.
func (r Report) SortedReasons() []Reason {
	return slices.SortedFunc(maps.Keys(r.Reasons), func(a, b Reason) int {
		return cmp.Or(cmp.Compare(r.Reasons[b], r.Reasons[a]), cmp.Compare(a, b))
	})
}

// Write writes the report as text. The first line has all counts.
func (r Report) Write(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "quality: code %d bytes, data %d bytes, instructions %d, suspicious %d (%.2f%%), "+
		"possible missed code %d bytes in %d runs\n",
		r.CodeBytes, r.DataBytes, r.Instructions, r.Suspicious, r.SuspiciousPercent(),
		r.MissedCodeBytes, r.MissedCodeRuns)
	for _, reason := range r.SortedReasons() {
		fmt.Fprintf(&b, "quality:   %-16s %d\n", reason, r.Reasons[reason])
	}

	for _, example := range r.Examples {
		reasons := make([]string, len(example.Reasons))
		for i, reason := range example.Reasons {
			reasons[i] = string(reason)
		}

		fmt.Fprintf(&b, "quality:   bank %d $%04X ROM $%06X: %s\n",
			example.Bank, example.Address, example.ROMOffset, strings.Join(reasons, ", "))
	}

	for _, example := range r.MissedExamples {
		fmt.Fprintf(&b, "quality:   possible missed code bank %d $%04X ROM $%06X: %d bytes, %d instructions\n",
			example.Bank, example.Address, example.ROMOffset, example.Bytes, example.Instructions)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

// Analyze returns the quality signals of a NES program.
func Analyze(app *program.Program) Report {
	analysis := inspectPhysical(app)
	report := analysis.report()
	missed := newMissedCode(len(analysis.bytes))
	for _, bank := range app.PRG {
		findMissedCode(&missed, bank, app.Mapper, analysis.bytes)
	}

	missed.report(&report)

	return report
}

// instruction is a decoded instruction of a bank.
type instruction struct {
	address    uint16
	codeAsData bool // the output writes the instruction as data bytes
	data       []byte
	opcode     cpu6502.Opcode
}

type codeCandidate struct {
	hardware     bool
	instructions int
	size         int
}

// firstExamples returns the first values of a list, up to maxExamples.
func firstExamples[T any](values []T) []T {
	return values[:min(len(values), maxExamples)]
}

// instructionReasons returns the reasons that the instruction looks like
// data. It does not check the transfer target.
func instructionReasons(ins instruction, mapperID uint16) []Reason {
	var reasons []Reason
	name := ins.opcode.Instruction.Name
	switch {
	case ins.opcode.Instruction.Unofficial || name == cpu6502.KilName:
		// The opcode data of KIL does not have the unofficial flag.
		reasons = append(reasons, Unofficial)
	case name == cpu6502.BrkName:
		reasons = append(reasons, Brk)
	case name == cpu6502.SedName:
		reasons = append(reasons, Decimal)
	case ins.codeAsData && name != cpu6502.BitName:
		reasons = append(reasons, CodeAsData)
	}

	if ins.opcode.Addressing == cpu6502.IndirectAddressing {
		return append(reasons, indirectAccessReasons(ins, mapperID)...)
	}

	address, ok := dataAddress(ins)
	if !ok {
		return reasons
	}
	if reason, ok := instructionAccessReason(ins, address, mapperID); ok {
		reasons = append(reasons, reason)
	}

	return reasons
}

// indirectAccessReasons checks both pointer reads of an indirect jump. The
// NES CPU reads the high byte from the same page when the low byte is at $FF.
// Pointer storage can be in RAM, ROM, or hardware registers.
func indirectAccessReasons(ins instruction, mapperID uint16) []Reason {
	if len(ins.data) < 3 {
		return nil
	}
	pointer := operandWord(ins.data)
	high := pointer&0xff00 | (pointer+1)&0xff
	var reasons []Reason
	for _, address := range [2]uint16{pointer, high} {
		reason, found := accessReason(cpu6502.LdaName, address, mapperID)
		if found && !slices.Contains(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}

	return reasons
}

// instructionAccessReason checks all possible effective addresses. An index
// value is unknown, so a warning must apply to each value of that index.
func instructionAccessReason(ins instruction, address, mapperID uint16) (Reason, bool) {
	name := ins.opcode.Instruction.Name
	reason, found := accessReason(name, address, mapperID)
	if !found {
		return "", false
	}
	if ins.opcode.Addressing == cpu6502.AbsoluteXAddressing || ins.opcode.Addressing == cpu6502.AbsoluteYAddressing {
		for index := uint16(1); index < 256; index++ {
			indexedReason, ok := accessReason(name, address+index, mapperID)
			if !ok || indexedReason != reason {
				return "", false
			}
		}
	}

	return reason, true
}

// accessReason returns the reason that a data access of the instruction looks
// like data.
func accessReason(name string, address, mapperID uint16) (Reason, bool) {
	if address >= register.PPU_CTRL && address < ppuRegisterEnd {
		address = register.PPU_CTRL | address&ppuRegisterMask
	}

	switch {
	case unusedAddress(address, mapperID):
		return UnusedAddress, true
	case reads(name) && writeOnlyRegister(address):
		return WriteOnlyRead, true
	case writes(name) && address == register.PPU_STATUS:
		return ReadOnlyWrite, true
	case writes(name) && address >= nes.CodeBaseAddress && mapperID == 0:
		return ROMWrite, true
	}

	return "", false
}

// findMissedCode adds the data runs of the bank that decode to plausible code.
func findMissedCode(missed *missedCode, bank *program.PRGBank, mapperID uint16, physicalBytes []byte) {
	data, untraced := bankData(bank)
	for i := range untraced {
		if physicalBytes[bank.ROMOffset+i] == physicalCode {
			untraced[i] = false
		}
	}

	start := -1
	for i := 0; i <= len(bank.Offsets); i++ {
		isData := i < len(untraced) && untraced[i]
		if isData {
			if start < 0 {
				start = i
			}

			continue
		}
		if start >= 0 && i-start >= minMissedCodeRun {
			addMissedCode(missed, bank, data[start:i], start, mapperID)
		}

		start = -1
	}
}

func addMissedCode(missed *missedCode, bank *program.PRGBank, data []byte, offset int, mapperID uint16) {
	start := bank.BaseAddress + uint16(offset)
	candidates := codeCandidates(data, start, mapperID)
	for i := 0; len(data)-i >= minMissedCodeRun; {
		candidate := candidates[i]
		if candidate.size >= minMissedCodeRun && candidate.instructions >= minMissedCodeInstructions && candidate.hardware {
			encoded := data[i : i+candidate.size]
			if skip := inconsistentFlowPrefix(encoded, start+uint16(i)); skip != 0 {
				i += skip
				continue
			}
			missed.add(MissedExample{
				Address:   start + uint16(i),
				Bank:      bank.ID,
				ROMOffset: bank.ROMOffset + offset + i,

				Bytes:        candidate.size,
				Instructions: candidate.instructions,
			})
			i += candidate.size
		} else {
			i++
		}
	}
}

// bankData expands packed records and excludes traced code and its operands.
func bankData(bank *program.PRGBank) ([]byte, []bool) {
	data := make([]byte, len(bank.Offsets))
	untraced := make([]bool, len(bank.Offsets))
	for i, info := range bank.Offsets {
		end := min(len(bank.Offsets), i+len(info.Data))
		copy(data[i:end], info.Data)
		if !info.IsType(tracedCode) {
			for j := i; j < end; j++ {
				untraced[j] = true
			}
		}
	}

	for i, info := range bank.Offsets {
		if info.IsType(tracedCode) {
			for j := i; j < min(len(bank.Offsets), i+max(1, len(info.Data))); j++ {
				untraced[j] = false
			}
		}
	}

	return data, untraced
}

// codeCandidates decodes each start once. A valid instruction uses the cached
// suffix at its next address. An invalid suffix excludes the candidate.
func codeCandidates(data []byte, start, mapperID uint16) []codeCandidate {
	candidates := make([]codeCandidate, len(data))
	for i := len(data) - 1; i >= 0; i-- {
		opcode := cpu6502.Opcodes[data[i]]
		if opcode.Instruction == nil {
			continue
		}
		size := opcode.Size()
		if i+size > len(data) {
			continue
		}
		ins := instruction{
			address: start + uint16(i),
			data:    data[i : i+size],
			opcode:  opcode,
		}
		if len(instructionReasons(ins, mapperID)) > 0 {
			continue
		}
		candidate := codeCandidate{
			hardware:     opcode.Instruction.Name == cpu6502.JsrName,
			instructions: 1,
			size:         size,
		}
		if address, ok := dataAddress(ins); ok && address >= register.PPU_CTRL && address < cpuTestStart {
			candidate.hardware = true
		}

		if !endsFlow(opcode) {
			if i+size >= len(data) || candidates[i+size].size == 0 {
				continue
			}
			suffix := candidates[i+size]
			candidate.size += suffix.size
			candidate.instructions += suffix.instructions
			candidate.hardware = candidate.hardware || suffix.hardware
		}

		candidates[i] = candidate
	}

	return candidates
}

// dataAddress returns the absolute data address of the instruction. Zero page
// and indirect addresses are not part of the result.
func dataAddress(ins instruction) (uint16, bool) {
	switch ins.opcode.Addressing {
	case cpu6502.AbsoluteAddressing, cpu6502.AbsoluteXAddressing, cpu6502.AbsoluteYAddressing:
	default:
		return 0, false
	}

	name := ins.opcode.Instruction.Name
	if name == cpu6502.JmpName || name == cpu6502.JsrName || len(ins.data) < 3 {
		return 0, false
	}
	return operandWord(ins.data), true
}

// operandWord returns the little-endian 16-bit operand of a 3-byte
// instruction.
func operandWord(data []byte) uint16 {
	return uint16(data[1]) | uint16(data[2])<<8
}

// transferTarget returns the destination of a branch, jmp, or jsr.
func transferTarget(ins instruction) (uint16, bool) {
	switch {
	case ins.opcode.Addressing == cpu6502.RelativeAddressing && len(ins.data) == 2:
		return ins.address + 2 + uint16(int8(ins.data[1])), true
	case ins.opcode.Addressing == cpu6502.AbsoluteAddressing && len(ins.data) == 3 &&
		(ins.opcode.Instruction.Name == cpu6502.JmpName || ins.opcode.Instruction.Name == cpu6502.JsrName):
		return operandWord(ins.data), true
	}

	return 0, false
}

func unusedAddress(address, mapperID uint16) bool {
	switch {
	case address >= cpuTestStart && address < expansionStart:
		return true
	case address >= expansionStart && address < cartridgeRAMStart:
		// MMC5 and Namco 163 have registers in this area.
		return mapperID != 5 && mapperID != 19 && !mapper.HasExpansionRegister(mapperID, address)
	}

	return false
}

func writeOnlyRegister(address uint16) bool {
	switch address {
	case register.PPU_CTRL, register.PPU_MASK, register.OAM_ADDR, register.PPU_SCROLL, register.PPU_ADDR,
		register.OAM_DMA:
		return true
	}

	return address >= register.APU_PL1_VOL && address < register.OAM_DMA
}

func reads(name string) bool {
	return cpu6502.MemoryReadInstructions.Contains(name) || cpu6502.MemoryReadWriteInstructions.Contains(name)
}

func writes(name string) bool {
	return cpu6502.MemoryWriteInstructions.Contains(name) || cpu6502.MemoryReadWriteInstructions.Contains(name)
}

func endsFlow(opcode cpu6502.Opcode) bool {
	switch opcode.Instruction.Name {
	case cpu6502.RtsName, cpu6502.RtiName:
		return true
	case cpu6502.JmpName:
		return opcode.Addressing == cpu6502.AbsoluteAddressing || opcode.Addressing == cpu6502.IndirectAddressing
	}

	return false
}
