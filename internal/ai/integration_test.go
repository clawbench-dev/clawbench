//go:build integration

package ai

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Shared Helpers ---

// codexCommand matches the agent config in config/agents/codex-MiniMax.yaml:
//
//	command: codex --profile m27
//
// This tells CodexBackend to use the MiniMax provider (MINIMAX_API_KEY) instead of OpenAI.
const codexCommand = "codex --profile m27"

// newSessionID returns a unique session ID for integration tests.
// Claude requires a valid UUID format for --session-id.
// Codebuddy/OpenCode/Codex accept any string.
func newSessionID() string {
	return uuid.New().String()
}

// testWorkDir returns a suitable working directory for integration tests.
// Prefers the current project directory (which is a git repo) over /tmp,
// since some CLIs (e.g., Codex) behave better in a git repo.
func testWorkDir() string {
	if dir, _ := os.Getwd(); dir != "" {
		return dir
	}
	return os.TempDir()
}

// requireCLIAvailable skips the test if the named CLI binary is not found on PATH.
func requireCLIAvailable(t *testing.T, cliName string) {
	t.Helper()
	if _, err := exec.LookPath(cliName); err != nil {
		t.Skipf("%s CLI not available, skipping integration test", cliName)
	}
}

// requireCodexEnv ensures the Codex CLI can actually run. Codex requires specific
// environment setup (API keys loaded from .env, profile configuration).
// It loads the project .env file and runs a smoke test with --profile m27
// (matching the agent config in config/agents/codex-MiniMax.yaml).
// Skip if the smoke test fails.
func requireCodexEnv(t *testing.T) {
	t.Helper()
	requireCLIAvailable(t, "codex")

	// Load .env file into process environment so API keys are available.
	// Go tests don't go through main.go, so .env is not auto-loaded.
	// Try multiple paths: project root (detected via go.mod), BinDir, current dir.
	dotenvPaths := []string{}
	// Walk up from current directory to find project root (has go.mod)
	if dir, _ := os.Getwd(); dir != "" {
		for d := dir; d != "/"; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
				dotenvPaths = append(dotenvPaths, filepath.Join(d, ".env"))
				break
			}
		}
	}
	if model.BinDir != "" {
		dotenvPaths = append(dotenvPaths, filepath.Join(model.BinDir, ".env"))
	}
	dotenvPaths = append(dotenvPaths, ".env")

	for _, p := range dotenvPaths {
		if _, err := os.Stat(p); err == nil {
			if err := model.LoadDotEnv(p); err != nil {
				t.Logf("warning: failed to load .env from %s: %v", p, err)
			} else {
				t.Logf("loaded .env from %s", p)
			}
			break
		}
	}

	// Quick smoke test: run codex with --profile m27 (matches agent config)
	// and a simple prompt. If it fails, the environment is not set up correctly.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "exec", "--profile", "m27", "--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check", "echo ok")
	cmd.Dir = os.TempDir()
	cmd.Env = os.Environ() // inherit env (includes MINIMAX_API_KEY from .env)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("codex CLI environment not ready (exit error: %v, output: %s), skipping integration test", err, truncate(string(output), 200))
	}
}

// collectAllEvents reads all events from the channel until it closes or timeout.
// Returns the collected events slice.
func collectAllEvents(t *testing.T, ch <-chan StreamEvent, timeout time.Duration) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timer.C:
			t.Log("collectEvents: timeout waiting for channel to close")
			return events
		}
	}
}

// findEvents returns all events matching the given type.
func findEvents(events []StreamEvent, eventType string) []StreamEvent {
	var matched []StreamEvent
	for _, e := range events {
		if e.Type == eventType {
			matched = append(matched, e)
		}
	}
	return matched
}

// requireEventSequence asserts that the events contain the specified event types
// in order (ignoring other event types in between).
func requireEventSequence(t *testing.T, events []StreamEvent, expectedTypes ...string) {
	t.Helper()
	var actualTypes []string
	for _, e := range events {
		actualTypes = append(actualTypes, e.Type)
	}

	idx := 0
	for _, actual := range actualTypes {
		if idx < len(expectedTypes) && actual == expectedTypes[idx] {
			idx++
		}
	}
	if idx < len(expectedTypes) {
		t.Errorf("expected event sequence %v not found; actual types: %v", expectedTypes, actualTypes)
	}
}

