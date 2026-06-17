package pi

import (
	"testing"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
)

func TestPiPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("pi")
	if entry == nil {
		t.Fatal("pi backend factory not registered")
	}
}

func TestPiPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("pi")
	if !entry.NeedsAutoResume {
		t.Error("pi should have needsAutoResume=true")
	}
}

func TestBuildPiStreamArgs_NewSession(t *testing.T) {
	req := ai.ChatRequest{
		Prompt:       "hello world",
		SystemPrompt: "you are helpful",
		Model:        "pi-4",
		WorkDir:      "/home/user/project",
		Resume:       false,
	}
	args := buildPiStreamArgs(req)

	// Base args
	assert.Equal(t, "-p", args[0])
	assert.Equal(t, "--mode", args[1])
	assert.Equal(t, "json", args[2])

	// New interactive session → no session flag (Pi creates persistent session)
	assert.NotContains(t, args, "--no-session")
	assert.NotContains(t, args, "--session")
	assert.NotContains(t, args, "--continue")

	// Skip AGENTS.md discovery
	assert.Contains(t, args, "--no-context-files")

	// System prompt
	assert.Contains(t, args, "--append-system-prompt")
	idx := indexOf(args, "--append-system-prompt")
	assert.Equal(t, "you are helpful", args[idx+1])

	// Model
	assert.Contains(t, args, "--model")
	idx = indexOf(args, "--model")
	assert.Equal(t, "pi-4", args[idx+1])

	// Working directory is set via cmd.Dir, not a CLI flag
	assert.NotContains(t, args, "--add-dir")

	// Prompt is last
	assert.Equal(t, "hello world", args[len(args)-1])

	// NOT resuming
	assert.NotContains(t, args, "--session")
	assert.NotContains(t, args, "--continue")
}

func TestBuildPiStreamArgs_ResumeSession(t *testing.T) {
	req := ai.ChatRequest{
		Prompt:    "continue this",
		SessionID: "sess-123",
		Resume:    true,
	}
	args := buildPiStreamArgs(req)

	// Resume with session ID → --session <id>
	assert.Contains(t, args, "--session")
	idx := indexOf(args, "--session")
	assert.Equal(t, "sess-123", args[idx+1])

	// NOT --no-session or --continue
	assert.NotContains(t, args, "--no-session")
	assert.NotContains(t, args, "--continue")
}

func TestBuildPiStreamArgs_ResumeContinue(t *testing.T) {
	req := ai.ChatRequest{
		Prompt: "keep going",
		Resume: true,
	}
	args := buildPiStreamArgs(req)

	// Resume without session ID → --continue
	assert.Contains(t, args, "--continue")

	// NOT --session or --no-session
	assert.NotContains(t, args, "--session")
	assert.NotContains(t, args, "--no-session")
}

func TestBuildPiStreamArgs_ScheduledExecution(t *testing.T) {
	req := ai.ChatRequest{
		Prompt:             "scheduled task",
		ScheduledExecution: true,
		Resume:             false,
	}
	args := buildPiStreamArgs(req)

	// Scheduled = new session → --no-session
	assert.Contains(t, args, "--no-session")
	assert.NotContains(t, args, "--session")
	assert.NotContains(t, args, "--continue")
}

func TestBuildPiStreamArgs_NoModel(t *testing.T) {
	req := ai.ChatRequest{
		Prompt: "hello",
		Model:  "",
	}
	args := buildPiStreamArgs(req)

	assert.NotContains(t, args, "--model")
}

func TestBuildPiStreamArgs_NoSystemPrompt(t *testing.T) {
	req := ai.ChatRequest{
		Prompt:       "hello",
		SystemPrompt: "",
	}
	args := buildPiStreamArgs(req)

	assert.NotContains(t, args, "--append-system-prompt")
}

// indexOf returns the index of the first occurrence of target in slice, or -1.
func indexOf(slice []string, target string) int {
	for i, v := range slice {
		if v == target {
			return i
		}
	}
	return -1
}

func TestPiBackendDefinition(t *testing.T) {
	backend := newPiBackend()
	cli, ok := backend.(*ai.CLIBackend)
	assert.True(t, ok, "expected *ai.CLIBackend, got %T", backend)
	assert.Equal(t, "pi", cli.BackendName)
	assert.Equal(t, "pi", cli.Cmd)
	assert.NotNil(t, cli.BuildArgsFn)
	assert.NotNil(t, cli.NewParserFn)

	// newParser should return a *PiStreamParser
	parser := cli.NewParserFn()
	assert.NotNil(t, parser)
	_, ok = parser.(*ai.PiStreamParser)
	assert.True(t, ok, "expected *ai.PiStreamParser, got %T", parser)

	// filterLine and preStart should be nil
	assert.Nil(t, cli.FilterLineFn)
	assert.Nil(t, cli.PreStartFn)
}

