package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// --- PermissionKey tests ---

func TestPermissionKey(t *testing.T) {
	assert.Equal(t, "sess1:tc1", PermissionKey("sess1", "tc1"))
	assert.Equal(t, ":tc1", PermissionKey("", "tc1"))
	assert.Equal(t, "sess1:", PermissionKey("sess1", ""))
	assert.Equal(t, ":", PermissionKey("", ""))
}

// --- NewClawBenchACPClient tests ---

func TestNewClawBenchACPClient(t *testing.T) {
	c := NewClawBenchACPClient()
	require.NotNil(t, c)
	assert.NotNil(t, c.sessionRoutes)
	assert.NotNil(t, c.pendingPermission)
	assert.Nil(t, c.commands)
	assert.Nil(t, c.poolEntry)
}

// --- RegisterSession / UnregisterSession tests ---

func TestClawBenchACPClient_RegisterUnregisterSession(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan<- StreamEvent, 10)

	c.RegisterSession("sess-1", ch)

	c.mu.Lock()
	_, ok := c.sessionRoutes["sess-1"]
	c.mu.Unlock()
	assert.True(t, ok, "session should be registered")

	c.UnregisterSession("sess-1")

	c.mu.Lock()
	_, ok = c.sessionRoutes["sess-1"]
	c.mu.Unlock()
	assert.False(t, ok, "session should be unregistered")
}

func TestClawBenchACPClient_UnregisterSession_NoPanicOnMissing(t *testing.T) {
	c := NewClawBenchACPClient()
	// Unregistering a session that was never registered should not panic
	assert.NotPanics(t, func() {
		c.UnregisterSession("nonexistent")
	})
}

func TestClawBenchACPClient_RegisterSession_Overwrite(t *testing.T) {
	c := NewClawBenchACPClient()
	ch1 := make(chan<- StreamEvent, 10)
	ch2 := make(chan<- StreamEvent, 10)

	c.RegisterSession("sess-1", ch1)
	c.RegisterSession("sess-1", ch2) // overwrite

	c.mu.Lock()
	route := c.sessionRoutes["sess-1"]
	c.mu.Unlock()
	assert.Equal(t, ch2, route)
}

// --- SessionUpdate routing tests ---

func TestClawBenchACPClient_SessionUpdate_RoutesToCorrectSession(t *testing.T) {
	c := NewClawBenchACPClient()
	ch1 := make(chan StreamEvent, 10)
	ch2 := make(chan StreamEvent, 10)

	c.RegisterSession("sess-1", ch1)
	c.RegisterSession("sess-2", ch2)

	ctx := context.Background()
	notif := acp.SessionNotification{
		SessionId: acp.SessionId("sess-1"),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "hello sess-1"},
				},
			},
		},
	}

	err := c.SessionUpdate(ctx, notif)
	assert.NoError(t, err)

	// sess-1 should receive the event (thinking_done + content = 2 events)
	events := drainACPEvents(ch1, 2)
	assert.Equal(t, "thinking_done", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "hello sess-1", events[1].Content)

	// sess-2 should not receive anything
	assertNoMoreACPEvents(ch2, t)
}

func TestClawBenchACPClient_SessionUpdate_UnregisteredSession_DropsSilently(t *testing.T) {
	c := NewClawBenchACPClient()

	ctx := context.Background()
	notif := acp.SessionNotification{
		SessionId: acp.SessionId("unknown-sess"),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "dropped"},
				},
			},
		},
	}

	err := c.SessionUpdate(ctx, notif)
	assert.NoError(t, err) // no error, just silently dropped
}

func TestClawBenchACPClient_SessionUpdate_CachesCommands(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	ctx := context.Background()
	notif := acp.SessionNotification{
		SessionId: acp.SessionId("sess-1"),
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: []acp.AvailableCommand{
					{Name: "/compact", Description: "Compact history"},
					{Name: "/ask", Description: "Ask question"},
				},
			},
		},
	}

	err := c.SessionUpdate(ctx, notif)
	assert.NoError(t, err)

	// Commands should be cached
	cmds := c.GetCommands()
	require.Len(t, cmds, 2)
	assert.Equal(t, "/compact", cmds[0].Name)
	assert.Equal(t, "/ask", cmds[1].Name)
}

