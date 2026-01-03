package nasm

import "github.com/retroenv/retrogolib/arch/system/nes/parameter"

// ParamConfig configures the instruction parameter string converter for NASM.
// NASM uses different syntax than NES assemblers, but we use the same config
// structure for consistency.
var ParamConfig = parameter.Config{
	ZeroPagePrefix: "",
	AbsolutePrefix: "",
	IndirectPrefix: "[",
	IndirectSuffix: "]",
}
