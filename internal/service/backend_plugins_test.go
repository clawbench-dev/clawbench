package service_test

// This file exists ONLY for its side effect: importing the backend plugins runs
// their init(), which registers each backend's ProviderEnv spec (BYOK env
// variables, config-file mode) into the model registry.
//
// The provider-config tests in this directory (package service) read that
// registry through model.ProviderUsesConfigFile / BuildProviderEnv. The internal
// test package cannot import backends/all without closing the
// service → ai → backends cycle, so the import lives in the external test
// package, which is compiled into the same binary and initialized before any
// test runs.
import _ "clawbench/internal/ai/backends/all"