func TestClawBenchACPClient_SessionUpdate_UnregisteredSession_StillCachesCommands(t *testing.T) {
	c := NewClawBenchACPClient()

	ctx := context.Background()
	notif := acp.SessionNotification{
		SessionId: acp.SessionId("unknown-sess"),
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: []acp.AvailableCommand{
					{Name: "/compact", Description: "Compact history"},
				},
			},
		},
	}

	err := c.SessionUpdate(ctx, notif)
	assert.NoError(t, err)

	// Commands cached even for unregistered sessions
	cmds := c.GetCommands()
	require.Len(t, cmds, 1)
	assert.Equal(t, "/compact", cmds[0].Name)
}

// --- Command caching tests ---

func TestClawBenchACPClient_SetGetCommands(t *testing.T) {
	c := NewClawBenchACPClient()

	// Initially nil/empty
	assert.Nil(t, c.GetCommands())

	cmds := []acp.AvailableCommand{
		{Name: "/compact", Description: "Compact"},
		{Name: "/ask", Description: "Ask"},
	}
	c.SetCommands(cmds)

	result := c.GetCommands()
	require.Len(t, result, 2)
	assert.Equal(t, "/compact", result[0].Name)
	assert.Equal(t, "/ask", result[1].Name)
}

func TestClawBenchACPClient_SetCommands_OverwritesPrevious(t *testing.T) {
	c := NewClawBenchACPClient()

	c.SetCommands([]acp.AvailableCommand{{Name: "/old"}})
	c.SetCommands([]acp.AvailableCommand{{Name: "/new"}})

	cmds := c.GetCommands()
	require.Len(t, cmds, 1)
	assert.Equal(t, "/new", cmds[0].Name)
}

func TestClawBenchACPClient_SetCommands_NilPoolEntry_NoPanic(t *testing.T) {
	c := NewClawBenchACPClient()
	assert.Nil(t, c.poolEntry)

	// SetCommands with nil poolEntry should not panic (debouncePersistACPState not called)
	assert.NotPanics(t, func() {
		c.SetCommands([]acp.AvailableCommand{{Name: "/test"}})
	})
}

func TestClawBenchACPClient_GetCommandsAsInfo(t *testing.T) {
	c := NewClawBenchACPClient()

	c.SetCommands([]acp.AvailableCommand{
		{Name: "/compact", Description: "Compact history"},
		{Name: "/ask", Description: "Ask question", Input: &acp.AvailableCommandInput{
			Unstructured: &acp.UnstructuredCommandInput{Hint: "your question"},
		}},
	})

	info := c.GetCommandsAsInfo()
	require.Len(t, info, 2)
	assert.Equal(t, "/compact", info[0].Name)
	assert.Equal(t, "Compact history", info[0].Description)
	assert.Equal(t, "", info[0].InputHint)

	assert.Equal(t, "/ask", info[1].Name)
	assert.Equal(t, "Ask question", info[1].Description)
	assert.Equal(t, "your question", info[1].InputHint)
}

func TestClawBenchACPClient_GetCommandsAsInfo_Empty(t *testing.T) {
	c := NewClawBenchACPClient()
	info := c.GetCommandsAsInfo()
	assert.Empty(t, info)
}

// --- Permission request/respond flow tests ---

func TestClawBenchACPClient_RequestPermission_NoOptions_AutoCancel(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	ctx := context.Background()
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		Options:   []acp.PermissionOption{}, // empty options
	}

	resp, err := c.RequestPermission(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp.Outcome.Cancelled)
}

func TestClawBenchACPClient_RequestPermission_NoRoute_AutoCancel(t *testing.T) {
	c := NewClawBenchACPClient()
	// No session registered for "sess-1"

	ctx := context.Background()
	title := "Read"
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-1"),
			Title:      &title,
			Kind:       &[]acp.ToolKind{acp.ToolKindRead}[0],
		},
		Options: []acp.PermissionOption{
			{Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow-once"},
		},
	}

	resp, err := c.RequestPermission(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp.Outcome.Cancelled)
}

