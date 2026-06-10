package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- deepseekBackend field tests ---

func TestDeepSeekBackend_Name(t *testing.T) {
	assert.Equal(t, "deepseek", deepseekBackend.Name())
}

func TestDeepSeekBackend_DefaultCommand(t *testing.T) {
	assert.Equal(t, "deepseek", deepseekBackend.defaultCommand)
}

func TestDeepSeekBackend_Fields(t *testing.T) {
	assert.NotNil(t, deepseekBackend.buildArgs, "buildArgs should be set")
	assert.NotNil(t, deepseekBackend.newParser, "newParser should be set")
	assert.Nil(t, deepseekBackend.filterLine, "filterLine should be nil (skip empty lines only)")
	assert.Nil(t, deepseekBackend.preStart, "preStart should be nil (prompt is positional arg)")
}

func TestDeepSeekBackend_ParserType(t *testing.T) {
	parser := deepseekBackend.newParser()
	assert.IsType(t, &DeepSeekStreamParser{}, parser, "parser should be DeepSeekStreamParser")
}

// --- buildDeepSeekStreamArgs tests via deepseekBackend.buildArgs ---

func TestDeepSeekBackend_BuildArgs_BasicPrompt(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "what is 1+1?"})
	assert.Equal(t, []string{
		"exec", "--auto", "--output-format", "stream-json",
		"what is 1+1?",
	}, args)
}

func TestDeepSeekBackend_BuildArgs_WithModel(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "hello", Model: "deepseek-v4-pro"})
	assert.Contains(t, args, "--model")
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0, "--model flag should exist")
	assert.Equal(t, "deepseek-v4-pro", args[idx+1], "model value should follow --model flag")
}

func TestDeepSeekBackend_BuildArgs_ProviderPrefixStripped(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "hello", Model: "deepseek/deepseek-v4-pro"})
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "deepseek-v4-pro", args[idx+1], "provider prefix should be stripped")
}

func TestDeepSeekBackend_BuildArgs_NestedProviderPrefix(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "hello", Model: "a/b/deepseek-v4-flash"})
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "deepseek-v4-flash", args[idx+1], "all prefix segments before last / should be stripped")
}

func TestDeepSeekBackend_BuildArgs_NoModel(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "hello"})
	assert.NotContains(t, args, "--model", "--model should not appear when Model is empty")
}

func TestDeepSeekBackend_BuildArgs_ResumeWithSessionID(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt:    "continue",
		SessionID: "4bf83f0f-a9b6-47b4",
		Resume:    true,
	})
	idx := indexOf(args, "--resume")
	require.GreaterOrEqual(t, idx, 0, "--resume flag should exist")
	assert.Equal(t, "4bf83f0f-a9b6-47b4", args[idx+1], "session ID should follow --resume")
	assert.NotContains(t, args, "--continue", "--continue should not appear when --resume is used")
}

func TestDeepSeekBackend_BuildArgs_ResumeWithoutSessionID_ContinueFallback(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt: "keep going",
		Resume: true,
	})
	assert.Contains(t, args, "--continue", "--continue should appear as fallback when Resume=true but SessionID is empty")
	assert.NotContains(t, args, "--resume", "--resume should not appear without SessionID")
}

func TestDeepSeekBackend_BuildArgs_ResumeFalse_NoResumeFlags(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt:    "new chat",
		SessionID: "some-id",
		Resume:    false,
	})
	assert.NotContains(t, args, "--resume", "--resume should not appear when Resume=false")
	assert.NotContains(t, args, "--continue", "--continue should not appear when Resume=false")
}

func TestDeepSeekBackend_BuildArgs_WithSystemPrompt(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt:       "review code",
		SystemPrompt: "You are a code reviewer.",
	})
	idx := indexOf(args, "--system-prompt")
	require.GreaterOrEqual(t, idx, 0, "--system-prompt flag should exist")
	assert.Equal(t, "You are a code reviewer.", args[idx+1])
}

