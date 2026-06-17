package backends

// This package defines the plugin framework (types + registry).
// Sub-packages (backends/codebuddy, etc.) import this package to call Register().
// The application's main package imports sub-packages for side effects:
//
//   import (
//       _ "clawbench/internal/ai/backends/claude"
//       _ "clawbench/internal/ai/backends/codebuddy"
//       // add more as they are migrated
//   )
//
// Do NOT import sub-packages from this file — it creates import cycles.

import (
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/cline"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
)