func TestClawBenchACPClient_RequestPermission_RespondApprove(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	title := "Read"
	kind := acp.ToolKindRead
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-1"),
			Title:      &title,
			Kind:       &kind,
		},
		Options: []acp.PermissionOption{
			{Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow-once"},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := c.RequestPermission(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp.Outcome.Selected)
		assert.Equal(t, acp.PermissionOptionId("allow-once"), resp.Outcome.Selected.OptionId)
	}()

	// Wait for the permission to be registered (tool_use event emitted)
	var toolUseEvent StreamEvent
	select {
	case toolUseEvent = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_use event")
	}
	assert.Equal(t, "tool_use", toolUseEvent.Type)
	require.NotNil(t, toolUseEvent.Tool)
	assert.Equal(t, "PermissionApproval", toolUseEvent.Tool.Name)
	assert.Equal(t, "perm_tc-1", toolUseEvent.Tool.ID)

	// Respond with approval
	key := PermissionKey("sess-1", "tc-1")
	ok := c.RespondPermission(key, "allow-once", false)
	assert.True(t, ok)

	// Wait for the goroutine to finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RequestPermission to complete")
	}

	// Should also emit tool_result
	select {
	case resultEvent := <-ch:
		assert.Equal(t, "tool_result", resultEvent.Type)
		require.NotNil(t, resultEvent.Tool)
		assert.Equal(t, "perm_tc-1", resultEvent.Tool.ID)
		assert.True(t, resultEvent.Tool.Done)
		assert.Equal(t, "success", resultEvent.Tool.Status)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_result event")
	}
}

func TestClawBenchACPClient_RequestPermission_RespondCancel(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	title := "Bash"
	kind := acp.ToolKindExecute
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-2"),
			Title:      &title,
			Kind:       &kind,
		},
		Options: []acp.PermissionOption{
			{Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce, OptionId: "deny"},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := c.RequestPermission(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp.Outcome.Cancelled)
	}()

	// Wait for tool_use event
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_use event")
	}

	// Respond with cancellation
	key := PermissionKey("sess-1", "tc-2")
	ok := c.RespondPermission(key, "", true)
	assert.True(t, ok)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RequestPermission to complete")
	}
}

func TestClawBenchACPClient_RequestPermission_ContextCancel(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	ctx, cancel := context.WithCancel(context.Background())

	title := "Edit"
	kind := acp.ToolKindEdit
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-3"),
			Title:      &title,
			Kind:       &kind,
		},
		Options: []acp.PermissionOption{
			{Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow"},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := c.RequestPermission(ctx, req)
		assert.Error(t, err) // context cancelled
		assert.NotNil(t, resp.Outcome.Cancelled)
	}()

	// Wait for tool_use event
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_use event")
	}

	// Cancel the context
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RequestPermission to complete after cancel")
	}
}

// --- RespondPermission edge cases ---

func TestClawBenchACPClient_RespondPermission_NoPendingRequest(t *testing.T) {
	c := NewClawBenchACPClient()
	ok := c.RespondPermission("nonexistent:key", "allow", false)
	assert.False(t, ok)
}

// --- UnregisterSession clears pending permissions ---

func TestClawBenchACPClient_UnregisterSession_ClearsPendingPermissions(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch)

	title := "Read"
	kind := acp.ToolKindRead
	req := acp.RequestPermissionRequest{
		SessionId: acp.SessionId("sess-1"),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-clear"),
			Title:      &title,
			Kind:       &kind,
		},
		Options: []acp.PermissionOption{
			{Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow"},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := c.RequestPermission(context.Background(), req)
		assert.NoError(t, err)
		// Should be cancelled because session was unregistered
		assert.NotNil(t, resp.Outcome.Cancelled)
	}()

	// Wait for tool_use event to confirm permission is pending
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_use event")
	}

	// Verify pending permission exists
	key := PermissionKey("sess-1", "tc-clear")
	c.mu.Lock()
	_, ok := c.pendingPermission[key]
	c.mu.Unlock()
	assert.True(t, ok, "pending permission should exist")

	// Unregister the session — should cancel pending permission
	c.UnregisterSession("sess-1")

	// Wait for RequestPermission to return
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RequestPermission to complete after unregister")
	}

	// Pending permission should be removed
	c.mu.Lock()
	_, ok = c.pendingPermission[key]
	c.mu.Unlock()
	assert.False(t, ok, "pending permission should be cleared after unregister")

	// Session route should also be gone
	c.mu.Lock()
	_, ok = c.sessionRoutes["sess-1"]
	c.mu.Unlock()
	assert.False(t, ok, "session route should be removed")
}

