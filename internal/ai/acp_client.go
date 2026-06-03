package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// ClawBenchACPClient implements the acp.Client interface to handle
// callbacks from ACP agents. It converts ACP session updates to
// ClawBench StreamEvents and forwards them via session routing.
//
// With connection pooling, a single ClawBenchACPClient is shared across
// all sessions on a connection. It uses sessionRoutes to demultiplex
// SessionUpdate notifications to the correct StreamEvent channel.
type ClawBenchACPClient struct {
	mu            sync.Mutex
	sessionRoutes map[string]chan<- StreamEvent // acpSessionID → streamCh
	commands      []acp.AvailableCommand        // cached from available_commands_update
}

// NewClawBenchACPClient creates a new ACP client with session routing support.
func NewClawBenchACPClient() *ClawBenchACPClient {
	return &ClawBenchACPClient{
		sessionRoutes: make(map[string]chan<- StreamEvent),
	}
}

// RegisterSession registers a StreamEvent channel for an ACP session.
// Events from this session will be forwarded to ch.
// Must be called before sending a Prompt for this session.
func (c *ClawBenchACPClient) RegisterSession(acpSessionID string, ch chan<- StreamEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionRoutes[acpSessionID] = ch
}

// UnregisterSession removes the StreamEvent channel for an ACP session.
// Must be called after the Prompt for this session completes.
func (c *ClawBenchACPClient) UnregisterSession(acpSessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessionRoutes, acpSessionID)
}

// GetCommands returns the cached available commands from the last session/new.
func (c *ClawBenchACPClient) GetCommands() []acp.AvailableCommand {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.commands
}

// SetCommands caches available commands from an ACP session update.
func (c *ClawBenchACPClient) SetCommands(cmds []acp.AvailableCommand) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands = cmds
}

// SessionUpdate converts ACP session update notifications to StreamEvents.
// Called by the SDK's internal goroutine from Connection.receive().
// It routes the update to the correct StreamEvent channel based on the
// ACP session ID. If no route is registered (session unregistered or
// cancelled), the update is silently dropped.
func (c *ClawBenchACPClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	// Cache available commands from the update (before route lookup)
	if n.Update.AvailableCommandsUpdate != nil {
		c.mu.Lock()
		c.commands = n.Update.AvailableCommandsUpdate.AvailableCommands
		c.mu.Unlock()
	}

	c.mu.Lock()
	ch, ok := c.sessionRoutes[string(n.SessionId)]
	c.mu.Unlock()

	if !ok {
		// No active stream for this session — drop the update.
		// This can happen after a session is cancelled or the prompt completes.
		return nil
	}

	mapACPSessionUpdate(n.Update, ch, ctx)
	return nil
}

// RequestPermission auto-approves the first permission option (current CLI behavior).
func (c *ClawBenchACPClient) RequestPermission(_ context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if len(p.Options) == 0 {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}, nil
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[0].OptionId),
	}, nil
}

// isPathAllowed checks that the given path is absolute and under an allowed root.
// This prevents ACP agents from accessing sensitive files outside the workspace
// (e.g., ~/.clawbench/auto-password, /etc/passwd).
func isPathAllowed(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	if !platform.IsPathUnderAnyRoot(path, model.RootPaths) {
		return fmt.Errorf("path not under allowed roots: %s", path)
	}
	return nil
}

// ReadTextFile delegates file reads to the OS filesystem with path validation.
func (c *ClawBenchACPClient) ReadTextFile(_ context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if err := isPathAllowed(p.Path); err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	content := string(b)
	if p.Line != nil || p.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if p.Line != nil && *p.Line > 0 {
			start = *p.Line - 1
			if start > len(lines) {
				start = len(lines)
			}
		}
		end := len(lines)
		if p.Limit != nil && *p.Limit > 0 && start+*p.Limit < end {
			end = start + *p.Limit
		}
		content = strings.Join(lines[start:end], "\n")
	}
	return acp.ReadTextFileResponse{Content: content}, nil
}

// WriteTextFile delegates file writes to the OS filesystem with path validation.
func (c *ClawBenchACPClient) WriteTextFile(_ context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := isPathAllowed(p.Path); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if dir := filepath.Dir(p.Path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return acp.WriteTextFileResponse{}, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return acp.WriteTextFileResponse{}, os.WriteFile(p.Path, []byte(p.Content), 0o644)
}

// CreateTerminal creates a terminal session (stub — returns error indicating not supported).
func (c *ClawBenchACPClient) CreateTerminal(_ context.Context, _ acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, fmt.Errorf("terminal not supported by clawbench ACP client")
}

// KillTerminal kills a terminal session (stub).
func (c *ClawBenchACPClient) KillTerminal(_ context.Context, _ acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, nil
}

// TerminalOutput returns terminal output (stub).
func (c *ClawBenchACPClient) TerminalOutput(_ context.Context, _ acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, fmt.Errorf("terminal not supported by clawbench ACP client")
}

// ReleaseTerminal releases terminal resources (stub).
func (c *ClawBenchACPClient) ReleaseTerminal(_ context.Context, _ acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit waits for terminal exit (stub).
func (c *ClawBenchACPClient) WaitForTerminalExit(_ context.Context, _ acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not supported by clawbench ACP client")
}