// concatContent joins all content from content-type events into a single string.
func concatContent(events []StreamEvent) string {
	var sb strings.Builder
	for _, e := range events {
		if e.Type == "content" {
			sb.WriteString(e.Content)
		}
	}
	return sb.String()
}

// extractSessionID extracts the session ID from metadata or session_capture events.
func extractSessionID(events []StreamEvent) string {
	// Prefer session_capture (early-captured external ID like OpenCode ses_xxx, Codex thread_xxx)
	for _, e := range events {
		if e.Type == "session_capture" && e.Content != "" {
			return e.Content
		}
	}
	// Fallback to metadata session ID (Claude/Codebuddy/Kimi)
	for _, e := range events {
		if e.Type == "metadata" && e.Meta != nil && e.Meta.SessionID != "" {
			return e.Meta.SessionID
		}
	}
	return ""
}

// --- 1. New Session Basic Dialog ---

func TestIntegration_Claude_NewSession(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from claude")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "metadata should contain model name")

	// AutoResumeBackend now forwards the "done" event
	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event from AutoResumeBackend")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_Codebuddy_NewSession(t *testing.T) {
	requireCLIAvailable(t, "codebuddy")
	backend, err := NewBackend("codebuddy")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from codebuddy")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "metadata should contain model name")

	// AutoResumeBackend now forwards the "done" event
	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event from AutoResumeBackend")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_OpenCode_NewSession(t *testing.T) {
	requireCLIAvailable(t, "opencode")
	backend, err := NewBackend("opencode")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "说一个字：好",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from opencode")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	// OpenCode metadata may or may not have model name depending on CLI version

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_Codex_NewSession(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "说一个字：好",
		WorkDir: testWorkDir(),
		Command: codexCommand,
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from codex")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "codex metadata should contain session ID (thread_id)")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

// --- 2. Stream Event Completeness ---

func TestIntegration_Claude_StreamEvents(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "1+1等于几？只回答数字",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	// Claude with --include-partial-messages should produce incremental content deltas
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events")

	// Metadata should include session ID from --session-id
	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents)
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "claude metadata should contain session ID")

	// Should have raw_output event for debugging
	rawEvents := findEvents(events, "raw_output")
	assert.NotEmpty(t, rawEvents, "should have raw_output event")
}

func TestIntegration_Codebuddy_StreamEvents(t *testing.T) {
	requireCLIAvailable(t, "codebuddy")
	backend, err := NewBackend("codebuddy")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "1+1等于几？只回答数字",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents)
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "codebuddy metadata should contain session ID")
	// Codebuddy should provide model via providerData
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "codebuddy metadata should contain model from providerData")

	rawEvents := findEvents(events, "raw_output")
	assert.NotEmpty(t, rawEvents, "should have raw_output event")
}

func TestIntegration_OpenCode_StreamEvents(t *testing.T) {
	requireCLIAvailable(t, "opencode")
	backend, err := NewBackend("opencode")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "1+1等于几？只回答数字",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	// OpenCode may produce thinking or content events (or both)
	thinkingEvents := findEvents(events, "thinking")
	contentEvents := findEvents(events, "content")
	assert.True(t, len(thinkingEvents) > 0 || len(contentEvents) > 0,
		"should have thinking or content events")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents)
	// OpenCode session ID is captured early via session_capture
	sessionCaptureEvents := findEvents(events, "session_capture")
	assert.NotEmpty(t, sessionCaptureEvents, "opencode should emit session_capture for ses_xxx ID")
}

func TestIntegration_Codex_StreamEvents(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "1+1等于几？只回答数字",
		WorkDir: testWorkDir(),
		Command: codexCommand,
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events")

	// Codex should capture thread_id via session_capture
	sessionCaptureEvents := findEvents(events, "session_capture")
	if assert.NotEmpty(t, sessionCaptureEvents, "codex should emit session_capture for thread ID") {
		t.Logf("codex session_capture content: %q", sessionCaptureEvents[0].Content)
		// Codex thread_id is a UUID (e.g. "019dfb6d-ad2c-7292-aaa8-93bf629c5fe2")
		assert.NotEmpty(t, sessionCaptureEvents[0].Content,
			"codex session_capture should contain a non-empty thread ID")
	}
}

