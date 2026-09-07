package ai

import (
	"encoding/json"
	"strings"
)

// ---------------------------------------------------------------------------
// ACP _meta extension parsing — per-agent adapters
// ---------------------------------------------------------------------------
//
// ACP reserves the `_meta` field on every request/response/notification for
// agent-specific extensions. Each agent packs useful information there in a
// different shape:
//
//   - CodeBuddy: OpenAI-style token usage (`_meta.usage` with prompt_tokens /
//     completion_tokens / prompt_cache_* / credit) plus a `codebuddy.ai/*`
//     namespace (usageByCategory, requestId, traceId, modelId, ...). Reported
//     both on session/update notifications and the PromptResponse.
//   - Claude / Codex (shared bridge protocol stack): `PromptResponse._meta.quota`
//     with a per-model `token_count` (cachedInputTokens / cachedWriteTokens /
//     inputTokens / outputTokens / reasoningOutputTokens / totalTokens).
//   - OpenCode: no _meta extensions — its usage arrives through the standard
//     usage_update notification and PromptResponse.Usage.
//
// The dispatch mirrors parseACPToolCall: per-agent functions handle their own
// shape, an unknown backend falls through to a generic recursive scan, and
// callers merge whatever is found into the canonical UsageState / Metadata.

// metaTokenUsage is the canonical, agent-agnostic token/cost detail extracted
// from a _meta payload.
type metaTokenUsage struct {
	// Token counters (0 = not reported by this agent).
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedReadTokens  int
	CachedWriteTokens int
	ThoughtTokens     int
	// Cache splits (CodeBuddy OpenAI-style detail).
	CacheCreationTokens int
	CacheHitTokens      int
	CacheMissTokens     int
	Credit              float64
	// Present reports whether any token/cost field was found at all.
	Present bool
}

// metaTrace is the canonical trace/identity detail extracted from a _meta payload.
type metaTrace struct {
	RequestID        string
	TraceID          string
	TraceParent      string
	MessageID        string
	MessageRequestID string
	RequestModelID   string
	RequestModelName string
	ResponseModelID  string
	FinishReason     string
	Outcome          string
	AgentPhase       string
}

// HasData reports whether any trace field was found.
func (t *metaTrace) HasData() bool {
	return t != nil && (t.RequestID != "" || t.TraceID != "" || t.TraceParent != "" ||
		t.MessageID != "" || t.MessageRequestID != "" || t.RequestModelID != "" ||
		t.RequestModelName != "" || t.ResponseModelID != "" || t.FinishReason != "" ||
		t.Outcome != "" || t.AgentPhase != "")
}

// metaCategoryUsage extracts the context-window breakdown map. Currently only
// CodeBuddy reports it (_meta.codebuddy.ai/usageByCategory).
type metaCategoryUsage struct {
	Categories map[string]int64
	Present    bool
}

// metaExtraction bundles everything extracted from one _meta payload.
type metaExtraction struct {
	Usage    *metaTokenUsage
	Trace    *metaTrace
	Category *metaCategoryUsage
}

// HasData reports whether any field was extracted.
func (e *metaExtraction) HasData() bool {
	return e != nil && ((e.Usage != nil && e.Usage.Present) || (e.Trace != nil && e.Trace.HasData()) ||
		(e.Category != nil && e.Category.Present))
}

// extractMetaUsage dispatches _meta parsing to the per-agent adapter.
// backend is the BackendID (e.g. "codebuddy", "claude", "codex", "opencode").
func extractMetaUsage(backend string, meta map[string]any) *metaExtraction {
	switch backend {
	case "codebuddy":
		return extractCodeBuddyMeta(meta)
	case "claude", "codex", "qoder":
		return extractClaudeMeta(meta)
	default:
		return extractGenericMeta(meta)
	}
}

// ---------------------------------------------------------------------------
// Shared _meta helpers
// ---------------------------------------------------------------------------

// metaFloat reads a numeric field that may be float64, int, or json.Number.
func metaFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// metaInt reads an integer field that may be float64, int, or json.Number.
func metaInt(v any) int {
	return int(metaFloat(v))
}

// metaString reads a string field.
func metaString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// splitTraceParent splits a W3C traceparent header
// ("00-<traceid>-<spanid>-<flags>") into its dash-separated components.
func splitTraceParent(tp string) []string {
	return strings.Split(tp, "-")
}

// metaMaxInt returns the maximum of base and vals (used to keep the richest
// value across multiple notifications).
func metaMaxInt(base int, vals ...int) int {
	out := base
	for _, v := range vals {
		if v > out {
			out = v
		}
	}
	return out
}

