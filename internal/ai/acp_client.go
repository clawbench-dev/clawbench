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
	"sync/atomic"

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
	poolEntry         *ACPConn                      // reference to pool entry for cache updates (deprecated alias)
	connRef           *ACPConn                      // reference to ACPConn for cache updates
	debouncers        map[string]*toolCallDebouncer // acpSessionID → debouncer

	// LoadSession replay buffer: during LoadSession, SessionUpdate messages
	// are collected here instead of being routed to SSE stream channels.
	loadSessionBuf   []acp.SessionNotification
	loadSessionBufMu sync.Mutex

	// Terminal sessions for ACP terminal/* methods (see acp_terminal.go)
	termMu    sync.Mutex
	terminals map[string]*terminalSession // terminalId → session
	termSeq   atomic.Int64                // auto-increment ID for terminal IDs
}

// NewClawBenchACPClient creates a new ACP client with session routing support.
func NewClawBenchACPClient() *ClawBenchACPClient {
	return &ClawBenchACPClient{
		sessionRoutes:     make(map[string]chan<- StreamEvent),
		pendingPermission: make(map[string]*pendingPermission),
		debouncers:        make(map[string]*toolCallDebouncer),
		terminals:         make(map[string]*terminalSession),
	}
}

// RegisterSession registers a StreamEvent channel for an ACP session.
// Events from this session will be forwarded to ch.
// Must be called before sending a Prompt for this session.
func (c *ClawBenchACPClient) RegisterSession(acpSessionID string, ch chan<- StreamEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionRoutes[acpSessionID] = ch
	c.debouncers[acpSessionID] = newToolCallDebouncer(ch, c.connRef)
}

// UnregisterSession removes the StreamEvent channel for an ACP session.
// Must be called after the Prompt for this session completes.
// Also cancels any pending permission requests for this session.
func (c *ClawBenchACPClient) UnregisterSession(acpSessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessionRoutes, acpSessionID)

	// Flush and remove debouncer
	if deb, ok := c.debouncers[acpSessionID]; ok {
		deb.flushAll()
		delete(c.debouncers, acpSessionID)
	}

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

// IsLoadSessionActive returns whether a LoadSession replay is in progress.
// Uses atomic load to avoid deadlocking with ACPConn.mu (see loadSessionActive docs).
func (c *ClawBenchACPClient) IsLoadSessionActive() bool {
	if c.connRef == nil {
		return false
	}
	return c.connRef.loadSessionActive.Load()
}

// GetAndClearLoadSessionBuf returns all collected SessionUpdate notifications
// from the LoadSession replay and clears the buffer.
func (c *ClawBenchACPClient) GetAndClearLoadSessionBuf() []acp.SessionNotification {
	c.loadSessionBufMu.Lock()
	buf := c.loadSessionBuf
	c.loadSessionBuf = nil
	c.loadSessionBufMu.Unlock()
	return buf
}

// SetLoadSessionBufForTest injects replay notifications for testing.
// Production code must not use this.
func (c *ClawBenchACPClient) SetLoadSessionBufForTest(buf []acp.SessionNotification) {
	c.loadSessionBufMu.Lock()
	c.loadSessionBuf = buf
	c.loadSessionBufMu.Unlock()
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

	// During LoadSession replay, collect messages in buffer instead of
	// routing to SSE stream channels. The load handler reads them after
	// the LoadSession RPC returns.
	if c.IsLoadSessionActive() {
		c.loadSessionBufMu.Lock()
		c.loadSessionBuf = append(c.loadSessionBuf, n)
		c.loadSessionBufMu.Unlock()
		return nil
	}

	c.mu.Lock()
	ch, ok := c.sessionRoutes[string(n.SessionId)]
	deb := c.debouncers[string(n.SessionId)]
	c.mu.Unlock()

	if !ok {
		// No active stream for this session — drop the update.
		// This can happen after a session is cancelled or the prompt completes.
		return nil
	}

	mapACPSessionUpdate(n.Update, ch, ctx, c.connRef, deb)
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
//
//nolint:gocyclo // RequestPermission has many branches (no-options / known-tool / default / cancelled / allowed) that are clearer inline than factored out
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
	toolName := extractToolName(title, kind, "")
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
	// Use a unique ID for the PermissionApproval card so the frontend
	// creates a separate block instead of merging with the original tool call.
	// ACP agents reuse the same toolCallId in RequestPermission (per protocol),
	// which would cause the frontend to merge the PermissionApproval into the
	// original tool_use block (e.g. ExitPlanMode) and never show the approval card.
	permissionBlockID := "perm_" + toolCallID
	approvalInput := map[string]any{
		"session_id":   sessionID,
		"toolCallId":   toolCallID,
		"permissionId": permissionBlockID,
		"toolName":     toolName,
		"toolInput":    toolInput,
		"options":      p.Options,
	}

	// Check autoApprove mode — if enabled, mark the event
	// and auto-select the first allow option instead of waiting for user.
	isAutoApprove := false
	if c.connRef != nil {
		isAutoApprove = c.connRef.IsAutoApprove()
	}
	if isAutoApprove {
		approvalInput["autoApproved"] = true
	}

	inputJSON, _ := json.Marshal(approvalInput)

	forwardACPEvent(ch, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{
			Name:  "PermissionApproval",
			ID:    permissionBlockID,
			Input: string(inputJSON),
			Done:  false,
		},
	})

	// Auto-approve branch: immediately select the first allow option
	if isAutoApprove {
		allowOptionID := ""
		for _, opt := range p.Options {
			if opt.Kind == acp.PermissionOptionKindAllowOnce || opt.Kind == acp.PermissionOptionKindAllowAlways {
				allowOptionID = string(opt.OptionId)
				break
			}
		}
		if allowOptionID != "" {
			slog.Info(
				"acp: auto-approving permission request",
				"session_id", sessionID,
				"tool_call_id", toolCallID,
				"tool_name", toolName,
				"option_id", allowOptionID,
			)
			// Remove from pending map — responding immediately
			c.mu.Lock()
			delete(c.pendingPermission, key)
			c.mu.Unlock()

			// Emit tool_result to mark the PermissionApproval as done
			forwardACPEvent(ch, StreamEvent{
				Type: "tool_result",
				Tool: &ToolCall{
					ID:     permissionBlockID,
					Done:   true,
					Status: "success",
					Output: "Auto-Approved",
				},
			})

			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(allowOptionID)),
			}, nil
		}
		// No allow option found — fall through to normal interactive flow
		slog.Warn(
			"acp: auto-approve mode but no allow option found, falling back to interactive",
			"session_id", sessionID,
			"tool_call_id", toolCallID,
		)
	}

	slog.Info(
		"acp: permission request pending user response",
		"session_id", sessionID,
		"tool_call_id", toolCallID,
		"tool_name", toolName,
	)

	// Notify frontend that this session has a pending approval
	if c.connRef != nil {
		c.connRef.mu.Lock()
		csid := c.connRef.clawbenchSID
		c.connRef.mu.Unlock()
		onPermissionStateChange(csid, true, toolName)
	}

	// Block until user responds or context is cancelled
	select {
	case resp := <-pp.Ch:
		c.mu.Lock()
		delete(c.pendingPermission, key)
		c.mu.Unlock()

		// Notify frontend that this session's pending approval was resolved
		if c.connRef != nil {
			c.connRef.mu.Lock()
			csid := c.connRef.clawbenchSID
			c.connRef.mu.Unlock()
			onPermissionStateChange(csid, false, "")
		}

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
				ID:     permissionBlockID,
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
