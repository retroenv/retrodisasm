package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/arch"
	"github.com/retroenv/retrogolib/assert"
)

//nolint:funlen,cyclop // test functions can be long and complex
func TestLoad(t *testing.T) {
	t.Run("load binary file", func(t *testing.T) {
		tmpFile := createTempFile(t, []byte{0x01, 0x02, 0x03, 0x04})
		defer os.Remove(tmpFile) //nolint:errcheck // test cleanup

		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: tmpFile},
			Flags:      options.Flags{Binary: true},
		}

		cart, cdlReader, err := loader.Load(opts, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.Nil(t, cdlReader)
	})

	t.Run("load CHIP8 file", func(t *testing.T) {
		tmpFile := createTempFile(t, []byte{0x12, 0x34, 0x56, 0x78})
		defer os.Remove(tmpFile) //nolint:errcheck // test cleanup

		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: tmpFile},
		}

		cart, cdlReader, err := loader.Load(opts, arch.CHIP8System)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.Nil(t, cdlReader)
	})

	t.Run("load NES file with valid header", func(t *testing.T) {
		// Create minimal valid NES ROM with iNES header
		nesData := make([]byte, 16+16384) // Header + 16KB PRG
		copy(nesData[0:4], []byte{'N', 'E', 'S', 0x1A})
		nesData[4] = 1 // 1 PRG bank

		tmpFile := createTempFile(t, nesData)
		defer os.Remove(tmpFile) //nolint:errcheck // test cleanup

		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: tmpFile},
		}

		cart, cdlReader, err := loader.Load(opts, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.Nil(t, cdlReader)
	})

	t.Run("error on non-existent file", func(t *testing.T) {
		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: "/nonexistent/file.nes"},
		}

		_, _, err := loader.Load(opts, arch.NES)
		assert.Error(t, err)
	})

	t.Run("load with CDL file", func(t *testing.T) {
		tmpFile := createTempFile(t, []byte{0x01, 0x02, 0x03, 0x04})
		defer os.Remove(tmpFile) //nolint:errcheck // test cleanup

		tmpCDL := createTempFile(t, []byte{0xC0, 0xDE})
		defer os.Remove(tmpCDL) //nolint:errcheck // test cleanup

		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: tmpFile, CodeDataLog: tmpCDL},
			Flags:      options.Flags{Binary: true},
		}

		cart, cdlReader, err := loader.Load(opts, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.NotNil(t, cdlReader)
		_ = cdlReader.Close()
	})

	t.Run("error on non-existent CDL file", func(t *testing.T) {
		tmpFile := createTempFile(t, []byte{0x01, 0x02, 0x03, 0x04})
		defer os.Remove(tmpFile) //nolint:errcheck // test cleanup

		loader := New()
		opts := options.Program{
			Parameters: options.Parameters{Input: tmpFile, CodeDataLog: "/nonexistent/cdl.log"},
			Flags:      options.Flags{Binary: true},
		}

		_, _, err := loader.Load(opts, arch.NES)
		assert.Error(t, err)
	})
}

//nolint:funlen // test functions can be long
func TestLoadFromBytes(t *testing.T) {
	t.Run("load binary data", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04}
		loader := New()

		cart, err := loader.LoadFromBytes(data, true, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		// Verify data was loaded (LoadBuffer pads to minimum PRG bank size)
		assert.True(t, len(cart.PRG) >= len(data))
		assert.Equal(t, data[0], cart.PRG[0])
		assert.Equal(t, data[1], cart.PRG[1])
	})

	t.Run("load CHIP8 data", func(t *testing.T) {
		data := []byte{0x12, 0x34, 0x56, 0x78}
		loader := New()

		cart, err := loader.LoadFromBytes(data, false, arch.CHIP8System)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		// Verify data was loaded (LoadBuffer pads to minimum PRG bank size)
		assert.True(t, len(cart.PRG) >= len(data))
		assert.Equal(t, data[0], cart.PRG[0])
		assert.Equal(t, data[3], cart.PRG[3])
	})

	t.Run("load NES ROM from bytes", func(t *testing.T) {
		nesData := buildMinimalNESROM(1, 0)
		loader := New()

		cart, err := loader.LoadFromBytes(nesData, false, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.Len(t, cart.PRG, 16384) // 1 bank = 16KB
	})

	t.Run("load NES ROM with mapper 1", func(t *testing.T) {
		nesData := buildMinimalNESROM(2, 1)
		loader := New()

		cart, err := loader.LoadFromBytes(nesData, false, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		assert.Equal(t, uint16(1), cart.Mapper)
		assert.Len(t, cart.PRG, 32768) // 2 banks = 32KB
	})

	t.Run("error on invalid NES header", func(t *testing.T) {
		// Invalid header - missing magic number
		invalidData := make([]byte, 100)
		loader := New()

		_, err := loader.LoadFromBytes(invalidData, false, arch.NES)
		assert.Error(t, err)
	})

	t.Run("load binary mode with NES system", func(t *testing.T) {
		// Even with NES system, binary mode treats as raw data
		data := []byte{0xEA, 0xEA, 0xEA} // NOPs
		loader := New()

		cart, err := loader.LoadFromBytes(data, true, arch.NES)
		assert.NoError(t, err)
		assert.NotNil(t, cart)
		// Verify data was loaded (LoadBuffer pads to minimum PRG bank size)
		assert.True(t, len(cart.PRG) >= len(data))
		assert.Equal(t, byte(0xEA), cart.PRG[0])
		assert.Equal(t, byte(0xEA), cart.PRG[2])
	})
}

func createTempFile(t *testing.T, data []byte) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.bin")
	err := os.WriteFile(tmpFile, data, 0600)
	assert.NoError(t, err)
	return tmpFile
}

// buildMinimalNESROM creates a minimal valid NES ROM in iNES format with specified PRG size.
// The mapper parameter is placed in the header at the correct position.
func buildMinimalNESROM(prgBanks, mapper byte) []byte {
	const nesHeaderSize = 16
	const prgBankSize = 16384 // 16KB

	data := make([]byte, nesHeaderSize+int(prgBanks)*prgBankSize)

	// iNES header
	copy(data[0:4], []byte{'N', 'E', 'S', 0x1A}) // Magic number
	data[4] = prgBanks                           // Number of 16KB PRG-ROM banks
	data[5] = 0                                  // Number of 8KB CHR-ROM banks
	data[6] = mapper << 4                        // Mapper low nibble
	data[7] = mapper & 0xF0                      // Mapper high nibble

	return data
}