func TestClawBenchACPClient_UnregisterSession_OnlyCancelsMatchingSession(t *testing.T) {
	c := NewClawBenchACPClient()
	ch1 := make(chan StreamEvent, 10)
	ch2 := make(chan StreamEvent, 10)
	c.RegisterSession("sess-1", ch1)
	c.RegisterSession("sess-2", ch2)

	// Inject pending permissions for both sessions
	c.RegisterPendingPermissionForTest("sess-1:tc-1", &PendingPermissionForTest{
		SessionID:  "sess-1",
		ToolCallID: "tc-1",
	})
	c.RegisterPendingPermissionForTest("sess-2:tc-2", &PendingPermissionForTest{
		SessionID:  "sess-2",
		ToolCallID: "tc-2",
	})

	// Unregister only sess-1
	c.UnregisterSession("sess-1")

	// sess-1 pending should be cancelled, sess-2 should remain
	c.mu.Lock()
	_, ok1 := c.pendingPermission["sess-1:tc-1"]
	_, ok2 := c.pendingPermission["sess-2:tc-2"]
	c.mu.Unlock()
	assert.False(t, ok1, "sess-1 pending permission should be cleared")
	assert.True(t, ok2, "sess-2 pending permission should remain")
}

// --- RegisterPendingPermissionForTest test ---

func TestClawBenchACPClient_RegisterPendingPermissionForTest(t *testing.T) {
	c := NewClawBenchACPClient()
	c.RegisterPendingPermissionForTest("sess:tc", &PendingPermissionForTest{
		SessionID:  "sess",
		ToolCallID: "tc",
	})

	c.mu.Lock()
	pp, ok := c.pendingPermission["sess:tc"]
	c.mu.Unlock()
	assert.True(t, ok)
	assert.Equal(t, "sess", pp.SessionID)
	assert.Equal(t, "tc", pp.ToolCallID)
	assert.NotNil(t, pp.Ch) // channel should be initialized
}

// --- isPathAllowed tests ---

func TestIsPathAllowed_AbsolutePathUnderRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style absolute paths not valid on Windows")
	}
	// Save and restore RootPaths
	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	defer func() { model.RootPaths = origRoots }()

	err := isPathAllowed("/home/user/project/file.go")
	assert.NoError(t, err)
}

func TestIsPathAllowed_RelativePath(t *testing.T) {
	err := isPathAllowed("relative/path/file.go")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
}

func TestIsPathAllowed_NotUnderRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style absolute paths not valid on Windows")
	}
	// Save and restore RootPaths — use a specific root
	origRoots := model.RootPaths
	model.RootPaths = []string{"/home/user/project"}
	defer func() { model.RootPaths = origRoots }()

	err := isPathAllowed("/etc/passwd")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not under allowed roots")
}

func TestIsPathAllowed_UnderSpecificRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style absolute paths not valid on Windows")
	}
	origRoots := model.RootPaths
	model.RootPaths = []string{"/home/user/project"}
	defer func() { model.RootPaths = origRoots }()

	err := isPathAllowed("/home/user/project/src/main.go")
	assert.NoError(t, err)
}

func TestIsPathAllowed_EmptyPath(t *testing.T) {
	err := isPathAllowed("")
	assert.Error(t, err) // empty path is not absolute
}

func TestIsPathAllowed_RootPathItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style absolute paths not valid on Windows")
	}
	origRoots := model.RootPaths
	model.RootPaths = []string{"/home/user/project"}
	defer func() { model.RootPaths = origRoots }()

	err := isPathAllowed("/home/user/project")
	assert.NoError(t, err)
}