// metaTokenUsageInformative reports whether the usage carries real token
// counters. Cost-only "naked" notifications (CodeBuddy sends used=0/size=0
// cost-only payloads) may still set credit while every token counter is zero —
// such payloads must never displace an adopted token snapshot.
func (u *metaTokenUsage) hasTokenCounters() bool {
	return u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 ||
		u.CachedReadTokens != 0 || u.CachedWriteTokens != 0 || u.ThoughtTokens != 0 ||
		u.CacheCreationTokens != 0 || u.CacheHitTokens != 0 || u.CacheMissTokens != 0
}

// metaMergeTrace overlays src onto dst, filling empty fields.
func metaMergeTrace(dst *metaTrace, src *metaTrace) {
	if src == nil || dst == nil {
		return
	}
	if dst.RequestID == "" {
		dst.RequestID = src.RequestID
	}
	if dst.TraceID == "" {
		dst.TraceID = src.TraceID
	}
	if dst.TraceParent == "" {
		dst.TraceParent = src.TraceParent
	}
	if dst.MessageID == "" {
		dst.MessageID = src.MessageID
	}
	if dst.MessageRequestID == "" {
		dst.MessageRequestID = src.MessageRequestID
	}
	if dst.RequestModelID == "" {
		dst.RequestModelID = src.RequestModelID
	}
	if dst.RequestModelName == "" {
		dst.RequestModelName = src.RequestModelName
	}
	if dst.ResponseModelID == "" {
		dst.ResponseModelID = src.ResponseModelID
	}
	if dst.FinishReason == "" {
		dst.FinishReason = src.FinishReason
	}
	if dst.Outcome == "" {
		dst.Outcome = src.Outcome
	}
	if dst.AgentPhase == "" {
		dst.AgentPhase = src.AgentPhase
	}
}

// metaMergeExtraction merges src into dst (the turn-level accumulator).
//
// Token/cache/credit usage adopts the LATEST informative snapshot wholesale.
// ACP agents (CodeBuddy in particular) re-report the complete current usage on
// each informative session/update notification, so the most recent one is
// authoritative. Per-field max-stitching was removed: it combined counters
// taken at different moments into one internally inconsistent row (e.g. a
// cacheMiss from an early tool call with input from a later one, or a Claude
// per-model total dwarfing the request) — which corrupted chat_metadata usage
// stats. A cost-only "naked" notification (all token counters zero) never
// replaces the adopted snapshot.
func metaMergeExtraction(dst *metaExtraction, src *metaExtraction) {
	if src == nil || dst == nil {
		return
	}
	if src.Usage != nil && src.Usage.hasTokenCounters() {
		cp := *src.Usage
		dst.Usage = &cp
	}
	if src.Trace != nil && src.Trace.HasData() {
		if dst.Trace == nil {
			dst.Trace = &metaTrace{}
		}
		metaMergeTrace(dst.Trace, src.Trace)
	}
	// Category breakdown is also a full re-report; adopt the latest one. It may
	// arrive on a notification whose token counters are zero (CodeBuddy sends
	// usageByCategory on a separate update), so it is independent of the usage
	// block above.
	if src.Category != nil && src.Category.Present && len(src.Category.Categories) > 0 {
		dst.Category = &metaCategoryUsage{Present: true, Categories: src.Category.Categories}
	}
}

// setIntIfNonZero overlays v onto *dst only when v != 0. Keeping the
// non-zero guard in one place collapses the repetitive "if src != 0 { dst =
// src }" chains into flat calls (identical overlay semantics for UsageState
// and Metadata).
func setIntIfNonZero(dst *int, v int) {
	if v != 0 {
		*dst = v
	}
}

// setFloatIfNonZero is the float64 counterpart of setIntIfNonZero.
func setFloatIfNonZero(dst *float64, v float64) {
	if v != 0 {
		*dst = v
	}
}

// applyMetaExtractionToUsageState overlays a parsed _meta extraction onto a
// UsageState (used for the usage_update event payload and cached state).
func applyMetaExtractionToUsageState(state *UsageState, ext *metaExtraction) {
	if ext == nil || state == nil {
		return
	}
	if u := ext.Usage; u != nil {
		applyTokenUsageTo(&state.InputTokens, &state.OutputTokens, &state.TotalTokens,
			&state.CachedReadTokens, &state.CachedWriteTokens, &state.ThoughtTokens,
			&state.CacheCreationTokens, &state.CacheHitTokens, &state.CacheMissTokens,
			&state.Credit, u)
	}
	if cat := ext.Category; cat != nil && cat.Present {
		state.UsageByCategory = cat.Categories
	}
}