// TestBuildPiStreamArgs_EndToEndResumeChain verifies the complete
// buildPiStreamArgs behavior across the session lifecycle:
//  1. New session → no session flag (Pi creates persistent session)
//  2. Pi emits session event → external_session_id captured
//  3. Resume with captured ID → --session <id>
//  4. Resume without captured ID → --continue (fallback)
func TestBuildPiStreamArgs_EndToEndResumeChain(t *testing.T) {
	piSessionID := "019e2172-6ebd-743e-8bb6-39d51df91bde"

	// Phase 1: New interactive session — should NOT use --no-session
	// so Pi creates a persistent session file on disk.
	newReq := ai.ChatRequest{
		Prompt:       "hello",
		SystemPrompt: "you are helpful",
		Model:        "minimax-cn/MiniMax-M2.7",
		Resume:       false,
	}
	newArgs := buildPiStreamArgs(newReq)
	assert.NotContains(t, newArgs, "--no-session",
		"new interactive session must NOT use --no-session (would make session ephemeral)")
	assert.NotContains(t, newArgs, "--session",
		"new session should not pass --session")
	assert.NotContains(t, newArgs, "--continue",
		"new session should not pass --continue")

	// Phase 2: Pi CLI emits session event with its own session ID.
	// The handler captures this via PiStreamParser.GetCapturedSessionID()
	// and persists it as external_session_id. We simulate this by
	// constructing a ChatRequest with the captured ID.

	// Phase 3: Resume with the Pi-assigned session ID → --session <id>
	resumeWithIDReq := ai.ChatRequest{
		Prompt:    "continue",
		SessionID: piSessionID,
		Resume:    true,
	}
	resumeWithIDArgs := buildPiStreamArgs(resumeWithIDReq)
	assert.Contains(t, resumeWithIDArgs, "--session")
	idx := indexOf(resumeWithIDArgs, "--session")
	assert.Equal(t, piSessionID, resumeWithIDArgs[idx+1],
		"resume should pass the Pi-assigned session ID to --session")
	assert.NotContains(t, resumeWithIDArgs, "--no-session")
	assert.NotContains(t, resumeWithIDArgs, "--continue")

	// Phase 4: Resume without a known session ID → --continue (fallback)
	// This happens when the session_capture was missed (e.g. stream cancelled early)
	resumeNoIDReq := ai.ChatRequest{
		Prompt:    "keep going",
		SessionID: "",
		Resume:    true,
	}
	resumeNoIDArgs := buildPiStreamArgs(resumeNoIDReq)
	assert.Contains(t, resumeNoIDArgs, "--continue",
		"resume without session ID should fall back to --continue")
	assert.NotContains(t, resumeNoIDArgs, "--session")
	assert.NotContains(t, resumeNoIDArgs, "--no-session")
}

// TestBuildPiStreamArgs_NewSessionNoNoSessionFlag specifically verifies
// the Layer 2 fix: new interactive sessions must NOT pass --no-session.
// Previously, --no-session was used, which made Pi sessions ephemeral.
// The captured session ID was then useless for --session resume because
// the session file was never saved to disk.
func TestBuildPiStreamArgs_NewSessionNoNoSessionFlag(t *testing.T) {
	req := ai.ChatRequest{
		Prompt: "hello",
		Resume: false,
	}
	args := buildPiStreamArgs(req)

	// The critical fix: no --no-session flag for new interactive sessions.
	// Without this, Pi creates an ephemeral session that cannot be resumed.
	assert.NotContains(t, args, "--no-session",
		"BUG REGRESSION: --no-session must not be used for new interactive sessions. "+
			"Pi creates ephemeral sessions with --no-session, making the captured "+
			"session ID unusable for --session resume.")

	// Should also not have any other session flag
	assert.NotContains(t, args, "--session")
	assert.NotContains(t, args, "--continue")
}

// TestBuildPiStreamArgs_ScheduledStillUsesNoSession verifies that
// scheduled executions still use --no-session (they don't need persistence).
func TestBuildPiStreamArgs_ScheduledStillUsesNoSession(t *testing.T) {
	req := ai.ChatRequest{
		Prompt:             "scheduled task",
		ScheduledExecution: true,
		Resume:             false,
	}
	args := buildPiStreamArgs(req)
	assert.Contains(t, args, "--no-session",
		"scheduled executions should still use --no-session")
}
