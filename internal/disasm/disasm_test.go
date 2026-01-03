package disasm

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/arch/m6502"
	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/assembler/ca65"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/arch/system/nes/parameter"
	"github.com/retroenv/retrogolib/assert"
	"github.com/retroenv/retrogolib/log"
)

func TestDisasmZeroDataReference(t *testing.T) {
	input := []byte{
		0xad, 0x20, 0x80, // lda a:$8020
		0xbd, 0x10, 0x80, // lda a:$8010,X
		0x04, 0xa9, // nop $A9
		0x40, // rti
	}

	expected := `Reset:
        lda a:_data_8020               ; $8000  AD 20 80
        lda a:_data_8010_indexed,X     ; $8003  BD 10 80
        .byte $04, $a9                   ; $8006  04 A9  disambiguous instruction: nop z:$A9
        rti                            ; $8008  40

        .byte $00, $00, $00, $00, $00, $00, $00 ; $8009

        _data_8010_indexed:
        .byte $12, $00, $00, $00, $00, $34, $00, $00, $00, $00, $00, $00, $00, $00, $00, $00 ; $8010

        _data_8020:
        .byte $00                        ; $8020
`

	setup := func(_ *options.Disassembler, cart *cartridge.Cartridge) {
		cart.PRG[0x0010] = 0x12
		cart.PRG[0x0015] = 0x34
	}
	runDisasm(t, setup, input, expected)
}

func TestDisasmBranchIntoUnofficialNop(t *testing.T) {
	input := []byte{
		0x90, 0x01, // bcc +1
		0xdc, 0xae, 0x8b, // nop $8BAE,X
		0x78, // sei
		0x40, // rti
	}

	expected := `Reset:
        bcc _label_8003
        .byte $dc                        ; disambiguous instruction: nop a:$8BAE,X

        _label_8003:
        ldx a:$788B                    ; branch into instruction detected
        rti
`

	runDisasm(t, nil, input, expected)
}

func TestDisasmReferencingUnofficialInstruction(t *testing.T) {
	input := []byte{
		0xbd, 0x06, 0x80, // $8000 lda a:_data_8005_indexed+1,X
		0x90, 0x02, // $8003 bcc _label_8007
		0x82, 0x04, // $8005 unofficial nop instruction: nop #$04
		0x40, // $8007 rti
	}

	expected := `Reset:
        lda a:_data_8005_indexed+1,X
        bcc _label_8007

        _data_8005_indexed:
        .byte $82, $04                   ; disambiguous instruction: nop #$04

        _label_8007:
        rti
`

	runDisasm(t, nil, input, expected)
}

// TestDisasmOutputUnofficialAsMnemonics tests that with OutputUnofficialAsMnemonics option,
// unofficial opcodes are output as actual mnemonics instead of .byte directives.
func TestDisasmOutputUnofficialAsMnemonics(t *testing.T) {
	input := []byte{
		0x90, 0x02, // $8000 bcc $8004
		0x82, 0x04, // $8002 unofficial nop instruction: nop #$04
		0x40, // $8004 rti
	}

	// With OutputUnofficialAsMnemonics, the unofficial nop is output as a mnemonic
	expected := `Reset:
        bcc _label_8004
        nop #$04

        _label_8004:
        rti
`

	setup := func(opts *options.Disassembler, _ *cartridge.Cartridge) {
		opts.OutputUnofficialAsMnemonics = true
		opts.OffsetComments = false
		opts.HexComments = false
	}
	runDisasm(t, setup, input, expected)
}

// TestDisasmStopAtUnofficial tests that with StopAtUnofficial option,
// unofficial opcodes that are not branch destinations stop the trace (treated as data).
// The official instructions after the unofficial one are also treated as data since
// the trace stopped at the unofficial opcode.
func TestDisasmStopAtUnofficial(t *testing.T) {
	input := []byte{
		0xbd, 0x09, 0x80, // $8000 lda a:$8009,X (references data)
		0x90, 0x05, // $8003 bcc $800a
		0x82, 0x04, // $8005 unofficial nop - trace stops here, treated as data
		0xa9, 0x42, // $8007 lda #$42 - NOT traced, becomes data
		0xea, // $8009 nop - NOT traced, becomes data
		0x40, // $800a rti - reached via branch from $8003
	}

	// With StopAtUnofficial, the trace stops at the unofficial opcode at $8005.
	// The official instructions at $8007-$8009 are NOT traced and become data.
	// $800a is still reached via the branch at $8003.
	expected := `Reset:
        lda a:_data_8009_indexed,X
        bcc _label_800a

        .byte $82, $04, $a9, $42

        _data_8009_indexed:
        .byte $ea

        _label_800a:
        rti
`

	setup := func(opts *options.Disassembler, _ *cartridge.Cartridge) {
		opts.StopAtUnofficial = true
		opts.OffsetComments = false
		opts.HexComments = false
	}
	runDisasm(t, setup, input, expected)
}

