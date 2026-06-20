package backends

import (
	"os/exec"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// BackendPlugin is the complete self-describing registration unit for an AI backend.
// Each backend sub-package registers itself via init() calling Register().
//
// Currently, CLI-mode registration uses ai.RegisterBackend() in factory.go,
// while ACP mapping data is registered via backends.Register() in acp_register.go.
// Eventually, ai.RegisterBackend should be deprecated in favor of a unified
// backends.Register() that handles both CLI and ACP data in one place.
type BackendPlugin struct {
	// ID is the unique backend identifier, e.g. "claude", "kimi".
	// Corresponds to Agent.Backend field.
	ID string

	// Spec describes the backend's auto-discovery configuration.
	// Collected into model.BackendRegistry at startup.
	Spec model.BackendSpec

	// CLI is the CLI-mode configuration. nil means no CLI support.
	// Used by backends that ride the CLIBackend skeleton (most backends).
	CLI *CLIPlugin

	// Custom is the custom backend factory. nil means use standard CLI or ACP path.
	// Used by backends that directly implement AIBackend or wrap CLIBackend (e.g. Codex, VeCLI).
	// When Custom is non-nil, Factory uses Custom.NewBackend and ignores CLI.
	Custom *CustomPlugin

	// ACP is the ACP-mode mapping data. nil means no ACP support.
	// ACP event handling logic stays in internal/ai/ as shared infrastructure;
	// sub-packages only register mapping data (tool names, input field remaps).
	ACP *ACPPlugin

	// NeedsAutoResume when true wraps CLI mode with AutoResumeBackend.
	NeedsAutoResume bool
}

// CLIPlugin provides CLI-mode configuration for backends using the CLIBackend skeleton.
type CLIPlugin struct {
	// NewBackend returns a CLIBackend instance (configured with buildArgs/newParser/filterLine/preStart).
	NewBackend func() *ai.CLIBackend

	// ToolNameMap is the backend's complete tool name normalization map.
	// key: backend raw tool name → value: canonical name (e.g. "read_file" → "Read")
	ToolNameMap map[string]string

	// InputRemaps is the backend's CLI-mode tool input field remapping map.
	// key: original field name → value: target field name (e.g. "filePath" → "file_path")
	InputRemaps map[string]string

	// PreExecHook is called before the CLI subprocess starts, for backend-specific env injection.
	// e.g. Pi's API key injection logic. nil means no extra injection needed.
	PreExecHook func(cmd *exec.Cmd, req ai.ChatRequest)
}

// CustomPlugin provides a custom backend factory for backends not using the CLIBackend skeleton.
type CustomPlugin struct {
	// NewBackend returns a custom AIBackend instance.
	NewBackend func() ai.AIBackend
}

// ACPPlugin provides ACP-mode mapping data.
// ACP event handling stays in internal/ai/ as shared infrastructure.
// Sub-packages only register backend-specific mapping tables;
// shared event processing code queries these tables at runtime.
type ACPPlugin struct {
	// ToolCallIDPrefixes maps ACP toolCallID prefixes to canonical names for this backend.
	// e.g. Kimi: "read_file" → "Read"
	ToolCallIDPrefixes map[string]string

	// InputRemaps is the backend's ACP-mode tool input field remapping map.
	InputRemaps map[string]string
}
