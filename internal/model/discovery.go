package model

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// BackendSpec defines a known AI backend for auto-discovery.
type BackendSpec struct {
	ID                   string   // agent id, e.g. "claude"
	Backend              string   // backend type, e.g. "claude"
	DefaultCmd           string   // command to detect on PATH, e.g. "claude"
	AltCmd               string   // fallback CLI command (e.g. "deepseek" when primary is "codewhale"); used for detection if DefaultCmd not found
	NoCLI                bool     // if true, this backend has no CLI (e.g. mock); always considered "present"
	Name                 string   // display name, e.g. "Claude"
	Specialty            string   // short description, e.g. "代码编写与推理"
	ThinkingEffortLevels []string // supported thinking effort levels, e.g. ["low","medium","high"]; nil = not supported
	AcpCommand           string   // ACP spawn command for acp-stdio transport, e.g. "kimi --acp"; empty = no ACP support
	ACPLoadSession       bool     // whether the ACP agent truly supports LoadSession (overrides ACP Initialize report)
	InstallCmd           string   // npm/pip install command, e.g. "npm install -g @anthropic-ai/claude-code"; empty = not installable
	SortOrder            int      // display/registration order for deterministic BackendRegistry ordering
}

// LoadBackendSpecs is set by the backends package at init time to provide
// BackendSpec entries from all registered backend plugins. Uses function-variable
// injection to avoid import cycles (model cannot import backends).
var LoadBackendSpecs func() []BackendSpec

// BackendSupportsCLIFn is set by the backends package at init time to report
// whether a backend has a CLI implementation (a registered CLI backend factory).
// Uses function-variable injection to avoid import cycles (model cannot import ai).
var BackendSupportsCLIFn func(backendID string) bool

// BackendSupportsCLI reports whether the given backend has a CLI implementation.
// Falls back to false when the function variable is not wired (e.g. isolated tests).
func BackendSupportsCLI(backendID string) bool {
	if BackendSupportsCLIFn == nil {
		return false
	}
	return BackendSupportsCLIFn(backendID)
}

// BackendSupportsMidTurnFn is set by the backends package at init time to report
// whether a backend can inject a message into an ALREADY RUNNING turn (rather
// than queueing it for the next one). Uses function-variable injection to avoid
// an import cycle (model cannot import ai).
var BackendSupportsMidTurnFn func(backendID string) bool

// BackendSupportsMidTurn reports whether the given backend can join a running
// turn. Falls back to false when the function variable is not wired (isolated
// tests, or a build without the backends package) — callers then use the
// "interrupt and send" affordance instead.
func BackendSupportsMidTurn(backendID string) bool {
	if BackendSupportsMidTurnFn == nil {
		return false
	}
	return BackendSupportsMidTurnFn(backendID)
}

// BackendRegistry lists all known AI backends for auto-discovery.
// Populated lazily from backend plugins via GetBackendRegistry().
// Direct reads should use GetBackendRegistry() to ensure initialization.
var BackendRegistry []BackendSpec

var backendRegistryOnce sync.Once

// GetBackendRegistry returns the populated BackendRegistry, initializing it
// lazily on first call from backend plugin specs. This ensures all backend
// sub-package init()s have completed before the registry is built.
func GetBackendRegistry() []BackendSpec {
	backendRegistryOnce.Do(func() {
		if LoadBackendSpecs != nil {
			BackendRegistry = LoadBackendSpecs()
		}
	})
	return BackendRegistry
}

// CheckCLIExists checks whether a CLI command is available on the system.
// It first tries `cmd --version` with a 5-second timeout.
// If that fails, it falls back to exec.LookPath — some CLIs (especially Node.js ones)
// may return non-zero exit codes for --version when run without a TTY or in certain
// environments, but the binary itself is still present and functional.
func CheckCLIExists(cmd string) bool {
	if cmd == "" {
		return false
	}

	// Primary check: run `cmd --version`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return true
	}

	// Fallback: check if the binary exists on PATH
	// This handles cases where --version fails (non-zero exit, timeout, etc.)
	// but the CLI is actually installed and usable for its primary function.
	if _, lookupErr := exec.LookPath(cmd); lookupErr == nil {
		slog.Warn("CLI --version failed but binary found on PATH, keeping agent",
			"cmd", cmd, "version_error", err)
		return true
	}

	slog.Warn("CLI not found on PATH",
		"cmd", cmd, "version_error", err)
	return false
}

// CheckCLIExistsErr returns an error describing why the CLI is not available,
// or nil if the CLI is available. This is used for more specific error reporting.
// It is a variable so it can be overridden in tests.
var CheckCLIExistsErr = checkCLIExistsErr

func checkCLIExistsErr(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return nil
	}

	_, lookupErr := exec.LookPath(cmd)
	if lookupErr == nil {
		// Binary exists but --version failed — CLI is still available
		return nil
	}

	return fmt.Errorf("CLI %q not found on PATH: %w", cmd, lookupErr)
}

// FindSpecByBackend returns the BackendSpec for the given backend type, or nil.
func FindSpecByBackend(backend string) *BackendSpec {
	registry := GetBackendRegistry()
	for i := range registry {
		if registry[i].Backend == backend {
			return &registry[i]
		}
	}
	return nil
}

// FindBackendSpecByDefaultCmd returns the BackendSpec whose DefaultCmd matches, or nil.
func FindBackendSpecByDefaultCmd(cmd string) *BackendSpec {
	registry := GetBackendRegistry()
	for i := range registry {
		if registry[i].DefaultCmd == cmd {
			return &registry[i]
		}
	}
	return nil
}

// CanDiscoverModels returns true if the spec supports model discovery via the registry.
func CanDiscoverModels(spec BackendSpec) bool {
	return HasModelSource(spec.Backend)
}

// Backends that support model refresh, for diagnostics and the settings UI.
// Prefer HasModelSource for a single backend.
func BackendsWithModelDiscovery() []string {
	return RegisteredModelSources()
}