// TestDisasmStopAtBRK tests that BRK instructions stop tracing (treated as data)
// unless they are branch destinations. Multiple consecutive BRKs are typically
// ROM padding ($00 bytes).
func TestDisasmStopAtBRK(t *testing.T) {
	input := []byte{
		0xbd, 0x08, 0x80, // $8000 lda a:$8008,X (references data)
		0x90, 0x04, // $8003 bcc $8009 (branch over BRKs)
		0x00, // $8005 brk - stops trace, treated as data
		0x00, // $8006 brk (consecutive BRK, padding)
		0x00, // $8007 brk (consecutive BRK, padding)
		0x40, // $8008 rti - never reached by trace, becomes data
		0x40, // $8009 rti - reached via branch from $8003
	}

	// BRK at $8005 stops the trace. Subsequent 0x00 bytes at $8006-$8007 are never
	// reached by execution flow (BRK jumps to IRQ handler), so they're treated as data.
	// RTI at $8008 is also never reached by trace, becomes data.
	// RTI at $8009 is reached via the branch at $8003.
	expected := `Reset:
        lda a:_data_8008_indexed,X
        bcc _label_8009
        brk

        .byte $00, $00

        _data_8008_indexed:
        .byte $40

        _label_8009:
        rti
`

	setup := func(opts *options.Disassembler, _ *cartridge.Cartridge) {
		opts.OffsetComments = false
		opts.HexComments = false
	}
	runDisasm(t, setup, input, expected)
}

// TestDisasmBranchToUnofficialInstruction ensures that when an unofficial instruction
// (converted to CodeAsData) is a branch destination, it doesn't get consumed by a preceding
// instruction that could overlap with it.
// This tests AssemblerSupportsUnofficial=false (nesasm mode) where trace continues but output is .byte
func TestDisasmBranchToUnofficialInstruction(t *testing.T) {
	input := []byte{
		0x10, 0x01, // $8000: bpl $8003 (branch to unofficial instruction)
		0x10,       // $8002: byte that looks like BPL opcode
		0x83, 0x00, // $8003: sax ($00,X) - unofficial instruction, branch destination
		0x40, // $8005: rti
	}

	expected := `
        _var_0000_indexed = $0000

        Reset:
        bpl _label_8003
        .byte $10                        ; bpl $7F87

        _label_8003:
        .byte $83, $00                   ; branch into instruction detected
        rti
`

	setup := func(opts *options.Disassembler, _ *cartridge.Cartridge) {
		opts.AssemblerSupportsUnofficial = false
		opts.OffsetComments = false
		opts.HexComments = false
	}
	runDisasm(t, setup, input, expected)
}

func TestDisasmJumpEngineTableFromCaller(t *testing.T) {
	input := []byte{
		0x20, 0x05, 0x80, // jsr $8005
		0x1a, 0x80, // .word 801a
		0x0a,       // 8005: asl a
		0xa8,       // tay
		0x68,       // pla
		0x85, 0x04, // sta $04
		0x68,       // pla
		0x85, 0x05, // sta $05
		0xc8,       // iny
		0xb1, 0x04, // lda $04,Y
		0x85, 0x06, // sta $06
		0xc8,       // iny
		0xb1, 0x04, // lda $04,Y
		0x85, 0x07, // sta $07
		0x6C, 0x06, 0x00, // jmp ($0006)
		0x40, // 801a: rti
	}

	expected := `
        _var_0004_indexed = $0004
        _var_0006 = $0006
        
        Reset:
        jsr _jump_engine_8005
        
        .word _label_801a
        
        _jump_engine_8005:               ; jump engine detected
        asl a
        tay
        pla
        sta z:_var_0004_indexed
        pla
        sta z:$05
        iny
        lda (_var_0004_indexed),Y
        sta z:_var_0006
        iny
        lda (_var_0004_indexed),Y
        sta z:$07
        jmp (_var_0006)
        
        _label_801a:
        rti
`

	runDisasm(t, nil, input, expected)
}

func TestDisasmJumpEngineTableAppended(t *testing.T) {
	input := []byte{
		0xa5, 0xd7, // lda z:$D7
		0x0a,             // asl a
		0xaa,             // tax
		0xbd, 0x15, 0x80, // lda a:$8015,X
		0x8d, 0x00, 0x02, // sta a:$0200
		0xbd, 0x16, 0x80, // lda a:$8016,X
		0x8d, 0x01, 0x02, // sta a:$0201
		0x6c, 0x00, 0x02, // jmp ($0200)
		0x00, 0x00,
		0x17, 0x80, // .word $8017
		0x40, // rti
	}

	expected := `
		_var_0200 = $0200
        
        Reset:                           ; jump engine detected
        lda z:$D7
        asl a
        tax
        lda a:_jump_table_8015,X
        sta a:_var_0200
        lda a:_jump_table_8015+1,X
        sta a:$0201
        jmp (_var_0200)
        
        .byte $00, $00
        
        _jump_table_8015:
        .word _label_8017
        
        _label_8017:
        rti
`

	runDisasm(t, nil, input, expected)
}