func TestIsPathAllowed_SymlinkEscapeAttempt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style path traversal not applicable on Windows")
	}
	origRoots := model.RootPaths
	model.RootPaths = []string{"/home/user/project"}
	defer func() { model.RootPaths = origRoots }()

	// Even with .. in path, filepath.IsAbs returns true for leading /
	// IsPathUnderAnyRoot should resolve and reject
	abs := filepath.Clean("/home/user/project/../../../etc/passwd")
	err := isPathAllowed(abs)
	assert.Error(t, err)
}

// --- ReadTextFile / WriteTextFile tests ---

func TestClawBenchACPClient_ReadTextFile_PathValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style path validation not applicable on Windows")
	}
	origRoots := model.RootPaths
	model.RootPaths = []string{"/tmp/acp-test-readonly"}
	defer func() { model.RootPaths = origRoots }()

	c := NewClawBenchACPClient()
	ctx := context.Background()

	// Relative path should be rejected
	_, err := c.ReadTextFile(ctx, acp.ReadTextFileRequest{Path: "relative/path"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")

	// Path outside roots should be rejected
	_, err = c.ReadTextFile(ctx, acp.ReadTextFileRequest{Path: "/etc/passwd"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not under allowed roots")
}

func TestClawBenchACPClient_WriteTextFile_PathValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style path validation not applicable on Windows")
	}
	origRoots := model.RootPaths
	model.RootPaths = []string{"/tmp/acp-test-writeonly"}
	defer func() { model.RootPaths = origRoots }()

	c := NewClawBenchACPClient()
	ctx := context.Background()

	// Relative path should be rejected
	_, err := c.WriteTextFile(ctx, acp.WriteTextFileRequest{Path: "relative/path", Content: "data"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")

	// Path outside roots should be rejected
	_, err = c.WriteTextFile(ctx, acp.WriteTextFileRequest{Path: "/etc/evil", Content: "data"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not under allowed roots")
}

// --- Terminal tests ---

func TestClawBenchACPClient_CreateTerminal_EchoCommand(t *testing.T) {
	c := NewClawBenchACPClient()
	resp, err := c.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		Command: "echo hello",
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.TerminalId)

	// Wait for command to complete
	exitResp, err := c.WaitForTerminalExit(context.Background(), acp.WaitForTerminalExitRequest{
		TerminalId: resp.TerminalId,
	})
	assert.NoError(t, err)
	assert.NotNil(t, exitResp.ExitCode)
	assert.Equal(t, 0, *exitResp.ExitCode)

	// Get output
	outResp, err := c.TerminalOutput(context.Background(), acp.TerminalOutputRequest{
		TerminalId: resp.TerminalId,
	})
	assert.NoError(t, err)
	assert.Contains(t, outResp.Output, "hello")
	assert.NotNil(t, outResp.ExitStatus)
	assert.NotNil(t, outResp.ExitStatus.ExitCode)
	assert.Equal(t, 0, *outResp.ExitStatus.ExitCode)
}

func TestClawBenchACPClient_CreateTerminal_FailingCommand(t *testing.T) {
	c := NewClawBenchACPClient()
	resp, err := c.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		Command: "exit 42",
	})
	assert.NoError(t, err)

	exitResp, err := c.WaitForTerminalExit(context.Background(), acp.WaitForTerminalExitRequest{
		TerminalId: resp.TerminalId,
	})
	assert.NoError(t, err)
	assert.NotNil(t, exitResp.ExitCode)
	assert.Equal(t, 42, *exitResp.ExitCode)
}

func TestClawBenchACPClient_CreateTerminal_OutputByteLimit(t *testing.T) {
	c := NewClawBenchACPClient()
	limit := 10
	resp, err := c.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		Command:         "echo abcdefghijklmnopqrstuvwxyz",
		OutputByteLimit: &limit,
	})
	assert.NoError(t, err)

	_, _ = c.WaitForTerminalExit(context.Background(), acp.WaitForTerminalExitRequest{
		TerminalId: resp.TerminalId,
	})

	outResp, err := c.TerminalOutput(context.Background(), acp.TerminalOutputRequest{
		TerminalId: resp.TerminalId,
	})
	assert.NoError(t, err)
	assert.LessOrEqual(t, len(outResp.Output), limit)
	assert.True(t, outResp.Truncated)
}

