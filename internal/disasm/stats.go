package disasm

import (
	"time"

	"github.com/retroenv/retrogolib/log"
)

type traceStats struct {
	queueRequests           uint64
	queueRejectedInvalid    uint64
	queueRejectedDuplicate  uint64
	queueAddedPrimary       uint64
	queueAddedFunctionRet   uint64
	branchDestinationsAdded uint64

	dequeuedPrimary      uint64
	dequeuedFunctionRet  uint64
	jumpEngineScanCalls  uint64
	jumpEngineEntryFound uint64

	additionalBanksConsidered uint64
	additionalBanksProcessed  uint64
	additionalBankQueueGrowth uint64
	splitSeedCandidatePairs   uint64
	splitSeedRejectedByCorr   uint64
	splitSeedRejectedPlaus    uint64
	splitSeedRejectedExtract  uint64
	splitSeedRejectInvalid    uint64
	splitSeedRejectOpcode     uint64
	splitSeedRejectShape      uint64
	splitSeedAcceptedTargets  uint64

	parsedOffsets       uint64
	alreadyParsedSkips  uint64
	inspectSkipped      uint64
	disambiguousHandled uint64
	overlapDetected     uint64
	codeBytesMarked     uint64
	codeAsDataBytes     uint64
}

func (dis *Disasm) resetTraceStats() {
	dis.stats = traceStats{}
}

func (dis *Disasm) logTraceStats(start time.Time, completed bool) {
	dis.logger.Debug("Trace stats",
		log.Bool("completed", completed),
		log.Duration("elapsed", time.Since(start)),
		log.Uint64("queue_requests", dis.stats.queueRequests),
		log.Uint64("queue_rejected_invalid", dis.stats.queueRejectedInvalid),
		log.Uint64("queue_rejected_duplicate", dis.stats.queueRejectedDuplicate),
		log.Uint64("queue_added_primary", dis.stats.queueAddedPrimary),
		log.Uint64("queue_added_function_return", dis.stats.queueAddedFunctionRet),
		log.Uint64("branch_destinations_added", dis.stats.branchDestinationsAdded),
		log.Uint64("dequeued_primary", dis.stats.dequeuedPrimary),
		log.Uint64("dequeued_function_return", dis.stats.dequeuedFunctionRet),
		log.Uint64("jump_engine_scan_calls", dis.stats.jumpEngineScanCalls),
		log.Uint64("jump_engine_entry_found", dis.stats.jumpEngineEntryFound),
		log.Uint64("additional_banks_considered", dis.stats.additionalBanksConsidered),
		log.Uint64("additional_banks_processed", dis.stats.additionalBanksProcessed),
		log.Uint64("additional_bank_queue_growth", dis.stats.additionalBankQueueGrowth),
		log.Uint64("split_seed_candidate_pairs", dis.stats.splitSeedCandidatePairs),
		log.Uint64("split_seed_rejected_correlation", dis.stats.splitSeedRejectedByCorr),
		log.Uint64("split_seed_rejected_plausibility", dis.stats.splitSeedRejectedPlaus),
		log.Uint64("split_seed_rejected_extract", dis.stats.splitSeedRejectedExtract),
		log.Uint64("split_seed_reject_invalid_target", dis.stats.splitSeedRejectInvalid),
		log.Uint64("split_seed_reject_opcode_gate", dis.stats.splitSeedRejectOpcode),
		log.Uint64("split_seed_reject_shape", dis.stats.splitSeedRejectShape),
		log.Uint64("split_seed_accepted_targets", dis.stats.splitSeedAcceptedTargets),
		log.Uint64("parsed_offsets", dis.stats.parsedOffsets),
		log.Uint64("already_parsed_skips", dis.stats.alreadyParsedSkips),
		log.Uint64("inspect_skipped", dis.stats.inspectSkipped),
		log.Uint64("disambiguous_handled", dis.stats.disambiguousHandled),
		log.Uint64("instruction_overlap_detected", dis.stats.overlapDetected),
		log.Uint64("code_bytes_marked", dis.stats.codeBytesMarked),
		log.Uint64("code_as_data_bytes", dis.stats.codeAsDataBytes),
	)
}
