package model_test

// This file exists ONLY for its side effect: importing the backend plugins runs
// their init(), which registers each backend's ProviderEnv spec and MCP-support
// flag into the model registries.
//
// The tests in this directory that live in `package model` (provider_env_test.go,
// mcp_test.go) read those registries. Both test packages are compiled into the
// same binary, and Go initializes every imported package before any test runs,
// so this blank import is what populates them.
//
// It is a separate file rather than piggybacking on discovery_test.go's import:
// that test could be moved or deleted, and the failure mode would be a silently
// empty registry (every BYOK test failing for a reason that has nothing to do
// with the change).
import _ "clawbench/internal/ai/backends/all"
