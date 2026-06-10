package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// mockACPAgent implements acp.Agent for E2E testing.
// It simulates a real ACP agent that provides slash commands, modes, and config options.
type mockACPAgent struct {
	conn     *acp.AgentSideConnection
	sessions map[string]*mockSession
	mu       sync.Mutex
}

type mockSession struct {
	cancel         context.CancelFunc
	mode           string
	thinkingEffort string
}

// Mock slash commands (similar to what CodeBuddy provides)
var mockCommands = []acp.AvailableCommand{
	{Name: "commit", Description: "Create a git commit", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "commit message"}}},
	{Name: "help", Description: "Show available commands and usage"},
	{Name: "review", Description: "Review code for issues", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "file or description"}}},
	{Name: "test", Description: "Run tests", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "test pattern"}}},
	{Name: "plan", Description: "Create an implementation plan", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "feature description"}}},
	{Name: "fix", Description: "Fix a bug or issue", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "bug description"}}},
	{Name: "search", Description: "Search the codebase", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "search query"}}},
	{Name: "doc", Description: "Generate documentation", Input: &acp.AvailableCommandInput{Unstructured: &acp.UnstructuredCommandInput{Hint: "topic"}}},
}

// Mode constants
const (
	modeCode       = "code"
	modePlan       = "plan"
	modeBypass     = "bypass-permissions"
	modeCodeName   = "Code"
	modePlanName   = "Plan"
	modeBypassName = "Bypass Permissions"
)

// Thinking effort constants
const (
	effortLow        = "low"
	effortMedium     = "medium"
	effortHigh       = "high"
	effortLowName    = "Low"
	effortMediumName = "Medium"
	effortHighName   = "High"
)

// Config option types (ACP protocol field).
const configOptionTypeSelect = "select"

func (a *mockACPAgent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
		},
	}, nil
}

func (a *mockACPAgent) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *mockACPAgent) Logout(ctx context.Context, params acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}

func (a *mockACPAgent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := randomID()
	a.mu.Lock()
	a.sessions[sid] = &mockSession{mode: modeBypass, thinkingEffort: effortMedium}
	a.mu.Unlock()

	modeCategory := acp.SessionConfigOptionCategoryMode
	thoughtLevelCategory := acp.SessionConfigOptionCategoryThoughtLevel

	return acp.NewSessionResponse{
		SessionId: acp.SessionId(sid),
		Modes: &acp.SessionModeState{
			AvailableModes: []acp.SessionMode{
				{Id: acp.SessionModeId(modeCode), Name: modeCodeName},
				{Id: acp.SessionModeId(modePlan), Name: modePlanName},
				{Id: acp.SessionModeId(modeBypass), Name: modeBypassName},
			},
			CurrentModeId: acp.SessionModeId(modeBypass),
		},
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("mode"),
					Name:         "Mode",
					Type:         configOptionTypeSelect,
					Category:     &modeCategory,
					CurrentValue: acp.SessionConfigValueId(modeBypass),
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							acp.SessionConfigSelectOption{Name: modeCodeName, Value: acp.SessionConfigValueId(modeCode)},
							acp.SessionConfigSelectOption{Name: modePlanName, Value: acp.SessionConfigValueId(modePlan)},
							acp.SessionConfigSelectOption{Name: modeBypassName, Value: acp.SessionConfigValueId(modeBypass)},
						},
					},
				},
			},
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("thinkingEffort"),
					Name:         "Thinking Effort",
					Type:         configOptionTypeSelect,
					Category:     &thoughtLevelCategory,
					CurrentValue: acp.SessionConfigValueId(effortMedium),
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							acp.SessionConfigSelectOption{Name: effortLowName, Value: acp.SessionConfigValueId(effortLow)},
							acp.SessionConfigSelectOption{Name: effortMediumName, Value: acp.SessionConfigValueId(effortMedium)},
							acp.SessionConfigSelectOption{Name: effortHighName, Value: acp.SessionConfigValueId(effortHigh)},
						},
					},
				},
			},
		},
	}, nil
}

func (a *mockACPAgent) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}

func (a *mockACPAgent) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}

