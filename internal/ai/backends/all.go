package backends

// This package defines the plugin framework (types + registry).
// Sub-packages (backends/codebuddy, etc.) import this package to call Register().
// The application's main package imports sub-packages for side effects:
//
//	import (
//	    _ "clawbench/internal/ai/backends/claude"
//	    _ "clawbench/internal/ai/backends/codebuddy"
//	    // etc.
//	)
//
// Do NOT import sub-packages from this file — it creates import cycles.
// Sub-package imports are in cmd/server/main.go.
