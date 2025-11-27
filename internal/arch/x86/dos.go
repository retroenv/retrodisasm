package x86

// dosInt21Functions maps DOS INT 21h function numbers to descriptions.
// Function number is passed in AH register before INT 21h call.
var dosInt21Functions = map[uint8]string{
	0x00: "Terminate Program",
	0x01: "Read Character with Echo",
	0x02: "Write Character",
	0x03: "Read Character from STDAUX",
	0x04: "Write Character to STDAUX",
	0x05: "Write Character to Printer",
	0x06: "Direct Console I/O",
	0x07: "Direct Console Input Without Echo",
	0x08: "Read Character Without Echo",
	0x09: "Print String",
	0x0A: "Buffered Input",
	0x0B: "Check Input Status",
	0x0C: "Flush Buffer, Read Keyboard",
	0x0D: "Disk Reset",
	0x0E: "Select Default Drive",
	0x0F: "Open File (FCB)",
	0x10: "Close File (FCB)",
	0x11: "Search First (FCB)",
	0x12: "Search Next (FCB)",
	0x13: "Delete File (FCB)",
	0x14: "Sequential Read (FCB)",
	0x15: "Sequential Write (FCB)",
	0x16: "Create File (FCB)",
	0x17: "Rename File (FCB)",
	0x19: "Get Default Drive",
	0x1A: "Set Disk Transfer Address",
	0x1B: "Get Allocation Info for Default Drive",
	0x1C: "Get Allocation Info for Drive",
	0x21: "Random Read (FCB)",
	0x22: "Random Write (FCB)",
	0x23: "Get File Size (FCB)",
	0x24: "Set Random Record Field (FCB)",
	0x25: "Set Interrupt Vector",
	0x26: "Create Program Segment Prefix",
	0x27: "Random Block Read (FCB)",
	0x28: "Random Block Write (FCB)",
	0x29: "Parse Filename",
	0x2A: "Get Date",
	0x2B: "Set Date",
	0x2C: "Get Time",
	0x2D: "Set Time",
	0x2E: "Set Verify Flag",
	0x2F: "Get Disk Transfer Address",
	0x30: "Get DOS Version",
	0x31: "Terminate and Stay Resident",
	0x33: "Get/Set Ctrl-Break Check",
	0x35: "Get Interrupt Vector",
	0x36: "Get Disk Free Space",
	0x38: "Get/Set Country Info",
	0x39: "Create Directory",
	0x3A: "Remove Directory",
	0x3B: "Change Current Directory",
	0x3C: "Create File",
	0x3D: "Open File",
	0x3E: "Close File Handle",
	0x3F: "Read File",
	0x40: "Write File",
	0x41: "Delete File",
	0x42: "Move File Pointer",
	0x43: "Get/Set File Attributes",
	0x44: "IOCTL",
	0x45: "Duplicate Handle",
	0x46: "Force Duplicate Handle",
	0x47: "Get Current Directory",
	0x48: "Allocate Memory",
	0x49: "Free Memory",
	0x4A: "Resize Memory Block",
	0x4B: "Execute Program",
	0x4C: "Terminate with Return Code",
	0x4D: "Get Return Code",
	0x4E: "Find First File",
	0x4F: "Find Next File",
	0x50: "Set PSP Segment",
	0x51: "Get PSP Segment",
	0x54: "Get Verify Flag",
	0x56: "Rename File",
	0x57: "Get/Set File Date and Time",
	0x58: "Get/Set Allocation Strategy",
	0x59: "Get Extended Error Info",
	0x5A: "Create Temporary File",
	0x5B: "Create New File",
	0x5C: "Lock/Unlock File Region",
	0x62: "Get PSP Address",
	0x63: "Get Lead Byte Table",
	0x65: "Get Extended Country Info",
	0x66: "Get/Set Global Code Page",
	0x67: "Set Handle Count",
	0x68: "Commit File",
}

// otherInterrupts maps other common DOS/BIOS interrupts to descriptions.
var otherInterrupts = map[uint8]string{
	0x10: "BIOS Video Services",
	0x13: "BIOS Disk Services",
	0x14: "BIOS Serial Port Services",
	0x15: "BIOS System Services",
	0x16: "BIOS Keyboard Services",
	0x17: "BIOS Printer Services",
	0x1A: "BIOS Time Services",
	0x20: "DOS Program Terminate",
	0x22: "DOS Terminate Handler",
	0x23: "DOS Ctrl-C Handler",
	0x24: "DOS Critical Error Handler",
	0x25: "DOS Absolute Disk Read",
	0x26: "DOS Absolute Disk Write",
	0x27: "DOS Terminate and Stay Resident",
	0x2F: "DOS Multiplex Interrupt",
	0x33: "Mouse Driver",
}

// getDOSInterruptComment generates a comment for a DOS/BIOS interrupt call.
// It looks back in the code to find the function number in AH for INT 21h.
func (ar *ArchX86) getDOSInterruptComment(intNum uint8) string {
	if intNum == 0x21 {
		// INT 21h - try to find the AH value to identify the function
		funcNum := ar.findAHValue()
		if funcNum != 0xFF {
			if desc, ok := dosInt21Functions[funcNum]; ok {
				return "DOS: " + desc
			}
			return "DOS: Function " + formatHex(funcNum)
		}
		return "DOS: System Call"
	}

	// Check other common interrupts
	if desc, ok := otherInterrupts[intNum]; ok {
		return desc
	}

	return ""
}

// findAHValue looks back through preceding instructions to find a MOV AH, imm8.
// Returns 0xFF if not found.
func (ar *ArchX86) findAHValue() uint8 {
	// Look at the current address and scan backwards
	// This is a simplified approach - a more thorough implementation would
	// track register values through the control flow.

	currentAddr := ar.dis.ProgramCounter()

	// Look back up to 20 bytes for a MOV AH, imm8 instruction
	// MOV AH, imm8 is opcode 0xB4 followed by the immediate value
	for i := uint16(2); i <= 20; i += 1 {
		if currentAddr < ProgramStart+i {
			break
		}
		addr := currentAddr - i

		b, err := ar.dis.ReadMemory(addr)
		if err != nil {
			continue
		}

		if b == 0xB4 { // MOV AH, imm8
			imm, err := ar.dis.ReadMemory(addr + 1)
			if err != nil {
				continue
			}
			return imm
		}
	}

	return 0xFF
}

// formatHex formats a byte as a hex string.
func formatHex(b uint8) string {
	return "0x" + hexDigits[b>>4:b>>4+1] + hexDigits[b&0xF:b&0xF+1]
}

const hexDigits = "0123456789abcdef"