func (a *mockACPAgent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	sid := string(params.SessionId)
	a.mu.Lock()
	s, ok := a.sessions[sid]
	if !ok || s == nil {
		// Session not found — treat as new session with the given ID
		s = &mockSession{mode: modeBypass, thinkingEffort: effortMedium}
		a.sessions[sid] = s
	}
	a.mu.Unlock()

	modeCategory := acp.SessionConfigOptionCategoryMode
	thoughtLevelCategory := acp.SessionConfigOptionCategoryThoughtLevel

	return acp.ResumeSessionResponse{
		Modes: &acp.SessionModeState{
			AvailableModes: []acp.SessionMode{
				{Id: acp.SessionModeId(modeCode), Name: modeCodeName},
				{Id: acp.SessionModeId(modePlan), Name: modePlanName},
				{Id: acp.SessionModeId(modeBypass), Name: modeBypassName},
			},
			CurrentModeId: acp.SessionModeId(s.mode),
		},
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("mode"),
					Name:         "Mode",
					Type:         configOptionTypeSelect,
					Category:     &modeCategory,
					CurrentValue: acp.SessionConfigValueId(s.mode),
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							acp.SessionConfigSelectOption{Name: modeCodeName, Value: acp.SessionConfigValueId(modeCode)},
							acp.SessionConfigSelectOption{Name: modePlanName, Value: acp.SessionConfigValueId(modePlan)},
							acp.SessionConfigSelectOption{Name: modeBypassName, Value: acp.SessionConfigValueId(modeBypass)},
						},
					},
				},
			},
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("thinkingEffort"),
					Name:         "Thinking Effort",
					Type:         configOptionTypeSelect,
					Category:     &thoughtLevelCategory,
					CurrentValue: acp.SessionConfigValueId(s.thinkingEffort),
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							acp.SessionConfigSelectOption{Name: effortLowName, Value: acp.SessionConfigValueId(effortLow)},
							acp.SessionConfigSelectOption{Name: effortMediumName, Value: acp.SessionConfigValueId(effortMedium)},
							acp.SessionConfigSelectOption{Name: effortHighName, Value: acp.SessionConfigValueId(effortHigh)},
						},
					},
				},
			},
		},
	}, nil
}

func (a *mockACPAgent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	a.mu.Lock()
	s, ok := a.sessions[string(params.SessionId)]
	if ok && s != nil {
		s.mode = string(params.ModeId)
	}
	a.mu.Unlock()
	return acp.SetSessionModeResponse{}, nil
}

