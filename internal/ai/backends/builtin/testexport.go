package builtin

import "clawbench/internal/ai"

// NewForTest constructs the in-process backend outside the registry, for callers
// (tests in other packages) that drive one turn directly instead of resolving a
// registered backend. It is the same constructor the plugin registration uses, so
// there is no second implementation that could drift.
func NewForTest() ai.AIBackend { return newBackend() }
