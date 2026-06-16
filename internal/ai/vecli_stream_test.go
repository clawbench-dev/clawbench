package ai

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseVeCLILine is a test helper that parses a single line through VeCLIStreamParser.
func parseVeCLILine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &VeCLIStreamParser{}
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// --- VeCLIStreamParser tests ---

func TestVeCLIStream_ParseLine_ContentLine(t *testing.T) {
	events := parseVeCLILine("Hello, world!")
	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Hello, world!\n", events[0].Content)
}

func TestVeCLIStream_ParseLine_MultipleLines(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &VeCLIStreamParser{}
	parser.ParseLine("Line 1", ch)
	parser.ParseLine("Line 2", ch)
	parser.ParseLine("Line 3", ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	require.Len(t, events, 3)
	assert.Equal(t, "Line 1\n", events[0].Content)
	assert.Equal(t, "Line 2\n", events[1].Content)
	assert.Equal(t, "Line 3\n", events[2].Content)
}

func TestVeCLIStream_ParseLine_EmptyLine(t *testing.T) {
	// VeCLIStreamParser does not filter — that's CLIBackend's job.
	// An empty line still produces a content event (just "\n").
	events := parseVeCLILine("")
	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "\n", events[0].Content)
}

func TestVeCLIStream_ParseLine_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{"JSON-like", `{"type":"content","text":"hello"}`, `{"type":"content","text":"hello"}` + "\n"},
		{"ANSI escape", "\x1b[32mgreen text\x1b[0m", "\x1b[32mgreen text\x1b[0m\n"},
		{"Chinese", "你好世界", "你好世界\n"},
		{"Tab", "col1\tcol2", "col1\tcol2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parseVeCLILine(tt.line)
			require.Len(t, events, 1)
			assert.Equal(t, tt.expected, events[0].Content)
		})
	}
}

func TestVeCLIStream_GetCapturedSessionID(t *testing.T) {
	parser := &VeCLIStreamParser{}
	assert.Equal(t, "", parser.GetCapturedSessionID(),
		"VeCLI has no session resume, GetCapturedSessionID should always return empty string")
}

// --- buildVeCLIArgs tests ---

func TestBuildVeCLIArgs_Basic(t *testing.T) {
	req := ChatRequest{
		Prompt: "say hello",
	}
	args := buildVeCLIArgs(req)

	expected := []string{"--yolo", "--prompt", "say hello"}
	assert.Equal(t, expected, args)
}

func TestBuildVeCLIArgs_WithWorkDir(t *testing.T) {
	req := ChatRequest{
		Prompt:  "hello",
		WorkDir: "/home/user/project",
	}
	args := buildVeCLIArgs(req)

	assert.Contains(t, args, "--include-directories")
	idx := indexOfArg(args, "--include-directories")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "/home/user/project", args[idx+1])
}

func TestBuildVeCLIArgs_WithModel(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
		Model:  "deepseek-v3-1-terminus",
	}
	args := buildVeCLIArgs(req)

	assert.Contains(t, args, "--model")
	idx := indexOfArg(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "deepseek-v3-1-terminus", args[idx+1])
}

func TestBuildVeCLIArgs_SystemPromptInjection(t *testing.T) {
	req := ChatRequest{
		Prompt:       "do something",
		SystemPrompt: "Be helpful",
	}
	args := buildVeCLIArgs(req)

	// Should contain --prompt with [System Instructions: ...] prefix
	idx := indexOfArg(args, "--prompt")
	require.GreaterOrEqual(t, idx, 0)
	prompt := args[idx+1]
	assert.Contains(t, prompt, "[System Instructions: Be helpful]")
	assert.Contains(t, prompt, "do something")
}

func TestBuildVeCLIArgs_NoModel(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildVeCLIArgs(req)

	assert.NotContains(t, args, "--model",
		"when Model is empty, --model flag should not be added; VeCLI auto-selects its default model")
}

func TestBuildVeCLIArgs_NoResumeFlag(t *testing.T) {
	req := ChatRequest{
		Prompt:    "continue",
		SessionID: "some-session",
		Resume:    true,
	}
	args := buildVeCLIArgs(req)

	assert.NotContains(t, args, "--resume",
		"VeCLI does not support --resume; buildVeCLIArgs should never include it")
}

// --- VeCLISessionSummary tests ---

func TestVeCLISessionSummary_ParseSuccess(t *testing.T) {
	raw := `{
		"sessionMetrics": {
			"models": {
				"deepseek-v3-1-terminus": {
					"api": {"totalRequests": 3, "totalErrors": 0, "totalLatencyMs": 549},
					"tokens": {"prompt": 400, "candidates": 100, "total": 500, "cached": 0, "thoughts": 0, "tool": 0}
				}
			},
			"tools": {
				"totalCalls": 2,
				"totalSuccess": 2,
				"totalFail": 0,
				"totalDurationMs": 300,
				"totalDecisions": {"accept": 0, "reject": 0, "modify": 0, "auto_accept": 2},
				"byName": {}
			},
			"files": {"totalLinesAdded": 10, "totalLinesRemoved": 5}
		}
	}`

	var summary VeCLISessionSummary
	err := json.Unmarshal([]byte(raw), &summary)
	require.NoError(t, err)

	meta := summary.extractMetadata("")
	assert.Equal(t, "deepseek-v3-1-terminus", meta.Model)
	assert.Equal(t, 400, meta.InputTokens)
	assert.Equal(t, 100, meta.OutputTokens)
	assert.Equal(t, 549, meta.DurationMs)
	assert.Equal(t, "stop", meta.StopReason)
	assert.False(t, meta.IsError)
}