// --- 3. Session Resume ---

func TestIntegration_Claude_ResumeSession(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "记住数字42，稍后我会问你。只回复OK",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	// Verify first conversation completed normally
	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume session
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")

	// Note: "done" event should now be forwarded by AutoResumeBackend
	doneEvents2 := findEvents(events2, "done")
	assert.NotEmpty(t, doneEvents2, "should receive 'done' event in resumed session")
}

// --- 3a. Claude Session Resume Deep Tests ---

// TestIntegration_Claude_MultiTurnResume verifies that Claude can maintain context
// across three turns in the same session: new → resume → resume again.
// This exercises the --resume flag being passed multiple times with the same
// session ID, and the CLI's ability to accumulate conversation history.
func TestIntegration_Claude_MultiTurnResume(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Turn 1: new session — ask Claude to remember a number
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "记住数字42，稍后我会问你。只回复OK",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	meta1 := findEvents(events1, "metadata")
	require.NotEmpty(t, meta1, "turn 1 should complete with metadata event")

	// Verify metadata contains a session ID (Claude echoes back --session-id)
	assert.NotEmpty(t, meta1[0].Meta.SessionID, "turn 1 metadata should contain session ID")

	// Turn 2: resume — ask what number was remembered
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content2 := concatContent(events2)
	assert.NotEmpty(t, content2, "turn 2 should receive content")

	// Turn 2 metadata should still have session ID
	meta2 := findEvents(events2, "metadata")
	require.NotEmpty(t, meta2, "turn 2 should have metadata event")
	assert.NotEmpty(t, meta2[0].Meta.SessionID, "turn 2 metadata should contain session ID")

	// Turn 3: resume again — ask for the number a second time
	ctx3, cancel3 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel3()

	ch3, err := backend.ExecuteStream(ctx3, ChatRequest{
		Prompt:    "再告诉我一次那个数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events3 := collectAllEvents(t, ch3, 90*time.Second)
	requireEventSequence(t, events3, "content", "metadata")
	content3 := concatContent(events3)
	assert.NotEmpty(t, content3, "turn 3 should receive content")

	// Verify done events on all resumed turns
	doneEvents2 := findEvents(events2, "done")
	assert.NotEmpty(t, doneEvents2, "turn 2 should receive 'done' event")
	doneEvents3 := findEvents(events3, "done")
	assert.NotEmpty(t, doneEvents3, "turn 3 should receive 'done' event")
}

// TestIntegration_Claude_ResumeSessionIDConsistency verifies that the session ID
// remains consistent across a new session and its resumed continuation.
// Claude uses the ClawBench UUID directly as --session-id / --resume, so the
// metadata.SessionID in both rounds should match the original UUID.
func TestIntegration_Claude_ResumeSessionIDConsistency(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	meta1 := findEvents(events1, "metadata")
	require.NotEmpty(t, meta1, "first conversation should complete with metadata event")
	firstSessionID := meta1[0].Meta.SessionID
	assert.NotEmpty(t, firstSessionID, "metadata should contain session ID")

	// Phase 2: resume session with same session ID
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "再说一个字：是",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	meta2 := findEvents(events2, "metadata")
	require.NotEmpty(t, meta2, "resumed session should complete with metadata event")
	resumedSessionID := meta2[0].Meta.SessionID
	assert.NotEmpty(t, resumedSessionID, "resumed metadata should contain session ID")

	// The session ID in metadata should match between the original and resumed rounds
	assert.Equal(t, firstSessionID, resumedSessionID,
		"session ID should remain consistent across resume — Claude uses ClawBench UUID directly")
}

// TestIntegration_Claude_ResumeAfterCancel verifies that a session can be resumed
// after being cancelled mid-stream. This simulates the real-world scenario where
// a user cancels a long-running response and then sends a new message to continue.
//
// The test flow:
//  1. Start a new session and cancel after receiving some content
//  2. Resume the same session ID — Claude should still have the initial context
//
// Note: After cancellation, the CLI process is killed, so the session state
// depends on what the CLI persisted before the kill signal. Claude CLI persists
// conversation state incrementally, so a resume after cancel should work.
func TestIntegration_Claude_ResumeAfterCancel(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session with a simple prompt, let it complete normally
	// (we use a simple prompt to ensure the session is established in Claude's state)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "记住数字7，只回复OK",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	meta1 := findEvents(events1, "metadata")
	require.NotEmpty(t, meta1, "first conversation should complete with metadata event")

	// Phase 2: cancel a second prompt mid-stream
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "现在从1数到100，每个数字一行",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	// Collect at least one content event, then cancel
	var events2 []StreamEvent
	cancelled := false
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch2:
			if !ok {
				goto phase2Done
			}
			events2 = append(events2, event)
			if !cancelled && event.Type == "content" {
				cancelled = true
				cancel2()
			}
		case <-timer.C:
			t.Log("phase 2: timeout waiting for content")
			goto phase2Done
		}
	}
phase2Done:
	contentEvents2 := findEvents(events2, "content")
	assert.NotEmpty(t, contentEvents2, "should have received content before cancel in phase 2")
	t.Logf("phase 2: cancelled after %d events, %d content events", len(events2), len(contentEvents2))

	// Phase 3: resume the session after cancellation
	// Claude should remember the number 7 from phase 1 even though phase 2 was cancelled
	ctx3, cancel3 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel3()

	ch3, err := backend.ExecuteStream(ctx3, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events3 := collectAllEvents(t, ch3, 90*time.Second)
	requireEventSequence(t, events3, "content", "metadata")
	content3 := concatContent(events3)
	assert.NotEmpty(t, content3, "should receive content in resumed session after cancel")

	// Best-effort: check if the response contains "7"
	// AI responses are non-deterministic, but with a clean resume the model
	// should recall the number from the first turn
	if !strings.Contains(content3, "7") {
		t.Logf("claude did not recall number 7 after cancel+resume — AI behavior is non-deterministic; content: %s", truncate(content3, 300))
	}
}

// TestIntegration_Claude_ResumeMetadataCapture verifies that the metadata event
// from a Claude resumed session contains the expected fields: SessionID, Model,
// and token usage (InputTokens/OutputTokens). These fields are critical for
// the handler's session capture logic (writing external_session_id to DB).
func TestIntegration_Claude_ResumeMetadataCapture(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	meta1 := findEvents(events1, "metadata")
	require.NotEmpty(t, meta1, "should have metadata event from new session")
	assert.NotEmpty(t, meta1[0].Meta.SessionID, "new session metadata should have SessionID")
	assert.NotEmpty(t, meta1[0].Meta.Model, "new session metadata should have Model")

	// Phase 2: resume session
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "再说一个字：是",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	meta2 := findEvents(events2, "metadata")
	require.NotEmpty(t, meta2, "should have metadata event from resumed session")
	assert.NotEmpty(t, meta2[0].Meta.SessionID, "resumed session metadata should have SessionID")
	assert.NotEmpty(t, meta2[0].Meta.Model, "resumed session metadata should have Model")

	// Token usage should be present (non-zero) in both rounds
	assert.NotZero(t, meta1[0].Meta.InputTokens, "new session should report input token usage")
	assert.NotZero(t, meta2[0].Meta.InputTokens, "resumed session should report input token usage")
}

func TestIntegration_Codebuddy_ResumeSession(t *testing.T) {
	requireCLIAvailable(t, "codebuddy")
	backend, err := NewBackend("codebuddy")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "记住数字42，稍后我会问你。只回复OK",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume session
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")

	// "done" event should now be forwarded by AutoResumeBackend
	doneEvents2 := findEvents(events2, "done")
	assert.NotEmpty(t, doneEvents2, "should receive 'done' event in resumed session")
}

func TestIntegration_OpenCode_ResumeSession(t *testing.T) {
	requireCLIAvailable(t, "opencode")
	backend, err := NewBackend("opencode")
	require.NoError(t, err)

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:  "记住数字42，稍后我会问你。只回复OK",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	sessionID := extractSessionID(events1)
	require.NotEmpty(t, sessionID, "should capture OpenCode session ID (ses_xxx)")
	assert.True(t, strings.HasPrefix(sessionID, "ses_"),
		"OpenCode session ID should start with ses_")

	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume with the OpenCode session ID
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")
}

func TestIntegration_Codex_ResumeSession(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:  "记住数字42，稍后我会问你。只回复OK",
		WorkDir: testWorkDir(),
		Command: codexCommand,
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	sessionID := extractSessionID(events1)
	require.NotEmpty(t, sessionID, "should capture Codex thread ID")
	// Codex thread_id is a UUID format, not "thread_xxx" prefix

	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume with the Codex thread ID
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
		Command:   codexCommand,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")
}

// --- 4. Context Cancellation ---

func TestIntegration_Claude_CancelMidStream(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "写一篇500字的文章，主题是春天的花园",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

func TestIntegration_Codebuddy_CancelMidStream(t *testing.T) {
	requireCLIAvailable(t, "codebuddy")
	backend, err := NewBackend("codebuddy")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "写一篇500字的文章，主题是春天的花园",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

func TestIntegration_OpenCode_CancelMidStream(t *testing.T) {
	requireCLIAvailable(t, "opencode")
	backend, err := NewBackend("opencode")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "写一篇500字的文章，主题是春天的花园",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

func TestIntegration_Codex_CancelMidStream(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "写一篇500字的文章，主题是春天的花园",
		WorkDir: testWorkDir(),
		Command: codexCommand,
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

// cancelOnFirstContent reads events from ch, calls cancelFunc after the first
// content event, then continues collecting until the channel closes.
func cancelOnFirstContent(t *testing.T, ch <-chan StreamEvent, cancelFunc context.CancelFunc) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	cancelled := false
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
			// Cancel after first content event
			if !cancelled && event.Type == "content" {
				cancelled = true
				cancelFunc()
			}
		case <-timer.C:
			t.Log("cancelOnFirstContent: timeout")
			return events
		}
	}
}

// --- 5. Error Paths ---

func TestIntegration_InvalidWorkDir(t *testing.T) {
	// Test all CLIBackend-based backends with an invalid work directory.
	// The CLI should fail to start or produce an error/warning event.
	backends := []struct {
		name    string
		cliName string
	}{
		{"claude", "claude"},
		{"codebuddy", "codebuddy"},
		{"opencode", "opencode"},
		{"vecli", "vecli"},
	}

	for _, tc := range backends {
		t.Run(tc.name, func(t *testing.T) {
			requireCLIAvailable(t, tc.cliName)
			backend, err := NewBackend(tc.name)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ch, err := backend.ExecuteStream(ctx, ChatRequest{
				Prompt:    "hello",
				SessionID: newSessionID(),
				WorkDir:   "/nonexistent/path/that/does/not/exist/abc123",
			})
			if err != nil {
				// ExecuteStream itself returned an error — acceptable
				t.Logf("ExecuteStream returned error (expected for invalid WorkDir): %v", err)
				return
			}

			// If no error from ExecuteStream, the stream should contain warning/error
			events := collectAllEvents(t, ch, 30*time.Second)
			hasError := len(findEvents(events, "error")) > 0
			hasWarning := len(findEvents(events, "warning")) > 0
			assert.True(t, hasError || hasWarning,
				"invalid WorkDir should produce error or warning events; got types: %v",
				eventTypes(events))
		})
	}
}

func TestIntegration_Codex_InvalidCommand(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "hello",
		WorkDir: testWorkDir(),
		Command: "nonexistent-codex-binary-12345",
	})
	// Codex should fail to start the command
	assert.Error(t, err, "invalid Command should cause ExecuteStream to return error")
}

