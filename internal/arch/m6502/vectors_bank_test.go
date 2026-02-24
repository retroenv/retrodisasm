package m6502

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/instruction"
	"github.com/retroenv/retrodisasm/internal/offset"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	cpu6502 "github.com/retroenv/retrogolib/arch/cpu/m6502"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestInitializeBankVectors_QueuesValidVectorsIncludingSameAddressAsLastBank(t *testing.T) {
	mapper := &bankVectorMapperMock{
		offsets: map[uint16]*offset.DisasmOffset{
			0x9100: {},
			0x9010: {},
		},
		memory: map[uint16]byte{
			0x9100: 0xEA, // nop
			0x9010: 0xEA, // nop
		},
		vectorsByBank: [][3]uint16{
			{0x9100, 0x9010, 0xFFFF}, // bank 0: NMI + Reset are valid
			{0x9000, 0x9010, 0x9020}, // last bank
		},
	}

	dis := &bankVectorDisasmMock{}
	arch := &Arch6502{
		dis:    dis,
		mapper: mapper,
		logger: log.NewTestLogger(t),
	}

	err := arch.InitializeBankVectors(0)
	assert.NoError(t, err)

	assert.Len(t, dis.queued, 2)
	assert.Equal(t, uint16(0x9100), dis.queued[0])
	assert.Equal(t, uint16(0x9010), dis.queued[1])

	offsetInfo := mapper.OffsetInfo(0x9100)
	assert.Equal(t, "NMI_Bank0", offsetInfo.Label)
	assert.True(t, offsetInfo.IsType(program.CallDestination))

	offsetInfo = mapper.OffsetInfo(0x9010)
	assert.Equal(t, "Reset_Bank0", offsetInfo.Label)
	assert.True(t, offsetInfo.IsType(program.CallDestination))
}

func TestInitializeBankVectors_SkipsInvalidOpcode(t *testing.T) {
	mapper := &bankVectorMapperMock{
		offsets: map[uint16]*offset.DisasmOffset{
			0x9200: {},
		},
		memory: map[uint16]byte{
			0x9200: 0x02, // invalid opcode
		},
		vectorsByBank: [][3]uint16{
			{0x9200, 0x0000, 0xFFFF}, // bank 0: only invalid opcode candidate
			{0x9000, 0x9010, 0x9020}, // last bank
		},
	}

	dis := &bankVectorDisasmMock{}
	arch := &Arch6502{
		dis:    dis,
		mapper: mapper,
		logger: log.NewTestLogger(t),
	}

	err := arch.InitializeBankVectors(0)
	assert.NoError(t, err)
	assert.Len(t, dis.queued, 0)
	assert.Equal(t, "", mapper.OffsetInfo(0x9200).Label)
}

type bankVectorDisasmMock struct {
	queued []uint16
}

func (m *bankVectorDisasmMock) AddAddressToParse(address, _, _ uint16, _ instruction.Instruction, _ bool) {
	m.queued = append(m.queued, address)
}

func (m *bankVectorDisasmMock) Cart() *cartridge.Cartridge {
	return nil
}

func (m *bankVectorDisasmMock) ChangeAddressRangeToCodeAsData(_ uint16, _ []byte) {
}

func (m *bankVectorDisasmMock) IsBranchDestination(_ uint16) bool {
	return false
}

func (m *bankVectorDisasmMock) MarkAddressAsUnreachable(_ uint16) {
}

func (m *bankVectorDisasmMock) Options() options.Disassembler {
	return options.Disassembler{}
}

func (m *bankVectorDisasmMock) ProgramCounter() uint16 {
	return 0
}

func (m *bankVectorDisasmMock) ReadMemory(_ uint16) (byte, error) {
	return 0, nil
}

func (m *bankVectorDisasmMock) ReadMemoryWord(_ uint16) (uint16, error) {
	return 0, nil
}

func (m *bankVectorDisasmMock) SetCodeBaseAddress(_ uint16) {
}

func (m *bankVectorDisasmMock) SetHandlers(_ program.Handlers) {
}

func (m *bankVectorDisasmMock) SetVectorsStartAddress(_ uint16) {
}

type bankVectorMapperMock struct {
	offsets       map[uint16]*offset.DisasmOffset
	memory        map[uint16]byte
	vectorsByBank [][3]uint16
}

func (m *bankVectorMapperMock) OffsetInfo(address uint16) *offset.DisasmOffset {
	return m.offsets[address]
}

func (m *bankVectorMapperMock) MappedBank(_ uint16) offset.MappedBank {
	return nil
}

func (m *bankVectorMapperMock) MappedBankIndex(_ uint16) uint16 {
	return 0
}

func (m *bankVectorMapperMock) ReadMemory(address uint16) byte {
	return m.memory[address]
}

func (m *bankVectorMapperMock) BankCount() int {
	return len(m.vectorsByBank)
}

func (m *bankVectorMapperMock) BankVectors(bankIndex int) [3]uint16 {
	if bankIndex < 0 || bankIndex >= len(m.vectorsByBank) {
		return [3]uint16{}
	}
	return m.vectorsByBank[bankIndex]
}

func TestIsValidVectorAddress(t *testing.T) {
	assert.False(t, isValidVectorAddress(0x0000))
	assert.False(t, isValidVectorAddress(0xFFFF))
	assert.False(t, isValidVectorAddress(0x7FFF))
	assert.False(t, isValidVectorAddress(cpu6502.InterruptVectorStartAddress))
	assert.True(t, isValidVectorAddress(0x8000))
	assert.True(t, isValidVectorAddress(0xFF00))
}