func TestVeCLISessionSummary_InvalidJSON(t *testing.T) {
	var summary VeCLISessionSummary
	err := json.Unmarshal([]byte("not valid json"), &summary)
	assert.Error(t, err)
}

func TestVeCLISessionSummary_EmptyModels(t *testing.T) {
	raw := `{
		"sessionMetrics": {
			"models": {},
			"tools": {"totalCalls": 0},
			"files": {}
		}
	}`

	var summary VeCLISessionSummary
	err := json.Unmarshal([]byte(raw), &summary)
	require.NoError(t, err)

	meta := summary.extractMetadata("fallback-model")
	assert.Equal(t, "fallback-model", meta.Model, "should use request model as fallback")
	assert.Equal(t, 0, meta.InputTokens)
	assert.Equal(t, 0, meta.OutputTokens)
}

// --- parseVeCLISessionSummary additional tests ---

func TestParseVeCLISessionSummary_EmptyObject(t *testing.T) {
	summary, err := parseVeCLISessionSummary([]byte("{}"))
	require.NoError(t, err)
	assert.NotNil(t, summary)
	assert.Empty(t, summary.SessionMetrics.Models)
}

func TestParseVeCLISessionSummary_MultipleModels(t *testing.T) {
	raw := `{
		"sessionMetrics": {
			"models": {
				"model-a": {
					"api": {"totalRequests": 1, "totalErrors": 0, "totalLatencyMs": 100},
					"tokens": {"prompt": 100, "candidates": 50, "total": 150, "cached": 0, "thoughts": 0, "tool": 0}
				},
				"model-b": {
					"api": {"totalRequests": 2, "totalErrors": 0, "totalLatencyMs": 200},
					"tokens": {"prompt": 200, "candidates": 100, "total": 300, "cached": 0, "thoughts": 0, "tool": 0}
				}
			},
			"tools": {"totalCalls": 0},
			"files": {}
		}
	}`

	summary, err := parseVeCLISessionSummary([]byte(raw))
	require.NoError(t, err)
	assert.Len(t, summary.SessionMetrics.Models, 2)
}

// --- extractMetadata additional tests ---

func TestVeCLISessionSummary_ExtractMetadata_MatchingModel(t *testing.T) {
	raw := `{
		"sessionMetrics": {
			"models": {
				"kimi-k2": {
					"api": {"totalRequests": 1, "totalErrors": 0, "totalLatencyMs": 500},
					"tokens": {"prompt": 300, "candidates": 150, "total": 450, "cached": 0, "thoughts": 0, "tool": 0}
				},
				"other-model": {
					"api": {"totalRequests": 2, "totalErrors": 0, "totalLatencyMs": 800},
					"tokens": {"prompt": 500, "candidates": 250, "total": 750, "cached": 0, "thoughts": 0, "tool": 0}
				}
			},
			"tools": {"totalCalls": 0},
			"files": {}
		}
	}`

	var summary VeCLISessionSummary
	require.NoError(t, json.Unmarshal([]byte(raw), &summary))

	meta := summary.extractMetadata("kimi-k2")
	assert.Equal(t, "kimi-k2", meta.Model)
	assert.Equal(t, 300, meta.InputTokens)
	assert.Equal(t, 150, meta.OutputTokens)
	assert.Equal(t, 500, meta.DurationMs)
}

func TestVeCLISessionSummary_ExtractMetadata_FallbackToFirst(t *testing.T) {
	raw := `{
		"sessionMetrics": {
			"models": {
				"some-model": {
					"api": {"totalRequests": 1, "totalErrors": 0, "totalLatencyMs": 300},
					"tokens": {"prompt": 100, "candidates": 50, "total": 150, "cached": 0, "thoughts": 0, "tool": 0}
				}
			},
			"tools": {"totalCalls": 0},
			"files": {}
		}
	}`

	var summary VeCLISessionSummary
	require.NoError(t, json.Unmarshal([]byte(raw), &summary))

	meta := summary.extractMetadata("nonexistent-model")
	assert.Equal(t, "some-model", meta.Model, "should fall back to first model entry")
	assert.Equal(t, 100, meta.InputTokens)
}

func TestVeCLISessionSummary_ExtractMetadata_NoModelsNoReqModel(t *testing.T) {
	var summary VeCLISessionSummary
	require.NoError(t, json.Unmarshal([]byte(`{"sessionMetrics":{"models":{},"tools":{},"files":{}}}`), &summary))

	meta := summary.extractMetadata("")
	assert.Equal(t, "", meta.Model, "should be empty when no models and no request model")
}

// --- VeCLIBackend additional tests ---

func TestVeCLIBackend_vecliPreStart(t *testing.T) {
	b := NewVeCLIBackend()
	cmd := &exec.Cmd{Args: []string{"vecli"}}
	req := ChatRequest{SessionID: "test-session-123"}

	b.vecliPreStart(cmd, req)

	assert.Contains(t, cmd.Args, "--session-summary")
	idx := indexOfArg(cmd.Args, "--session-summary")
	require.GreaterOrEqual(t, idx, 0)
	assert.Contains(t, cmd.Args[idx+1], "test-session-123.json", "summary filename should include session ID")

	v, ok := b.summaryMap.Load("test-session-123")
	assert.True(t, ok, "summaryMap should contain session ID")
	assert.Contains(t, v.(string), "test-session-123.json")

	b.summaryMap.LoadAndDelete("test-session-123")
}

func TestVeCLIBackend_Name(t *testing.T) {
	b := NewVeCLIBackend()
	assert.Equal(t, "vecli", b.Name())
}

// indexOfArg returns the index of the first occurrence of target in slice, or -1.
func indexOfArg(slice []string, target string) int {
	for i, v := range slice {
		if v == target {
			return i
		}
	}
	return -1
}
