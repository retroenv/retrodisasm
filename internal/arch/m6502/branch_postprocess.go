package m6502

import (
	"github.com/retroenv/retrogolib/log"
)

// ProcessComplementaryBranches processes all detected complementary branch pairs
// after all code has been disassembled and branch destinations are known.
// This marks unreachable second branches and adds appropriate comments.
func (ar *Arch6502) ProcessComplementaryBranches() {
	for _, pair := range ar.complementaryBranchPairs {
		// Check if the second branch has incoming jumps from other code
		if ar.dis.IsBranchDestination(pair.SecondAddress) {
			// The second branch is reachable from other code - don't mark as unreachable
			ar.logger.Debug("Complementary branch sequence with incoming jump",
				log.Hex("first_address", pair.FirstAddress),
				log.String("first_branch", pair.FirstBranch),
				log.Hex("second_address", pair.SecondAddress),
				log.String("second_branch", pair.SecondBranch),
				log.String("status", "second branch is reachable from other code"))

			// Add comments to both instructions
			firstOffset := ar.mapper.OffsetInfo(pair.FirstAddress)
			if firstOffset.Comment != "" {
				firstOffset.Comment += "; "
			}

			firstOffset.Comment += "unconditional branch pattern (complementary branches)"

			secondOffset := ar.mapper.OffsetInfo(pair.SecondAddress)
			if secondOffset.Comment != "" {
				secondOffset.Comment += "; "
			}

			secondOffset.Comment += "reachable from other code despite complementary branch"
		} else {
			// The second branch is unreachable - mark it as dead code
			ar.logger.Debug("Marking unreachable complementary branch",
				log.Hex("first_address", pair.FirstAddress),
				log.String("first_branch", pair.FirstBranch),
				log.Hex("second_address", pair.SecondAddress),
				log.String("second_branch", pair.SecondBranch),
				log.String("status", "second branch marked as unreachable"))

			// Add comment to first instruction
			firstOffset := ar.mapper.OffsetInfo(pair.FirstAddress)
			if firstOffset.Comment != "" {
				firstOffset.Comment += "; "
			}

			firstOffset.Comment += "unconditional branch pattern (complementary branches)"

			// Mark the second branch as unreachable
			ar.dis.MarkAddressAsUnreachable(pair.SecondAddress)
		}
	}
}