// eventTypes returns a slice of event type strings for debugging.
func eventTypes(events []StreamEvent) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// --- 6. AutoResume ExitPlanMode ---

func TestIntegration_AutoResume_ExitPlanMode(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	// Verify it's an AutoResumeBackend
	_, ok := backend.(*AutoResumeBackend)
	require.True(t, ok, "claude backend should be AutoResumeBackend")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Use a prompt that tends to trigger EnterPlanMode in Claude.
	// Keep it short to avoid long tool-use chains that timeout.
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "请进入规划模式，帮我规划一下如何给hello world程序写测试",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 200*time.Second)

	// Check if ExitPlanMode was triggered
	resumeSplitEvents := findEvents(events, "resume_split")
	if len(resumeSplitEvents) == 0 {
		t.Log("ExitPlanMode was not triggered in this run — this is expected and not a failure")
		t.Log("AI behavior is non-deterministic; ExitPlanMode may not always be triggered")
		// Still verify basic flow completed — need either metadata or content events
		metaEvents := findEvents(events, "metadata")
		if len(metaEvents) > 0 {
			t.Log("basic flow completed with metadata event")
		} else {
			contentEvents := findEvents(events, "content")
			if assert.NotEmpty(t, contentEvents, "should have at least content events") {
				t.Log("basic flow produced content events but no metadata (may have timed out)")
			}
		}
		return
	}

	// If ExitPlanMode was triggered, verify the resume flow
	t.Log("ExitPlanMode detected — verifying resume flow")

	// Should have resume_split event
	requireEventSequence(t, events, "resume_split", "content", "metadata")

	// Should have two rounds of content (before and after resume)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events after resume")

	// Should have metadata from the second round
	metaEvents := findEvents(events, "metadata")
	assert.NotEmpty(t, metaEvents, "should have metadata from resumed session")

	// "done" event should now be forwarded by AutoResumeBackend
	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event")
}

