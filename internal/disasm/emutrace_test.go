package disasm

import (
	"os"
	"testing"

	"github.com/retroenv/retrogolib/assert"
)

func TestParseJoypadSequence(t *testing.T) {
	sequence, err := parseJoypadSequence("8,0,$10,0x20 255")
	assert.NoError(t, err)
	assert.Equal(t, []byte{8, 0, 16, 32, 255}, sequence)
}

func TestParseJoypadSequenceInvalidToken(t *testing.T) {
	_, err := parseJoypadSequence("8,zz,1")
	assert.True(t, err != nil)
}

func TestParseJoypadSequenceSettingPrefersCLI(t *testing.T) {
	oldValue := os.Getenv(envEmuTraceJoypad1Seq)
	t.Cleanup(func() {
		_ = os.Setenv(envEmuTraceJoypad1Seq, oldValue)
	})

	_ = os.Setenv(envEmuTraceJoypad1Seq, "1,2,3")

	sequence, err := parseJoypadSequenceSetting("4,5", envEmuTraceJoypad1Seq)
	assert.NoError(t, err)
	assert.Equal(t, []byte{4, 5}, sequence)
}

func TestParseJoypadSequenceSettingUsesEnv(t *testing.T) {
	oldValue := os.Getenv(envEmuTraceJoypad2Seq)
	t.Cleanup(func() {
		_ = os.Setenv(envEmuTraceJoypad2Seq, oldValue)
	})

	_ = os.Setenv(envEmuTraceJoypad2Seq, "0x01,$02,3")

	sequence, err := parseJoypadSequenceSetting("", envEmuTraceJoypad2Seq)
	assert.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3}, sequence)
}