func (a *mockACPAgent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	// params is a union: .ValueId (select) or .Boolean
	if params.ValueId != nil {
		a.mu.Lock()
		s, ok := a.sessions[string(params.ValueId.SessionId)]
		if ok && s != nil {
			switch string(params.ValueId.ConfigId) {
			case "mode":
				s.mode = string(params.ValueId.Value)
			case "thinkingEffort":
				s.thinkingEffort = string(params.ValueId.Value)
			}
		}
		a.mu.Unlock()
	}
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (a *mockACPAgent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	a.mu.Lock()
	s, ok := a.sessions[string(params.SessionId)]
	a.mu.Unlock()
	if ok && s != nil && s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (a *mockACPAgent) Prompt(_ context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)
	a.mu.Lock()
	s, ok := a.sessions[sid]
	a.mu.Unlock()
	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sid)
	}

	// Cancel any previous turn
	a.mu.Lock()
	if s.cancel != nil {
		prev := s.cancel
		a.mu.Unlock()
		prev()
	} else {
		a.mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	s.cancel = cancel
	a.mu.Unlock()

	mode := ""
	a.mu.Lock()
	session, ok := a.sessions[sid]
	if ok && session != nil {
		mode = session.mode
	}
	a.mu.Unlock()

	if err := a.simulateTurn(ctx, sid, params, mode); err != nil {
		if ctx.Err() != nil {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil //nolint:nilerr // context cancellation is not an error, return cancelled response
		}
		return acp.PromptResponse{}, err
	}

	a.mu.Lock()
	s.cancel = nil
	a.mu.Unlock()

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// simulateTurn sends a realistic sequence of ACP session notifications,
// including available_commands_update, thinking, tool calls, and message chunks.
func (a *mockACPAgent) simulateTurn(ctx context.Context, sid string, params acp.PromptRequest, currentMode string) error { //nolint:gocyclo // mock agent simulates many ACP protocol steps
	// 1. Send available_commands_update at the start of each turn
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: mockCommands,
			},
		},
	}); err != nil {
		return err
	}
	if err := pause(ctx, 50*time.Millisecond); err != nil {
		return err
	}

	// 1b. Send plan update (simulating the agent's execution plan)
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.UpdatePlan(
			acp.PlanEntry{Content: "Analyze the request", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
			acp.PlanEntry{Content: "Generate response", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
			acp.PlanEntry{Content: "Verify output", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusPending},
		),
	}); err != nil {
		return err
	}
	if err := pause(ctx, 50*time.Millisecond); err != nil {
		return err
	}

	// 2. Send thinking block (simulating the agent thinking)
	userText := extractUserText(params.Prompt)
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    acp.UpdateAgentThoughtText(fmt.Sprintf("Processing user request: %s", truncate(userText, 80))),
	}); err != nil {
		return err
	}
	if err := pause(ctx, 100*time.Millisecond); err != nil {
		return err
	}

	// 3. Send tool_call (simulating reading a file)
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.StartToolCall(
			acp.ToolCallId("call_read_1"),
			"Reading project files",
			acp.WithStartKind(acp.ToolKindRead),
			acp.WithStartStatus(acp.ToolCallStatusPending),
			acp.WithStartLocations([]acp.ToolCallLocation{{Path: "/project/README.md"}}),
			acp.WithStartRawInput(map[string]any{"path": "/project/README.md"}),
		),
	}); err != nil {
		return err
	}
	if err := pause(ctx, 100*time.Millisecond); err != nil {
		return err
	}

	// 4. Tool call completed
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.UpdateToolCall(
			acp.ToolCallId("call_read_1"),
			acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
			acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock("# Mock Project\n\nThis is a sample project for E2E testing."))}),
		),
	}); err != nil {
		return err
	}
	if err := pause(ctx, 50*time.Millisecond); err != nil {
		return err
	}

	// 4b. Request permission for a write operation (simulating agent asking for approval)
	// Skip permission request in bypass-permissions mode — the host would auto-approve anyway
	if currentMode != modeBypass {
		writeTitle := "Writing to main.go"
		writeKind := acp.ToolKindEdit
		permResp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: acp.SessionId(sid),
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: acp.ToolCallId("call_write_perm_1"),
				Title:      &writeTitle,
				Kind:       &writeKind,
				RawInput:   map[string]any{"file_path": "/project/main.go", "content": "package main\nfunc main() {}"},
				Status:     nil,
			},
			Options: []acp.PermissionOption{
				{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow Once", OptionId: "allow_once"},
				{Kind: acp.PermissionOptionKindAllowAlways, Name: "Allow Always", OptionId: "allow_always"},
				{Kind: acp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: "reject_once"},
			},
		})
		if err != nil {
			// Non-fatal: permission request may fail if client doesn't support it
			slog.Warn("acp-mock: request_permission failed (non-fatal)", "error", err)
		} else if permResp.Outcome.Selected != nil {
			slog.Info("acp-mock: permission granted", "option_id", permResp.Outcome.Selected.OptionId)
		} else {
			slog.Info("acp-mock: permission cancelled")
		}
		if err := pause(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}

	// 5. Send the main response text word-by-word
	response := "Hello! I am a mock ACP agent for E2E testing. I received your message and processed it successfully."
	words := strings.Fields(response)
	for i, word := range words {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sep := " "
		if i == 0 {
			sep = ""
		}
		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: acp.SessionId(sid),
			Update:    acp.UpdateAgentMessageText(sep + word),
		}); err != nil {
			return err
		}
		if err := pause(ctx, 30*time.Millisecond); err != nil {
			return err
		}
	}

	return nil
}

func randomID() string {
	var b [12]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	return "sess_" + hex.EncodeToString(b[:])
}

func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// extractUserText extracts the text content from prompt ContentBlocks.
func extractUserText(blocks []acp.ContentBlock) string {
	var texts []string
	for _, block := range blocks {
		if block.Text != nil {
			texts = append(texts, block.Text.Text)
		}
	}
	return strings.Join(texts, " ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	ag := &mockACPAgent{sessions: make(map[string]*mockSession)}
	asc := acp.NewAgentSideConnection(ag, os.Stdout, os.Stdin)
	asc.SetLogger(slog.Default())
	ag.conn = asc // Wire up the connection for SessionUpdate calls

	slog.Info("acp-mock: agent started, waiting for connection on stdin/stdout")

	// Block until the peer disconnects or context is cancelled
	select {
	case <-asc.Done():
	case <-ctx.Done():
	}

	slog.Info("acp-mock: agent shutting down")
}