// applyMetaExtractionToMetadata overlays a parsed _meta extraction onto a
// message-level Metadata (used for the metadata event / DB persistence).
func applyMetaExtractionToMetadata(meta *Metadata, ext *metaExtraction) {
	if ext == nil || meta == nil {
		return
	}
	if u := ext.Usage; u != nil {
		applyTokenUsageTo(&meta.InputTokens, &meta.OutputTokens, &meta.TotalTokens,
			&meta.CachedReadTokens, &meta.CachedWriteTokens, &meta.ThoughtTokens,
			&meta.CacheCreationTokens, &meta.CacheHitTokens, &meta.CacheMissTokens,
			&meta.Credit, u)
	}
	if cat := ext.Category; cat != nil && cat.Present {
		meta.UsageByCategory = cat.Categories
	}
	if tr := ext.Trace; tr != nil {
		applyTraceToMetadata(meta, tr)
	}
}

// applyTokenUsageTo overlays the non-zero token/cost fields of u onto the ten
// destination pointers. The callees share field layout across UsageState and
// Metadata, so one helper keeps both apply paths identical.
func applyTokenUsageTo(input, output, total, cachedRead, cachedWrite, thought,
	cacheCreation, cacheHit, cacheMiss *int, credit *float64, u *metaTokenUsage,
) {
	setIntIfNonZero(input, u.InputTokens)
	setIntIfNonZero(output, u.OutputTokens)
	setIntIfNonZero(total, u.TotalTokens)
	setIntIfNonZero(cachedRead, u.CachedReadTokens)
	setIntIfNonZero(cachedWrite, u.CachedWriteTokens)
	setIntIfNonZero(thought, u.ThoughtTokens)
	setIntIfNonZero(cacheCreation, u.CacheCreationTokens)
	setIntIfNonZero(cacheHit, u.CacheHitTokens)
	setIntIfNonZero(cacheMiss, u.CacheMissTokens)
	setFloatIfNonZero(credit, u.Credit)
}

// applyTraceToMetadata overlays the non-empty trace/identity fields of tr
// onto a message-level Metadata.
func applyTraceToMetadata(meta *Metadata, tr *metaTrace) {
	if tr.RequestID != "" {
		meta.RequestID = tr.RequestID
	}
	if tr.TraceID != "" {
		meta.TraceID = tr.TraceID
	}
	if tr.MessageID != "" {
		meta.MessageID = tr.MessageID
	}
	if tr.MessageRequestID != "" {
		meta.MessageRequestID = tr.MessageRequestID
	}
	if tr.RequestModelID != "" && meta.Model == "" {
		meta.Model = tr.RequestModelID
	}
	if tr.RequestModelName != "" {
		meta.RequestModelName = tr.RequestModelName
	}
	if tr.ResponseModelID != "" {
		meta.ResponseModelID = tr.ResponseModelID
	}
	if tr.FinishReason != "" {
		meta.FinishReason = tr.FinishReason
	}
	if tr.Outcome != "" {
		meta.Outcome = tr.Outcome
	}
	if tr.AgentPhase != "" {
		meta.AgentPhase = tr.AgentPhase
	}
}

// mergeMetaExtractionToConn accumulates per-agent _meta extensions onto the
// connection so the turn-final metadata event can include everything observed.
// Safe to call from the ACP notification goroutine: it uses the dedicated
// metaMu lock, not c.mu (which would deadlock with RPCs holding c.mu).
func mergeMetaExtractionToConn(conn *ACPConn, backendID string, meta map[string]any) {
	if conn == nil || len(meta) == 0 {
		return
	}
	ext := extractMetaUsage(backendID, meta)
	if ext == nil {
		return
	}
	conn.mergeMetaExtraction(ext)
}

