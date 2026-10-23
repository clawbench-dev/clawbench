package ai

// MergeMetadata fills dst's empty fields from src, leaving every value dst
// already carries untouched.
//
// Used by the executor's post-loop drain (see SessionExecutor.mergeDrainedMetadata):
// a metadata event that arrives after the event loop exited describes the same
// turn, so it may only contribute what is still missing. Replacing outright
// would drop WallMs and the session-level model/transport injected by
// buildResult, while letting src's zeros win would erase the very usage the
// drain exists to rescue.
func MergeMetadata(dst, src *Metadata) {
	if dst == nil || src == nil {
		return
	}
	fillStr := func(d *string, s string) {
		if *d == "" {
			*d = s
		}
	}
	fillInt := func(d *int, s int) {
		if *d == 0 {
			*d = s
		}
	}
	fillFloat := func(d *float64, s float64) {
		if *d == 0 {
			*d = s
		}
	}

	fillStr(&dst.Mode, src.Mode)
	fillStr(&dst.ThinkingEffort, src.ThinkingEffort)
	fillStr(&dst.Transport, src.Transport)
	fillStr(&dst.Model, src.Model)
	fillStr(&dst.SessionID, src.SessionID)
	fillStr(&dst.StopReason, src.StopReason)
	fillStr(&dst.ErrorMessage, src.ErrorMessage)
	fillStr(&dst.RequestID, src.RequestID)
	fillStr(&dst.TraceID, src.TraceID)
	fillStr(&dst.MessageID, src.MessageID)
	fillStr(&dst.MessageRequestID, src.MessageRequestID)
	fillStr(&dst.RequestModelName, src.RequestModelName)
	fillStr(&dst.ResponseModelID, src.ResponseModelID)
	fillStr(&dst.FinishReason, src.FinishReason)
	fillStr(&dst.Outcome, src.Outcome)
	fillStr(&dst.AgentPhase, src.AgentPhase)

	fillInt(&dst.InputTokens, src.InputTokens)
	fillInt(&dst.OutputTokens, src.OutputTokens)
	fillInt(&dst.DurationMs, src.DurationMs)
	fillInt(&dst.WallMs, src.WallMs)
	fillInt(&dst.TotalTokens, src.TotalTokens)
	fillInt(&dst.CachedReadTokens, src.CachedReadTokens)
	fillInt(&dst.CachedWriteTokens, src.CachedWriteTokens)
	fillInt(&dst.ThoughtTokens, src.ThoughtTokens)
	fillInt(&dst.CacheCreationTokens, src.CacheCreationTokens)
	fillInt(&dst.CacheHitTokens, src.CacheHitTokens)
	fillInt(&dst.CacheMissTokens, src.CacheMissTokens)

	fillFloat(&dst.CostUSD, src.CostUSD)
	fillFloat(&dst.Credit, src.Credit)
	if dst.CostSessionTotal == 0 {
		dst.CostSessionTotal = src.CostSessionTotal
	}
	// CostCumulative is the flag that makes the ledger diff this value against
	// the previous turn's total (service.normalizeLedgerCost). It must travel
	// with CostUSD/CostSessionTotal — a filled-in cumulative amount without the
	// flag would be stored verbatim and counted once per message.
	if src.CostCumulative {
		dst.CostCumulative = true
	}
	if src.IsError {
		dst.IsError = true
	}
	if len(dst.UsageByCategory) == 0 && len(src.UsageByCategory) > 0 {
		dst.UsageByCategory = src.UsageByCategory
	}
}
