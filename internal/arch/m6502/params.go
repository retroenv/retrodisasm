package m6502

import (
	"fmt"

	"github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes"
)

type paramReaderFunc func(dis disasm, address uint16) (any, []byte, error)

var paramReader = map[m6502.AddressingMode]paramReaderFunc{
	m6502.ImpliedAddressing:     paramReaderImplied,
	m6502.ImmediateAddressing:   paramReaderImmediate,
	m6502.AccumulatorAddressing: paramReaderAccumulator,
	m6502.AbsoluteAddressing:    paramReaderAbsolute,
	m6502.AbsoluteXAddressing:   paramReaderAbsoluteX,
	m6502.AbsoluteYAddressing:   paramReaderAbsoluteY,
	m6502.ZeroPageAddressing:    paramReaderZeroPage,
	m6502.ZeroPageXAddressing:   paramReaderZeroPageX,
	m6502.ZeroPageYAddressing:   paramReaderZeroPageY,
	m6502.RelativeAddressing:    paramReaderRelative,
	m6502.IndirectAddressing:    paramReaderIndirect,
	m6502.IndirectXAddressing:   paramReaderIndirectX,
	m6502.IndirectYAddressing:   paramReaderIndirectY,
}

// ReadOpParam reads the opcode parameters after the first opcode byte
// and translates it into system specific types.
func (ar *Arch6502) ReadOpParam(addressing int, address uint16) (any, []byte, error) {
	fun, ok := paramReader[m6502.AddressingMode(addressing)]
	if !ok {
		return nil, nil, fmt.Errorf("unsupported addressing mode %00x", addressing)
	}
	return fun(ar.dis, address)
}

// ReadMemory reads a byte from memory using NES-specific memory mapping.
// In binary mode, all reads go through the mapper directly.
func (ar *Arch6502) ReadMemory(address uint16) (byte, error) {
	var value byte

	// In binary mode, always use mapper (no NES-specific memory mapping)
	if ar.options.Binary {
		value = ar.mapper.ReadMemory(address)
		return value, nil
	}

	switch {
	case address < 0x2000:
		value = ar.dis.Cart().CHR[address]

	case address >= nes.CodeBaseAddress:
		value = ar.mapper.ReadMemory(address)

	default:
		return 0, fmt.Errorf("invalid read from address #%04x", address)
	}
	return value, nil
}

func paramReaderImplied(disasm, uint16) (any, []byte, error) {
	return nil, nil, nil
}

func paramReaderImmediate(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return int(b), opcodes, nil
}

func paramReaderAccumulator(disasm, uint16) (any, []byte, error) {
	return m6502.Accumulator(0), nil, nil
}

func paramReaderAbsolute(dis disasm, address uint16) (any, []byte, error) {
	w, opcodes, err := paramReadWord(dis, address)
	if err != nil {
		return nil, nil, err
	}

	return m6502.Absolute(w), opcodes, nil
}

func paramReaderAbsoluteX(dis disasm, address uint16) (any, []byte, error) {
	w, opcodes, err := paramReadWord(dis, address)
	if err != nil {
		return nil, nil, err
	}
	return m6502.AbsoluteX(w), opcodes, nil
}

func paramReaderAbsoluteY(dis disasm, address uint16) (any, []byte, error) {
	w, opcodes, err := paramReadWord(dis, address)
	if err != nil {
		return nil, nil, err
	}
	return m6502.AbsoluteY(w), opcodes, nil
}

func paramReaderZeroPage(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return m6502.ZeroPage(b), opcodes, nil
}

func paramReaderZeroPageX(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return m6502.ZeroPageX(b), opcodes, nil
}

func paramReaderZeroPageY(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return m6502.ZeroPageY(b), opcodes, nil
}

func paramReaderRelative(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}

	offset := uint16(b)
	if offset < 0x80 {
		address += 2 + offset
	} else {
		address += 2 + offset - 0x100
	}

	opcodes := []byte{byte(offset)}
	return m6502.Absolute(address), opcodes, nil
}

func paramReaderIndirect(dis disasm, address uint16) (any, []byte, error) {
	// do not read actual address in disassembler mode
	w, opcodes, err := paramReadWord(dis, address)
	if err != nil {
		return nil, nil, err
	}
	return m6502.Indirect(w), opcodes, nil
}

func paramReaderIndirectX(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return m6502.IndirectX(b), opcodes, nil
}

func paramReaderIndirectY(dis disasm, address uint16) (any, []byte, error) {
	b, err := dis.ReadMemory(address + 1)
	if err != nil {
		return nil, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	opcodes := []byte{b}
	return m6502.IndirectY(b), opcodes, nil
}

func paramReadWord(dis disasm, address uint16) (uint16, []byte, error) {
	b1, err := dis.ReadMemory(address + 1)
	if err != nil {
		return 0, nil, fmt.Errorf("reading memory at address %04x: %w", address+1, err)
	}
	b2, err := dis.ReadMemory(address + 2)
	if err != nil {
		return 0, nil, fmt.Errorf("reading memory at address %04x: %w", address+2, err)
	}
	w := uint16(b2)<<uint16(8) | uint16(b1)
	opcodes := []byte{b1, b2}
	return w, opcodes, nil
}
