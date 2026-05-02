package m6502

import (
	"github.com/retroenv/retrodisasm/internal/offset"
)

// FormatBranchReference formats an instruction with a branch destination label.
func (ar *Arch6502) FormatBranchReference(offsetInfo *offset.DisasmOffset, label string) string {
	return offset.FormatBranchReference(offsetInfo, label)
}
