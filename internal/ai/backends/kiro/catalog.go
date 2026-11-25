package kiro

import "clawbench/internal/model"

// KiroCatalog is the fallback model list for the picker.
//
// It is deliberately MINIMAL. Kiro has no usable local model command — the
// catalog is server-side and gated. Measured on kiro-cli 3.5.0:
//
//   - signed out: `kiro-cli chat --list-models --format json` prints
//     "Not logged in. Set the KIRO_API_KEY environment variable or run
//     `kiro-cli login` first." and exits without a list;
//   - with a key the service rejects: it prints "error: service error", and an
//     ACP turn answers -32000 ModelRegistryAccessDeniedError.
//
// So unlike grok/antigravity there is no probe worth shipping here, and the
// only entry that can be asserted without guessing an id format is the one
// Kiro documents as the recommended default ("Auto"). The real ids arrive
// through the ACP session once a paying account drives one; whoever captures
// them should extend this file from that frame, not from the docs' display
// names (the docs list "Claude Sonnet 4.5" etc. but never the id string).
//
// Because the list is short, every undocumented id here is a guess that could
// silently fail with "model not found" — add only what was observed.
var KiroCatalog = []model.AgentModel{
	{ID: "auto", Name: "Auto (recommended)", Default: true},
}
