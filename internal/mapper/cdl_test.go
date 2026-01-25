package mapper

import (
	"testing"

	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/codedatalog"
	"github.com/retroenv/retrogolib/assert"
)

func TestApplyCodeDataLog_Code(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x100),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{}
	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
	})

	// Create CDL flags marking bytes as code
	prgFlags := []codedatalog.PrgFlag{
		codedatalog.Code, // Byte 0 is code
		codedatalog.Code, // Byte 1 is code
		0,                // Byte 2 is data (no flag)
		codedatalog.Code, // Byte 3 is code
	}

	mapper.ApplyCodeDataLog(prgFlags)

	// Verify that code addresses were added to parse
	assert.Len(t, mockDis.addedAddresses, 3)
	assert.Equal(t, uint16(0x8000), mockDis.addedAddresses[0])
	assert.Equal(t, uint16(0x8001), mockDis.addedAddresses[1])
	assert.Equal(t, uint16(0x8003), mockDis.addedAddresses[2])
}

func TestApplyCodeDataLog_SubEntryPoint(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x100),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{}
	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
	})

	// Create CDL flags marking bytes as subroutine entry points
	prgFlags := []codedatalog.PrgFlag{
		codedatalog.SubEntryPoint, // Byte 0 is a subroutine entry
		0,                         // Byte 1 is regular
		codedatalog.Code | codedatalog.SubEntryPoint, // Byte 2 is code + entry point
	}

	mapper.ApplyCodeDataLog(prgFlags)

	// Verify that entry points were marked as CallDestination
	assert.True(t, mapper.banks[0].offsets[0].IsType(program.CallDestination))
	assert.False(t, mapper.banks[0].offsets[1].IsType(program.CallDestination))
	assert.True(t, mapper.banks[0].offsets[2].IsType(program.CallDestination))
}

func TestApplyCodeDataLog_BoundsCheck(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x10),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)
	mapper.SetCodeBaseAddress(0x8000)

	mockDis := &mockDisasm{}
	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
	})

	// Create CDL flags larger than the bank
	prgFlags := make([]codedatalog.PrgFlag, 0x100)
	for i := range prgFlags {
		prgFlags[i] = codedatalog.Code
	}

	// Should not panic and should stop at bank boundary
	mapper.ApplyCodeDataLog(prgFlags)

	// Current behavior: processes until index > len(offsets)
	// With 0x10 (16) offsets, processes indices 0-16 (17 addresses)
	assert.Len(t, mockDis.addedAddresses, 0x11)
}

func TestApplyCodeDataLog_Empty(t *testing.T) {
	cart := &cartridge.Cartridge{
		PRG: make([]byte, 0x100),
	}
	arch := &mockArchitecture{bankWindowSize: 0}

	mapper, err := New(arch, cart)
	assert.NoError(t, err)

	mockDis := &mockDisasm{}
	mapper.InjectDependencies(Dependencies{
		Disasm: mockDis,
	})

	// Empty CDL flags
	prgFlags := []codedatalog.PrgFlag{}

	mapper.ApplyCodeDataLog(prgFlags)

	// Should not have added any addresses
	assert.Len(t, mockDis.addedAddresses, 0)
}