func TestDeepSeekBackend_BuildArgs_NoSystemPrompt(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: "hello"})
	assert.NotContains(t, args, "--system-prompt", "--system-prompt should not appear when SystemPrompt is empty")
}

func TestDeepSeekBackend_BuildArgs_FullRequest(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt:       "explain this",
		Model:        "deepseek/deepseek-v4-flash",
		SessionID:    "session-abc",
		Resume:       true,
		SystemPrompt: "Respond in Chinese",
	})

	// Verify base args
	assert.Equal(t, "exec", args[0])
	assert.Equal(t, "--auto", args[1])
	assert.Equal(t, "--output-format", args[2])
	assert.Equal(t, "stream-json", args[3])

	// Verify --resume with session ID
	assert.Contains(t, args, "--resume")
	idx := indexOf(args, "--resume")
	assert.Equal(t, "session-abc", args[idx+1])

	// Verify --system-prompt
	assert.Contains(t, args, "--system-prompt")
	idx = indexOf(args, "--system-prompt")
	assert.Equal(t, "Respond in Chinese", args[idx+1])

	// Verify --model with provider prefix stripped
	assert.Contains(t, args, "--model")
	idx = indexOf(args, "--model")
	assert.Equal(t, "deepseek-v4-flash", args[idx+1])

	// Verify prompt is the last arg
	assert.Equal(t, "explain this", args[len(args)-1])
}

func TestDeepSeekBackend_BuildArgs_ArgOrder(t *testing.T) {
	// Verify argument ordering: base → resume/continue → system-prompt → model → prompt
	args := deepseekBackend.buildArgs(ChatRequest{
		Prompt:       "test prompt",
		Model:        "deepseek-v4-flash",
		SessionID:    "sid-123",
		Resume:       true,
		SystemPrompt: "sys",
	})

	idxExec := indexOf(args, "exec")
	idxResume := indexOf(args, "--resume")
	idxSysPrompt := indexOf(args, "--system-prompt")
	idxModel := indexOf(args, "--model")
	promptIdx := len(args) - 1

	assert.Less(t, idxExec, idxResume, "exec should come before --resume")
	assert.Less(t, idxResume, idxSysPrompt, "--resume should come before --system-prompt")
	assert.Less(t, idxSysPrompt, idxModel, "--system-prompt should come before --model")
	assert.Less(t, idxModel, promptIdx, "--model should come before prompt")
	assert.Equal(t, "test prompt", args[promptIdx], "prompt should be the last argument")
}

func TestDeepSeekBackend_BuildArgs_EmptyPrompt(t *testing.T) {
	args := deepseekBackend.buildArgs(ChatRequest{Prompt: ""})
	assert.Equal(t, "", args[len(args)-1], "empty prompt should still be appended as last arg")
}

// --- buildDeepSeekStreamArgs edge cases ---

func TestBuildDeepSeekStreamArgs_SingleSlashModel(t *testing.T) {
	// Model with exactly one slash: "provider/model"
	args := buildDeepSeekStreamArgs(ChatRequest{Prompt: "hi", Model: "deepseek/deepseek-v4-pro"})
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "deepseek-v4-pro", args[idx+1])
}

func TestBuildDeepSeekStreamArgs_ModelNoSlash(t *testing.T) {
	// Model without any slash: "deepseek-v4-pro"
	args := buildDeepSeekStreamArgs(ChatRequest{Prompt: "hi", Model: "deepseek-v4-pro"})
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "deepseek-v4-pro", args[idx+1])
}

func TestBuildDeepSeekStreamArgs_TrailingSlashModel(t *testing.T) {
	// Edge: model ending with slash → LastIndex finds it, substring after is empty
	args := buildDeepSeekStreamArgs(ChatRequest{Prompt: "hi", Model: "provider/"})
	idx := indexOf(args, "--model")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "", args[idx+1], "model after trailing slash should be empty string")
}
