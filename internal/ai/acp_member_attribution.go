package ai

// Agent Teams attribution used to live here as a set of package functions that
// read CodeBuddy's private _meta keys and joined a member name to its spawning
// Agent tool call. It now belongs to the backend that ships the shape: see
// internal/ai/backends/codebuddy/attribution.go, reached through
// ACPBehavior.AttributeFrame.
//
// What remains in this package is the ONE key the shared LoadSession path still
// reads directly, because the end-of-replay decision is shared: a replayed
// history must not be treated as live content by any backend.

// metaKeyCodeBuddyHistoryReplay marks a LoadSession history replay boundary:
// "start" / "end" (verified against a real process). The "end" marker is the
// authoritative end-of-replay signal for the load handler.
const metaKeyCodeBuddyHistoryReplay = "codebuddy.ai/historyReplay"
