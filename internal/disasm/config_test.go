package disasm

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retroenv/retrodisasm/internal/assembler"
	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrodisasm/internal/program"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/arch/system/nes/cartridge"
	"github.com/retroenv/retrogolib/assert"
)

func TestConfigNamesAndComments(t *testing.T) {
	// Explicit names must survive vector setup and the single-use variable filter.
	dis := configuredDisasm(t, []byte{0xa5, 0xc4, 0x8d, 0, 0x20, 0x60}, `
[vectors]
reset = Start
[variables]
Score = $C4
[constants]
VideoControl = $2000
[comments]
$8000 = Read score; preserve # punctuation
$8008 = Table annotation
`)
	app, output := processConfiguredDisasm(t, dis)
	assert.Equal(t, "Start", app.Handlers.Reset)
	assert.True(t, strings.Contains(output, "lda z:Score"), output)
	assert.True(t, strings.Contains(output, "sta VideoControl"), output)
	assert.True(t, strings.Contains(output, "Read score; preserve # punctuation"), output)
	assert.True(t, strings.Contains(output, "Table annotation"), output)
}

func TestConfigDataStopsTracing(t *testing.T) {
	dis := configuredDisasm(t, []byte{0xea, 0xa9, 0x01, 0x60}, "[data]\n$8001=$8003")
	app, _ := processConfiguredDisasm(t, dis)
	assert.True(t, app.PRG[0].Offsets[0].IsType(program.CodeOffset))
	assert.True(t, app.PRG[0].Offsets[1].IsType(program.DataOffset))
	assert.False(t, app.PRG[0].Offsets[1].IsType(program.CodeOffset))
}

func TestConfigCodeAfterReturn(t *testing.T) {
	dis := configuredDisasm(t, []byte{0x60, 0xa9, 1, 0x60, 0xa9, 2, 0x60}, "[code]\n$8001=$8006")
	app, _ := processConfiguredDisasm(t, dis)
	assert.True(t, app.PRG[0].Offsets[1].IsType(program.CodeOffset))
	assert.True(t, app.PRG[0].Offsets[4].IsType(program.CodeOffset))
}

func TestConfigOperandAnnotations(t *testing.T) {
	// Labels and comments inside encodings must split bytes without changing their order.
	dis := configuredDisasm(t, []byte{0xad, 0x34, 0x12, 0x60}, `
[labels]
$8001 = LowByte
[comments]
$8000 = Original instruction
$8002 = High byte
`)
	app, output := processConfiguredDisasm(t, dis)
	assert.Equal(t, []byte{0xad}, app.PRG[0].Offsets[0].Data)
	assert.Equal(t, []byte{0x34}, app.PRG[0].Offsets[1].Data)
	assert.Equal(t, []byte{0x12}, app.PRG[0].Offsets[2].Data)
	assert.True(t, strings.Contains(output, "LowByte:"), output)
	assert.True(t, strings.Contains(output, "High byte"), output)
	assert.True(t, strings.Contains(output, "Original instruction"), output)
}

func TestConfigOverlappingInstruction(t *testing.T) {
	dis := configuredDisasm(t, []byte{0xad, 0x34, 0x12, 0x60}, "[data]\n$8001=$8002")
	_, err := dis.Process(t.Context(), io.Discard, func(_ string) (io.WriteCloser, error) {
		return nopWriteCloser{io.Discard}, nil
	})
	assert.ErrorContains(t, err, "overlaps configured data")
}

func TestConfigPresentationInsideInstruction(t *testing.T) {
	// Presentation hints inside operands must survive conversion to raw bytes.
	dis := configuredDisasm(t, []byte{0xad, 0x34, 0x12, 0x60},
		"[comments_before]\n$8001=Low byte\n[blank_lines]\n$8002=2")
	app, output := processConfiguredDisasm(t, dis)
	assert.Equal(t, []byte{0xad}, app.PRG[0].Offsets[0].Data)
	assert.Equal(t, []byte{0x34}, app.PRG[0].Offsets[1].Data)
	assert.Equal(t, []byte{0x12}, app.PRG[0].Offsets[2].Data)
	assert.True(t, strings.Contains(output, "; Low byte\n"), output)
	assert.Equal(t, 2, *app.PRG[0].Offsets[2].BlankLines)
}

func TestConfigValidation(t *testing.T) {
	for _, content := range []string{
		"[rom]\nprg_crc32=00000000", "[labels]\n$1000=Outside",
		"[variables]\nRomByte=$8000", "[vectors]\nreset=$9000",
		"[vectors]\nreset=Start\n[labels]\n$8000=Other",
		"[labels]\n$8001=Reset", "[data]\n$FFF9=$FFFF",
		"[constants]\nPort=$2000\n[variables]\nVariable=$2000",
	} {
		t.Run(content, func(t *testing.T) {
			opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
			dis := testProgram(t, opts, cartridge.New(), []byte{0x60})
			assert.Error(t, dis.ApplyAnnotations(writeConfig(t, content)))
		})
	}
}