// MergeUsageState merges an incoming partial usage_update onto the existing
// session usage state, returning the resulting state.
//
// ACP agents do NOT send usage_update notifications as full snapshots.
// CodeBuddy in particular distributes its extension detail (cache/credit,
// usageByCategory, token counters) across multiple notifications within one
// turn, and many notifications are "naked" — carrying only cost with
// used=0, size=0 and no _meta at all. Treating every notification as a full
// snapshot (unconditional overwrite) lets a naked trailing notification wipe a
// previously-known context window to {used:0, size:0} — the "context panel
// shows all zeros after many turns" bug. See the frontend's updateUsageState
// in web/src/composables/useSessionIdentity.ts which already implements the
// same partial-update semantics for the same reason.
//
// Merge rules (incoming is a partial update — non-informative values never
// regress existing state):
//
//   - size: sticky. A known non-zero window is a session-level invariant (set
//     by the model) and is never regressed by an incoming 0/absent size. A
//     non-zero size change (e.g. model switch) is accepted.
//   - used: the distinguishing signal is size. When incoming.Size > 0 the
//     notification carries the authoritative window, so its used is trusted
//     outright — including a genuine 0 (empty/compacted context). A 0 used is
//     only backfilled from the usageByCategory sum in the anomalous case where
//     the agent reported a window plus a populated breakdown but omitted used.
//     When incoming.Size == 0 (a "naked" cost-only notification) an incoming
//     used > 0 still applies, but a naked 0 keeps the existing used — this is
//     the core regression guard: a naked trailing notification must never wipe
//     a known window to {used:0, size:0}.
//   - token/cache/credit extension fields: incoming non-zero → update;
//     incoming zero/absent → keep existing (partial notifications omit them).
//   - cost: monotonic cumulative. An incoming cost lower than the existing one
//     is treated as "no new cost info" and does not regress the total.
//   - currency: filled when incoming carries one.
//
// existing may be nil (first observation) — incoming is then used as-is.
// incoming must not be nil.
func MergeUsageState(existing, incoming *UsageState) *UsageState {
	if incoming == nil {
		return existing
	}
	if existing == nil {
		return cloneUsageState(incoming)
	}
	out := cloneUsageState(existing)

	// Size is a session-level invariant: never regress a known window to 0.
	if incoming.Size > 0 {
		out.Size = incoming.Size
	}
	// Cost is monotonic cumulative: never regress the running total.
	if incoming.Cost > existing.Cost {
		out.Cost = incoming.Cost
	}
	if incoming.Currency != "" {
		out.Currency = incoming.Currency
	}

	switch {
	case incoming.Size > 0:
		// Authoritative window snapshot — trust its used outright. A genuine 0
		// (empty/compacted context) stays 0; only the anomalous "window plus
		// populated breakdown but used omitted" case is backfilled.
		out.Used = incoming.Used
		if incoming.Used <= 0 {
			if sum := sumCategory(incoming.UsageByCategory); sum > 0 {
				out.Used = int(sum)
			}
		}
	case incoming.Used > 0:
		// Naked notification without window info but a real used — apply it.
		out.Used = incoming.Used
	default:
		// Naked zero (used=0, size=0, cost-only): keep the existing used — the
		// core regression guard against wiping a known window.
	}

	// Token/cache/credit extension fields: non-zero incoming values update;
	// zero/absent keep the existing value (partial notifications omit them).
	setIntIfNonZero(&out.InputTokens, incoming.InputTokens)
	setIntIfNonZero(&out.OutputTokens, incoming.OutputTokens)
	setIntIfNonZero(&out.TotalTokens, incoming.TotalTokens)
	setIntIfNonZero(&out.CachedReadTokens, incoming.CachedReadTokens)
	setIntIfNonZero(&out.CachedWriteTokens, incoming.CachedWriteTokens)
	setIntIfNonZero(&out.ThoughtTokens, incoming.ThoughtTokens)
	setIntIfNonZero(&out.CacheCreationTokens, incoming.CacheCreationTokens)
	setIntIfNonZero(&out.CacheHitTokens, incoming.CacheHitTokens)
	setIntIfNonZero(&out.CacheMissTokens, incoming.CacheMissTokens)
	setFloatIfNonZero(&out.Credit, incoming.Credit)

	// Category breakdown: an incoming meaningful breakdown replaces the stale
	// one (it is the authoritative per-notification view); an empty/zero one is
	// omitted by the agent and keeps the existing breakdown.
	if sumCategory(incoming.UsageByCategory) > 0 {
		out.UsageByCategory = cloneCategory(incoming.UsageByCategory)
	}
	return out
}

// sumCategory sums the token counts of a usageByCategory breakdown.
func sumCategory(cat map[string]int64) int64 {
	var total int64
	for _, v := range cat {
		total += v
	}
	return total
}

// cloneUsageState returns a deep-enough copy of a UsageState (category map is
// cloned so later mutations of the source never leak into the cached state).
func cloneUsageState(s *UsageState) *UsageState {
	if s == nil {
		return nil
	}
	c := *s
	if s.UsageByCategory != nil {
		c.UsageByCategory = cloneCategory(s.UsageByCategory)
	}
	return &c
}

// cloneCategory copies a usageByCategory breakdown map.
func cloneCategory(cat map[string]int64) map[string]int64 {
	if cat == nil {
		return nil
	}
	out := make(map[string]int64, len(cat))
	for k, v := range cat {
		out[k] = v
	}
	return out
}