func TestClawBenchACPClient_TerminalOutput_NotFound(t *testing.T) {
	c := NewClawBenchACPClient()
	_, err := c.TerminalOutput(context.Background(), acp.TerminalOutputRequest{
		TerminalId: "nonexistent",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClawBenchACPClient_WaitForTerminalExit_NotFound(t *testing.T) {
	c := NewClawBenchACPClient()
	_, err := c.WaitForTerminalExit(context.Background(), acp.WaitForTerminalExitRequest{
		TerminalId: "nonexistent",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClawBenchACPClient_KillTerminal_NoError(t *testing.T) {
	c := NewClawBenchACPClient()
	_, err := c.KillTerminal(context.Background(), acp.KillTerminalRequest{
		TerminalId: "nonexistent",
	})
	assert.NoError(t, err)
}

func TestClawBenchACPClient_ReleaseTerminal_NoError(t *testing.T) {
	c := NewClawBenchACPClient()
	_, err := c.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
		TerminalId: "nonexistent",
	})
	assert.NoError(t, err)
}

// --- Auto-approve RequestPermission tests ---

func TestRequestPermission_AutoApprove(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{autoApprove: true}
	client.connRef = conn

	acpSessionID := "test-acp-sid-auto"
	client.RegisterSession(acpSessionID, ch)

	ctx := context.Background()
	allowOptID := "allow_once"
	title := "Bash"
	kind := acp.ToolKindExecute
	resp, err := client.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSessionID),
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tc-auto-1",
			Title:      &title,
			Kind:       &kind,
		},
		Options: []acp.PermissionOption{
			{OptionId: acp.PermissionOptionId(allowOptID), Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp.Outcome.Selected)
	assert.Equal(t, acp.PermissionOptionId(allowOptID), resp.Outcome.Selected.OptionId)

	// Should have emitted tool_use and tool_result events
	var toolUseFound, toolResultFound bool
	for range 2 {
		select {
		case evt := <-ch:
			if evt.Type == "tool_use" && evt.Tool.Name == "PermissionApproval" {
				toolUseFound = true
				var input map[string]any
				assert.NoError(t, json.Unmarshal([]byte(evt.Tool.Input), &input))
				assert.True(t, input["autoApproved"].(bool))
			}
			if evt.Type == "tool_result" {
				toolResultFound = true
				assert.Equal(t, "Auto-Approved", evt.Tool.Output)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for SSE event")
		}
	}
	assert.True(t, toolUseFound, "expected tool_use event for PermissionApproval")
	assert.True(t, toolResultFound, "expected tool_result event for PermissionApproval")
}

func TestRequestPermission_AutoApproveOff_Interactive(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{autoApprove: false}
	client.connRef = conn

	acpSessionID := "test-acp-sid-interactive"
	client.RegisterSession(acpSessionID, ch)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start RequestPermission in a goroutine — it should block waiting for user
	done := make(chan struct{})
	go func() {
		defer close(done)
		title := "Bash"
		kind := acp.ToolKindExecute
		resp, err := client.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: acp.SessionId(acpSessionID),
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: "tc-interactive-1",
				Title:      &title,
				Kind:       &kind,
			},
			Options: []acp.PermissionOption{
				{OptionId: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp.Outcome.Selected)
	}()

	// Read the tool_use event — should NOT have autoApproved
	select {
	case evt := <-ch:
		if evt.Type == "tool_use" && evt.Tool.Name == "PermissionApproval" {
			var input map[string]any
			assert.NoError(t, json.Unmarshal([]byte(evt.Tool.Input), &input))
			_, hasAutoApproved := input["autoApproved"]
			assert.False(t, hasAutoApproved, "autoApproved should not be set when autoApprove is off")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool_use event")
	}

	// Respond to unblock the goroutine
	client.RespondPermission(PermissionKey(acpSessionID, "tc-interactive-1"), "allow", false)
	<-done
}
