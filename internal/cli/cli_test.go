package cli

import (
	"os"
	"testing"

	"github.com/retroenv/retrodisasm/internal/options"
	"github.com/retroenv/retrogolib/assert"
)

func TestParseFlags_DisasmOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options.Disassembler
	}{
		{
			name: "default flags",
			args: []string{"prog", "test.nes"},
			want: options.Disassembler{HexComments: true, OffsetComments: true},
		},
		{
			name: "nohexcomments flag",
			args: []string{"prog", "-nohexcomments", "test.nes"},
			want: options.Disassembler{OffsetComments: true},
		},
		{
			name: "nooffsets flag",
			args: []string{"prog", "-nooffsets", "test.nes"},
			want: options.Disassembler{HexComments: true},
		},
		{
			name: "z flag",
			args: []string{"prog", "-z", "test.nes"},
			want: options.Disassembler{HexComments: true, OffsetComments: true, ZeroBytes: true},
		},
		{
			name: "all disasm flags",
			args: []string{"prog", "-nohexcomments", "-nooffsets", "-z", "test.nes"},
			want: options.Disassembler{ZeroBytes: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldArgs := os.Args
			t.Cleanup(func() { os.Args = oldArgs })

			os.Args = tt.args

			_, got, err := ParseFlags()
			assert.NoError(t, err)
			assert.Equal(t, tt.want.HexComments, got.HexComments)
			assert.Equal(t, tt.want.OffsetComments, got.OffsetComments)
			assert.Equal(t, tt.want.ZeroBytes, got.ZeroBytes)
		})
	}
}

func TestParseFlags_CHRExport(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantInput    string
		wantExport   bool
		wantFilename string
	}{
		{
			name: "disabled",
			args: []string{"prog", "test.nes"},
		},
		{
			name:       "default filename",
			args:       []string{"prog", "-chr", "test.nes"},
			wantExport: true,
		},
		{
			name:         "custom filename",
			args:         []string{"prog", "-chr=tiles.chr", "test.nes"},
			wantExport:   true,
			wantFilename: "tiles.chr",
		},
		{
			name:         "long custom filename",
			args:         []string{"prog", "--chr=assets/tiles.chr", "test.nes"},
			wantExport:   true,
			wantFilename: "assets/tiles.chr",
		},
		{
			name:         "custom filename after input",
			args:         []string{"prog", "test.nes", "-chr=tiles.chr"},
			wantExport:   true,
			wantFilename: "tiles.chr",
		},
		{
			name:      "flag-like input after terminator",
			args:      []string{"prog", "--", "-chr"},
			wantInput: "-chr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldArgs := os.Args
			t.Cleanup(func() { os.Args = oldArgs })
			os.Args = tt.args

			opts, _, err := ParseFlags()
			assert.NoError(t, err)
			wantInput := tt.wantInput
			if wantInput == "" {
				wantInput = "test.nes"
			}
			assert.Equal(t, wantInput, opts.Input)
			assert.Equal(t, tt.wantExport, opts.ExportCHR)
			assert.Equal(t, tt.wantFilename, opts.CHRFilename)
		})
	}
}

func TestValidateOptionCombinations(t *testing.T) {
	tests := []struct {
		name        string
		opts        options.Program
		disasmOpts  options.Disassembler
		expectError bool
	}{
		{
			name:        "no conflict",
			opts:        options.Program{},
			disasmOpts:  options.Disassembler{},
			expectError: false,
		},
		{
			name: "verify only",
			opts: options.Program{
				Flags: options.Flags{AssembleTest: true},
			},
			disasmOpts:  options.Disassembler{},
			expectError: false,
		},
		{
			name:        "output unofficial only",
			opts:        options.Program{},
			disasmOpts:  options.Disassembler{OutputUnofficialAsMnemonics: true},
			expectError: false,
		},
		{
			name: "verify and output unofficial conflict",
			opts: options.Program{
				Flags: options.Flags{AssembleTest: true},
			},
			disasmOpts:  options.Disassembler{OutputUnofficialAsMnemonics: true},
			expectError: true,
		},
		{
			name: "custom CHR filename in batch mode",
			opts: options.Program{
				Parameters: options.Parameters{Batch: "*.nes", CHRFilename: "tiles.chr"},
				ExportCHR:  true,
			},
			disasmOpts:  options.Disassembler{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptionCombinations(tt.opts, tt.disasmOpts)
			if tt.expectError {
				assert.True(t, err != nil)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