func TestConfigGeneratedSymbolCollision(t *testing.T) {
	// An annotation label must not silently collide with a hardware alias used by the ROM.
	dis := configuredDisasm(t, []byte{0xad, 2, 0x20, 0x60}, "[labels]\n$8003=PPU_STATUS")
	_, err := dis.Process(t.Context(), io.Discard, func(_ string) (io.WriteCloser, error) {
		return nopWriteCloser{io.Discard}, nil
	})
	assert.ErrorContains(t, err, "duplicate assembly symbol")
}

func TestConfigSharedVectors(t *testing.T) {
	opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
	cart := cartridge.New()
	cart.PRG[0x7ffb], cart.PRG[0x7fff] = 0x80, 0x80
	dis := testProgram(t, opts, cart, []byte{0x60})
	assert.NoError(t, dis.ApplyAnnotations(writeConfig(t, "[vectors]\nnmi=Entry\nreset=Entry\nirq=Entry")))
	app, _ := processConfiguredDisasm(t, dis)
	assert.Equal(t, program.Handlers{NMI: "Entry", Reset: "Entry", IRQ: "Entry"}, app.Handlers)
}

func TestConfigRejectsMirroredAnnotation(t *testing.T) {
	// A mirror outside the emitted PRG range would give its label the wrong address.
	opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
	cart := cartridge.New()
	cart.PRG = make([]byte, 0x4000)
	cart.PRG[0x3ffd] = 0x80
	dis := testProgram(t, opts, cart, []byte{0x60})
	assert.ErrorContains(t, dis.ApplyAnnotations(writeConfig(t, "[labels]\n$C010=Mirror")), "outside mapped program bytes")
}

func TestConfigSymbolicTablesAndOperands(t *testing.T) {
	dis := configuredDisasm(t, []byte{0xa9, 0x08, 0x60, 0, 0, 0, 0, 0, 0x00, 0x80, 0}, `
[symbols]
TableAlias = $8008
[labels]
$8008 = Table
[operands]
$8000 = <Table
[words]
$8008 = Start
[bytes]
$800A = @1
[vectors]
reset = Start
`)
	app, output := processConfiguredDisasm(t, dis)
	assert.True(t, strings.Contains(output, "lda #<(Table)"), output)
	assert.True(t, strings.Contains(output, "TableAlias:"), output)
	assert.True(t, strings.Contains(output, ".word Start"), output)
	assert.True(t, app.PRG[0].Offsets[8].IsType(program.ExpressionData))
	assert.Equal(t, []byte{0, 0x80}, app.PRG[0].Offsets[8].Data)
}

func TestConfigRejectsIncorrectExpressions(t *testing.T) {
	for _, content := range []string{
		"[operands]\n$8000=$FF", "[operands]\n$8000=Missing",
		"[operands]\n$8002=0", "[bytes]\n$8000=@1",
		"[bytes]\n$8008=$FF", "[bytes]\n$8008=$100", "[bytes]\n$FFF9=@2",
		"[bytes]\n$8008=@2\n[words]\n$8008=0",
		"[bytes]\n$8008=@2\n[labels]\n$8009=Interior",
		"[bytes]\n$8008=@2\n[comments_before]\n$8009=Interior",
		"[bytes]\n$8008=@2\n[blank_lines]\n$8009=0",
	} {
		t.Run(content, func(t *testing.T) {
			dis := configuredDisasm(t, []byte{0xa9, 1, 0x60}, content)
			_, err := dis.Process(t.Context(), io.Discard, func(_ string) (io.WriteCloser, error) {
				return nopWriteCloser{io.Discard}, nil
			})
			assert.Error(t, err)
		})
	}
}

func configuredDisasm(t *testing.T, input []byte, content string) *Disasm {
	t.Helper()
	opts := options.NewDisassembler(assembler.Ca65, arch.NES.String())
	dis := testProgram(t, opts, cartridge.New(), input)
	assert.NoError(t, dis.ApplyAnnotations(writeConfig(t, content)))
	return dis
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "game.ini")
	assert.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
	return filename
}

func processConfiguredDisasm(t *testing.T, dis *Disasm) (*program.Program, string) {
	t.Helper()
	var output bytes.Buffer
	app, err := dis.Process(t.Context(), &output, func(_ string) (io.WriteCloser, error) {
		return nopWriteCloser{&output}, nil
	})
	assert.NoError(t, err)
	return app, output.String()
}
