package m6502

import (
	"encoding/binary"
	"fmt"

	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrogolib/arch/cpu/cpu6502"
	"github.com/retroenv/retrogolib/log"
)

const (
	jumpEngineMaxContextSize = 0x25
)

// checkForJumpEngineJmp checks if the current instruction is the jump instruction inside a jump engine function.
// The function offsets after the call to the jump engine will be used as destinations to disassemble as code.
// This can be found in some official games like Super Mario Bros.
func (ar *Arch6502) checkForJumpEngineJmp(jumpAddress uint16, offsetInfo *offset.DisasmOffset) error {
	instruction := offsetInfo.Opcode.Instruction()
	addressing := cpu6502.AddressingMode(offsetInfo.Opcode.Addressing())
	if instruction.Name() != cpu6502.JmpName || addressing != cpu6502.IndirectAddressing {
		return nil
	}

	contextOffsets, contextAddresses := ar.jumpEngine.JumpContextInfo(jumpAddress, offsetInfo)
	contextSize := jumpAddress - offsetInfo.Context + 3
	dataReferences, err := ar.jumpEngine.GetContextDataReferences(contextOffsets, contextAddresses, ar.codeBaseAddress)
	if err != nil {
		return fmt.Errorf("getting context data references: %w", err)
	}

	if len(dataReferences) > 1 {
		ar.jumpEngine.GetFunctionTableReference(offsetInfo.Context, dataReferences)
	}

	ar.logger.Debug("Jump engine detected",
		log.Hex("address", jumpAddress),
		log.Uint16("code_size", contextSize),
	)

	// if code reaches this point, no branching instructions beside the final indirect jmp have been found
	// in the function, this makes it likely a jump engine
	ar.jumpEngine.AddJumpEngine(offsetInfo.Context)

	if contextSize < jumpEngineMaxContextSize {
		if err := ar.jumpEngine.HandleJumpEngineCallers(offsetInfo.Context, ar.codeBaseAddress); err != nil {
			return fmt.Errorf("handling jump engine callers: %w", err)
		}
		return nil
	}
	offsetInfo.Comment = "jump engine detected"
	return nil
}

// checkForJumpEngineCall checks if the current instruction is a call into a jump engine function.
func (ar *Arch6502) checkForJumpEngineCall(address uint16, offsetInfo *offset.DisasmOffset) error {
	instruction := offsetInfo.Opcode.Instruction()
	addressing := cpu6502.AddressingMode(offsetInfo.Opcode.Addressing())
	if instruction.Name() != cpu6502.JsrName || addressing != cpu6502.AbsoluteAddressing {
		return nil
	}

	pc := ar.dis.ProgramCounter()
	_, opcodes, err := ar.ReadOpParam(offsetInfo.Opcode.Addressing(), pc)
	if err != nil {
		return err
	}

	destination := binary.LittleEndian.Uint16(opcodes)
	if err := ar.jumpEngine.HandleJumpEngineDestination(address, destination, ar.codeBaseAddress); err != nil {
		return fmt.Errorf("handling jump engine destination: %w", err)
	}
	return nil
}