// TODO detect jump engine in generated code
func TestDisasmJumpEngineZeroPage(t *testing.T) {
	input := []byte{
		0xbd, 0x15, 0x80, // lda a:$8015,X
		0x85, 0xe4, // sta z:$e4
		0xbd, 0x16, 0x80, // lda a:$8016,X
		0x85, 0xe5, // sta z:$e5
		0xa9, 0x4c, // lda #$4c
		0x85, 0xe3, // sta z:$e3
		0x20, 0xe3, 0x00, // jsr $00e3
		0x60, // rts
		0x00, 0x00, 0x00,
		0x17, 0x80, // .word $8017
		0x60, // rts
	}

	expected := `
        _var_00e3 = $00E3
        
        Reset:
        lda a:_data_8015_indexed,X
        sta z:$E4
        lda a:_data_8016_indexed,X
        sta z:$E5
        lda #$4C
        sta z:_var_00e3
        jsr a:_var_00e3
        rts
        
        .byte $00, $00, $00
        
        _data_8015_indexed:
        .byte $17
        
        _data_8016_indexed:
        .byte $80, $60
`

	runDisasm(t, nil, input, expected)
}

func TestDisasmMixedAccess(t *testing.T) {
	input := []byte{
		0x85, 0x04, // sta $04
		0xb1, 0x04, // lda $04,Y
		0x40, // rti
	}

	expected := `
        _var_0004_indexed = $0004
        
        Reset:
        sta z:_var_0004_indexed
        lda (_var_0004_indexed),Y
        rti
`

	runDisasm(t, nil, input, expected)
}

func TestDisasmDisambiguousInstructions(t *testing.T) {
	input := []byte{
		0x4c, 0x05, 0x80, // jmp $8005
		0x04, 0xa9, // nop $A9
		0xea,       // nop
		0x30, 0xFB, // bmi $03
		0x30, 0xFA, // bmi $04
		0x40, // rti
	}

	expected := `Reset:
        jmp _label_8005
        
        _label_8003:
        .byte $04                        ; branch into instruction detected: disambiguous instruction: nop z:$A9
        
        _label_8004:
        .byte $a9
        
        _label_8005:
        nop
        bmi _label_8003
        bmi _label_8004
        rti
`

	runDisasm(t, nil, input, expected)
}

func TestDisasmDifferentCodeBaseAddress(t *testing.T) {
	input := []byte{
		0x20, 0x68, 0xa2, // jsr a268
		0xb9, 0xfe, 0xbf, // lda a:$bffe,Y
		0x40, // rti
	}

	expected := `
        _var_bffe_indexed = $BFFE
        
        Reset:
        jsr a:$A268                    ; $C000  20 68 A2
        lda a:_var_bffe_indexed,Y      ; $C003  B9 FE BF
        rti                            ; $C006  40
`

	setup := func(_ *options.Disassembler, cart *cartridge.Cartridge) {
		cart.PRG = make([]byte, 0x4000)
		cart.PRG[0x3FFD] = 0xC0 // reset handler that forces base address to $C000
	}
	runDisasm(t, setup, input, expected)
}

func TestDisasmIndirectJmp(t *testing.T) {
	input := []byte{
		0x6c, 0xce, 0x20, // jmp ($20CE)
	}

	expected := `Reset:                           ; jump engine detected
        jmp ($20CE)                    ; $8000  6C CE 20
`

	setup := func(_ *options.Disassembler, _ *cartridge.Cartridge) {}

	runDisasm(t, setup, input, expected)
}

func testProgram(t *testing.T, options options.Disassembler, cart *cartridge.Cartridge, code []byte) *Disasm {
	t.Helper()

	if len(cart.PRG) == 0x8000 {
		// point reset handler to offset 0 of PRG buffer, aka 0x8000 address
		cart.PRG[0x7FFD] = 0x80
	}

	copy(cart.PRG, code)

	logger := log.NewTestLogger(t)
	ar := m6502.New(logger, parameter.New(ca65.ParamConfig))
	disasm, err := New(logger, ar, cart, options, ca65.New)
	assert.NoError(t, err)

	return disasm
}

func trimStringList(s string) string {
	sl := strings.Split(s, "\n")
	for i, s := range sl {
		sl[i] = strings.TrimSpace(s)
	}
	s = strings.Join(sl, "\n")
	return s
}

func runDisasm(t *testing.T, setup func(options *options.Disassembler, cart *cartridge.Cartridge), input []byte, expected string) {
	t.Helper()

	opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
	opts.CodeOnly = true

	cart := cartridge.New()

	if setup != nil {
		setup(&opts, cart)
	} else {
		opts.OffsetComments = false
		opts.HexComments = false
	}

	disasm := testProgram(t, opts, cart, input)

	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)

	newBankWriter := func(_ string) (io.WriteCloser, error) {
		return nil, nil // nolint: nilnil
	}

	app, err := disasm.Process(context.Background(), writer, newBankWriter)
	assert.NoError(t, err)
	assert.True(t, app != nil, "app should not be nil")

	assert.NoError(t, writer.Flush())

	buf := trimStringList(buffer.String())
	expected = trimStringList(expected)
	assert.Equal(t, expected, buf)
}