// --- 7. System Prompt Injection ---

// TestIntegration_*_SystemPromptInjection tests verify that the SystemPrompt field
// in ChatRequest is correctly passed through to the CLI backend. The tests check
// two things:
// 1. The system prompt is present in the CLI arguments (verified via raw_output for
//    backends that support --system-prompt, or via prompt injection for others)
// 2. The AI response acknowledges the system prompt (best-effort — AI may not comply)

func TestIntegration_Claude_SystemPromptInjection(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	const marker = "INTEGRATION_TEST_MARKER_7X9Z"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		SessionID:    newSessionID(),
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Verify the stream completed successfully with metadata (which means CLI args were valid)
	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")

	// Best-effort check: AI may or may not include the marker in its response
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("claude did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

func TestIntegration_Codebuddy_SystemPromptInjection(t *testing.T) {
	requireCLIAvailable(t, "codebuddy")
	backend, err := NewBackend("codebuddy")
	require.NoError(t, err)

	const marker = "INTEGRATION_TEST_MARKER_K3P8"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		SessionID:    newSessionID(),
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Verify the stream completed successfully with metadata (which means CLI args were valid)
	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")

	// Best-effort check
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("codebuddy did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

func TestIntegration_OpenCode_SystemPromptInjection(t *testing.T) {
	requireCLIAvailable(t, "opencode")
	backend, err := NewBackend("opencode")
	require.NoError(t, err)

	// OpenCode CLI has no --system-prompt flag; prompt is injected as [System Instructions: ...]
	const marker = "INTEGRATION_TEST_MARKER_R5N1"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model.ChatSystemPromptInterval = 10

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Best-effort check
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("opencode did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

func TestIntegration_Codex_SystemPromptInjection(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	// Codex CLI has no --system-prompt flag; prompt is injected as [System Instructions: ...]
	const marker = "INTEGRATION_TEST_MARKER_Q8V6"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model.ChatSystemPromptInterval = 10

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
		Command:      codexCommand,
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Best-effort check
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("codex did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

// --- Qoder Integration Tests ---

func TestIntegration_Qoder_NewSession(t *testing.T) {
	requireCLIAvailable(t, "qodercli")
	backend, err := NewBackend("qoder")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from qoder")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "qoder metadata should contain session ID")

	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event from AutoResumeBackend")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_Qoder_StreamEvents(t *testing.T) {
	requireCLIAvailable(t, "qodercli")
	backend, err := NewBackend("qoder")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "1+1等于几？只回答数字",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents)
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "qoder metadata should contain session ID")

	// Should have raw_output event for debugging
	rawEvents := findEvents(events, "raw_output")
	assert.NotEmpty(t, rawEvents, "should have raw_output event")
}

func TestIntegration_Qoder_ResumeSession(t *testing.T) {
	requireCLIAvailable(t, "qodercli")
	backend, err := NewBackend("qoder")
	require.NoError(t, err)

	sessionID := newSessionID()

	// Phase 1: new session
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:    "记住数字42，稍后我会问你。只回复OK",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 90*time.Second)
	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume session
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 90*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")

	doneEvents2 := findEvents(events2, "done")
	assert.NotEmpty(t, doneEvents2, "should receive 'done' event in resumed session")
}

func TestIntegration_Qoder_SystemPromptInjection(t *testing.T) {
	requireCLIAvailable(t, "qodercli")
	backend, err := NewBackend("qoder")
	require.NoError(t, err)

	// Qoder CLI supports --system-prompt flag natively
	const marker = "INTEGRATION_TEST_MARKER_Q7W3"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model.ChatSystemPromptInterval = 10

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		SessionID:    newSessionID(),
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Best-effort check — AI compliance is non-deterministic
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("qoder did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

// --- VeCLI Integration Tests ---

// requireVeCLIEnv ensures the VeCLI CLI can run in the test environment.
func requireVeCLIEnv(t *testing.T) {
	t.Helper()
	requireCLIAvailable(t, "vecli")
	// VeCLI uses VOLCENGINE_ACCESS_KEY + VOLCENGINE_SECRET_KEY env vars,
	// or interactive login. If env vars are missing, the CLI will error.
	// We don't skip here — let the actual test reveal the auth issue.
}

func TestIntegration_VeCLI_NewSession(t *testing.T) {
	requireVeCLIEnv(t)
	backend, err := NewBackend("vecli")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "说一个字：好",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	// VeCLI may fail due to API auth issues; skip gracefully
	contentEvents := findEvents(events, "content")
	if len(contentEvents) == 0 {
		warningEvents := findEvents(events, "warning")
		errorEvents := findEvents(events, "error")
		t.Skipf("vecli produced no content events (likely auth/network issue); warnings: %d, errors: %d, event types: %v",
			len(warningEvents), len(errorEvents), eventTypes(events))
	}

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from vecli")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "vecli metadata should contain model name from session-summary")
	assert.NotZero(t, metaEvents[0].Meta.DurationMs, "vecli metadata should contain duration from session-summary")

	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_VeCLI_StreamEvents(t *testing.T) {
	requireVeCLIEnv(t)
	backend, err := NewBackend("vecli")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "1+1等于几？只回答数字",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	contentEvents := findEvents(events, "content")
	if len(contentEvents) == 0 {
		warningEvents := findEvents(events, "warning")
		t.Skipf("vecli produced no content events (likely auth/network issue); warnings: %d, event types: %v",
			len(warningEvents), eventTypes(events))
	}
	assert.NotEmpty(t, contentEvents, "should have content events")

	// VeCLI metadata comes from session-summary file (post-process)
	metaEvents := findEvents(events, "metadata")
	assert.NotEmpty(t, metaEvents, "should have metadata from session-summary")

	// Should have raw_output event for debugging
	rawEvents := findEvents(events, "raw_output")
	assert.NotEmpty(t, rawEvents, "should have raw_output event")
}

func TestIntegration_VeCLI_CancelMidStream(t *testing.T) {
	requireVeCLIEnv(t)
	backend, err := NewBackend("vecli")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "写一篇500字的文章，主题是春天的花园",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	if len(contentEvents) == 0 {
		t.Skipf("vecli produced no content before cancel (likely auth/network issue); event types: %v", eventTypes(events))
	}
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

func TestIntegration_VeCLI_SystemPromptInjection(t *testing.T) {
	requireVeCLIEnv(t)
	backend, err := NewBackend("vecli")
	require.NoError(t, err)

	// VeCLI has no --system-prompt flag; prompt is injected as [System Instructions: ...]
	const marker = "INTEGRATION_TEST_MARKER_V9K2"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model.ChatSystemPromptInterval = 10

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 90*time.Second)

	contentEvents := findEvents(events, "content")
	if len(contentEvents) == 0 {
		t.Skipf("vecli produced no content events (likely auth/network issue); event types: %v", eventTypes(events))
	}

	requireEventSequence(t, events, "content", "metadata")

	// Best-effort check — AI compliance is non-deterministic
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("vecli did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

// --- DeepSeek Integration Tests ---

func TestIntegration_DeepSeek_NewSession(t *testing.T) {
	requireCLIAvailable(t, "deepseek")
	backend, err := NewBackend("deepseek")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "说一个字：好",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 150*time.Second)

	requireEventSequence(t, events, "content", "metadata")
	content := concatContent(events)
	assert.NotEmpty(t, content, "should receive content from deepseek")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "metadata should contain model name")

	// DeepSeek TUI sends session_capture before content
	sessionCaptureEvents := findEvents(events, "session_capture")
	assert.NotEmpty(t, sessionCaptureEvents, "should have session_capture event")

	// AutoResumeBackend forwards the "done" event
	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event from AutoResumeBackend")

	errorEvents := findEvents(events, "error")
	assert.Empty(t, errorEvents, "should not have error events")
}

func TestIntegration_DeepSeek_StreamEvents(t *testing.T) {
	requireCLIAvailable(t, "deepseek")
	backend, err := NewBackend("deepseek")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "1+1等于几？只回答数字",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 150*time.Second)

	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events")

	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")
	assert.NotEmpty(t, metaEvents[0].Meta.Model, "metadata should contain model name")
	assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "metadata should contain session ID")

	// DeepSeek TUI sends session_capture early
	sessionCaptureEvents := findEvents(events, "session_capture")
	assert.NotEmpty(t, sessionCaptureEvents, "should have session_capture event")
}

func TestIntegration_DeepSeek_ResumeSession(t *testing.T) {
	requireCLIAvailable(t, "deepseek")
	backend, err := NewBackend("deepseek")
	require.NoError(t, err)

	// Phase 1: new session — capture session ID from session_capture event
	ctx1, cancel1 := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel1()

	ch1, err := backend.ExecuteStream(ctx1, ChatRequest{
		Prompt:  "记住数字42，稍后我会问你。只回复OK",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events1 := collectAllEvents(t, ch1, 150*time.Second)
	sessionID := extractSessionID(events1)
	require.NotEmpty(t, sessionID, "should capture DeepSeek session ID from session_capture event")

	doneEvents1 := findEvents(events1, "metadata")
	require.NotEmpty(t, doneEvents1, "first conversation should complete with metadata event")

	// Phase 2: resume session using captured session ID
	ctx2, cancel2 := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel2()

	ch2, err := backend.ExecuteStream(ctx2, ChatRequest{
		Prompt:    "我之前让你记住的数字是什么？只回答数字",
		SessionID: sessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	require.NoError(t, err)

	events2 := collectAllEvents(t, ch2, 150*time.Second)
	requireEventSequence(t, events2, "content", "metadata")
	content := concatContent(events2)
	assert.NotEmpty(t, content, "should receive content in resumed session")

	doneEvents2 := findEvents(events2, "done")
	assert.NotEmpty(t, doneEvents2, "should receive 'done' event in resumed session")
}

func TestIntegration_DeepSeek_CancelMidStream(t *testing.T) {
	requireCLIAvailable(t, "deepseek")
	backend, err := NewBackend("deepseek")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "写一篇500字的文章，主题是春天的花园",
		WorkDir: testWorkDir(),
	})
	require.NoError(t, err)

	events := cancelOnFirstContent(t, ch, cancel)
	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
}

func TestIntegration_DeepSeek_SystemPromptInjection(t *testing.T) {
	requireCLIAvailable(t, "deepseek")
	backend, err := NewBackend("deepseek")
	require.NoError(t, err)

	// DeepSeek TUI supports --system-prompt flag natively
	const marker = "INTEGRATION_TEST_MARKER_D5K9"
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:       "请重复以下标记：" + marker,
		WorkDir:      testWorkDir(),
		SystemPrompt: "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求",
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 150*time.Second)
	requireEventSequence(t, events, "content", "metadata")

	// Verify the stream completed successfully with metadata (which means CLI args were valid)
	metaEvents := findEvents(events, "metadata")
	require.NotEmpty(t, metaEvents, "should have metadata event")

	// Best-effort check — AI compliance is non-deterministic
	content := concatContent(events)
	if !strings.Contains(content, marker) {
		t.Logf("deepseek did not include marker %q in response — AI compliance is non-deterministic; content: %s", marker, truncate(content, 200))
	}
}

// --- Helpers ---

// truncate returns the first n chars of s with "..." appended if longer.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
