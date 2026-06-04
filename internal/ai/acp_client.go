package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// pendingPermission tracks an in-flight permission request that is
// waiting for the user's response via the HTTP API.
type pendingPermission struct {
	SessionID  string
	ToolCallID string
	ToolName   string
	ToolInput  string // JSON-encoded raw input
	Options    []acp.PermissionOption
	Ch         chan acp.RequestPermissionResponse
}

// ClawBenchACPClient implements the acp.Client interface to handle
// callbacks from ACP agents. It converts ACP session updates to
// ClawBench StreamEvents and forwards them via session routing.
//
// With connection pooling, a single ClawBenchACPClient is shared across
// all sessions on a connection. It uses sessionRoutes to demultiplex
// SessionUpdate notifications to the correct StreamEvent channel.
type ClawBenchACPClient struct {
	mu                sync.Mutex
	sessionRoutes     map[string]chan<- StreamEvent // acpSessionID → streamCh
	commands          []acp.AvailableCommand        // cached from available_commands_update
	pendingPermission map[string]*pendingPermission // PermissionKey → pending request
	poolEntry         *ACPConnEntry                 // reference to pool entry for cache updates
}

// NewClawBenchACPClient creates a new ACP client with session routing support.
func NewClawBenchACPClient() *ClawBenchACPClient {
	return &ClawBenchACPClient{
		sessionRoutes:     make(map[string]chan<- StreamEvent),
		pendingPermission: make(map[string]*pendingPermission),
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
// Also cancels any pending permission requests for this session.
func (c *ClawBenchACPClient) UnregisterSession(acpSessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessionRoutes, acpSessionID)

	// Cancel any pending permission requests for this session
	for key, pp := range c.pendingPermission {
		if pp.SessionID == acpSessionID {
			pp.Ch <- acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeCancelled(),
			}
			delete(c.pendingPermission, key)
		}
	}
}

// GetCommands returns the cached available commands from the last session/new.
func (c *ClawBenchACPClient) GetCommands() []acp.AvailableCommand {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.commands
}

// GetCommandsAsInfo returns cached commands as AvailableCommandInfo slices
// for JSON serialization to the frontend.
func (c *ClawBenchACPClient) GetCommandsAsInfo() []AvailableCommandInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	cmds := make([]AvailableCommandInfo, 0, len(c.commands))
	for _, c := range c.commands {
		info := AvailableCommandInfo{
			Name:        c.Name,
			Description: c.Description,
		}
		if c.Input != nil && c.Input.Unstructured != nil {
			info.InputHint = c.Input.Unstructured.Hint
		}
		cmds = append(cmds, info)
	}
	return cmds
}

// SetCommands caches available commands from an ACP session update.
func (c *ClawBenchACPClient) SetCommands(cmds []acp.AvailableCommand) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands = cmds
	// Trigger persist so commands are saved to DB
	if c.poolEntry != nil {
		c.poolEntry.debouncePersistACPState()
	}
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

	mapACPSessionUpdate(n.Update, ch, ctx, c.poolEntry)
	return nil
}

// PermissionKey returns the map key for a pending permission request.
// Exported so the handler layer can construct the key from URL parameters.
func PermissionKey(sessionID, toolCallID string) string {
	return sessionID + ":" + toolCallID
}

// RequestPermission blocks until the user responds to a permission request
// via the HTTP API, or the context is cancelled (session cancelled/disconnected).
// The ACP SDK dispatches inbound requests on dedicated goroutines, so blocking
// here is safe — it won't deadlock the transport.
func (c *ClawBenchACPClient) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if len(p.Options) == 0 {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}, nil
	}

	toolCallID := string(p.ToolCall.ToolCallId)
	sessionID := string(p.SessionId)
	key := PermissionKey(sessionID, toolCallID)

	// Extract tool info for the frontend card
	var title string
	if p.ToolCall.Title != nil {
		title = *p.ToolCall.Title
	}
	var kind acp.ToolKind
	if p.ToolCall.Kind != nil {
		kind = *p.ToolCall.Kind
	}
	toolName := extractToolName(title, kind)
	var toolInput string
	if p.ToolCall.RawInput != nil {
		if b, err := json.Marshal(p.ToolCall.RawInput); err == nil {
			toolInput = string(b)
		}
	}

	pp := &pendingPermission{
		SessionID:  sessionID,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		ToolInput:  toolInput,
		Options:    p.Options,
		Ch:         make(chan acp.RequestPermissionResponse, 1),
	}

	// Register the pending permission
	c.mu.Lock()
	c.pendingPermission[key] = pp
	// Get the stream channel to emit the tool_use event
	ch, ok := c.sessionRoutes[sessionID]
	c.mu.Unlock()

	if !ok {
		// No active stream — auto-cancel
		c.mu.Lock()
		delete(c.pendingPermission, key)
		c.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}, nil
	}

	// Emit a tool_use event for the PermissionApproval card in the AI message
	approvalInput := map[string]any{
		"session_id": sessionID,
		"toolCallId": toolCallID,
		"toolName":   toolName,
		"toolInput":  toolInput,
		"options":    p.Options,
	}
	inputJSON, _ := json.Marshal(approvalInput)

	forwardACPEvent(ch, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{
			Name:  "PermissionApproval",
			ID:    toolCallID,
			Input: string(inputJSON),
			Done:  false,
		},
	})

	slog.Info(
		"acp: permission request pending user response",
		"session_id", sessionID,
		"tool_call_id", toolCallID,
		"tool_name", toolName,
	)

	// Block until user responds or context is cancelled
	select {
	case resp := <-pp.Ch:
		c.mu.Lock()
		delete(c.pendingPermission, key)
		c.mu.Unlock()

		// Emit tool_result to mark the PermissionApproval as done
		resultStatus := "success"
		resultOutput := "Approved"
		if resp.Outcome.Cancelled != nil {
			resultStatus = "error"
			resultOutput = "Cancelled"
		}
		forwardACPEvent(ch, StreamEvent{
			Type: "tool_result",
			Tool: &ToolCall{
				ID:     toolCallID,
				Done:   true,
				Status: resultStatus,
				Output: resultOutput,
			},
		})

		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pendingPermission, key)
		c.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}, ctx.Err()
	}
}

// RegisterPendingPermissionForTest injects a pending permission entry for testing.
// Production code must not use this.
func (c *ClawBenchACPClient) RegisterPendingPermissionForTest(key string, pp *PendingPermissionForTest) {
	c.mu.Lock()
	c.pendingPermission[key] = &pendingPermission{
		SessionID:  pp.SessionID,
		ToolCallID: pp.ToolCallID,
		Ch:         make(chan acp.RequestPermissionResponse, 1),
	}
	c.mu.Unlock()
}

// PendingPermissionForTest is the test-visible version of pendingPermission.
type PendingPermissionForTest struct {
	SessionID  string
	ToolCallID string
}

// RespondPermission delivers a user's response to a pending permission request.
// Called by the HTTP handler when the frontend submits the user's choice.
// Returns false if no pending request was found for this key.
func (c *ClawBenchACPClient) RespondPermission(key string, optionID string, cancelled bool) bool {
	c.mu.Lock()
	pp, ok := c.pendingPermission[key]
	if !ok {
		c.mu.Unlock()
		return false
	}
	delete(c.pendingPermission, key)
	c.mu.Unlock()

	if cancelled {
		pp.Ch <- acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}
	} else {
		pp.Ch <- acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(optionID)),
		}
	}
	return true
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
