package service

import (
	"clawbench/internal/ai"
	builtin "clawbench/internal/ai/backends/builtin"
)

// newBuiltinBackendForTest builds the in-process backend directly, bypassing the
// registry lookup: this test drives one turn and inspects the frames it emits, so
// there is nothing to resolve.
func newBuiltinBackendForTest() ai.AIBackend { return builtin.NewForTest() }
