// Package x86 provides x86 (8086/8088) architecture support for disassembling DOS .com files.
//
// DOS .com files are simple executable programs for MS-DOS that:
//   - Load at segment offset 0x0100 (with PSP at 0x0000-0x00FF)
//   - Use 16-bit real mode x86 instructions
//   - Have a maximum size of ~64KB (one segment)
//   - Are stored as raw binary without headers
//
// This package wraps the retrogolib x86 CPU definitions and provides
// the architecture-specific logic needed by the disassembler.
package x86
