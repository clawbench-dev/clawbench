package handler_test

// This file exists ONLY for its side effect: importing the backend plugins runs
// their init(), which registers each backend's ProviderEnv spec into the model
// registry.
//
// The provider/agent handler tests live in `package handler` and validate
// bindings against real backends (cursor is key-only, antigravity speaks Gemini,
// …). Those checks read the model registry. An internal test package cannot
// import backends/all without closing the handler → ai → backends cycle, so the
// import lives in the external test package, which is compiled into the same
// binary and initialized before any test runs.
import _ "clawbench/internal/ai/backends/all"
