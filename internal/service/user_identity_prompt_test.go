package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// setUserIdentity pins the two runtime globals for one test and restores them.
func setUserIdentity(t *testing.T, nickname, profile string) {
	t.Helper()
	origNick, origProfile := model.UserNickname, model.UserProfile
	model.UserNickname, model.UserProfile = nickname, profile
	t.Cleanup(func() { model.UserNickname, model.UserProfile = origNick, origProfile })
}

// The default placeholder name alone is not information: injecting it would put
// a meaningless line in every prompt of every install that never configured a
// nickname.
func TestBuildUserIdentitySection_DefaultNameAloneIsEmpty(t *testing.T) {
	setUserIdentity(t, model.DefaultUserNickname, "")
	assert.Empty(t, BuildUserIdentitySection())
}

func TestBuildUserIdentitySection_NicknameOnly(t *testing.T) {
	setUserIdentity(t, "老板", "")
	section := BuildUserIdentitySection()

	assert.Contains(t, section, "## About the User")
	assert.Contains(t, section, "The user's name is 老板.")
	assert.NotContains(t, section, "describes themselves", "no profile means no profile block")
}

func TestBuildUserIdentitySection_ProfileOnly(t *testing.T) {
	setUserIdentity(t, model.DefaultUserNickname, "后端工程师，偏好简洁回答")
	section := BuildUserIdentitySection()

	assert.Contains(t, section, "## About the User")
	assert.Contains(t, section, "后端工程师，偏好简洁回答")
	assert.NotContains(t, section, "The user's name is", "the default placeholder is not a name")
}

func TestBuildUserIdentitySection_TrimsAndCarriesBoth(t *testing.T) {
	setUserIdentity(t, "  老板  ", "\n  做后端，喜欢直接给结论  \n")
	section := BuildUserIdentitySection()

	assert.Contains(t, section, "The user's name is 老板.")
	assert.Contains(t, section, "做后端，喜欢直接给结论")
	assert.NotContains(t, section, "  \n", "surrounding whitespace must be trimmed")
}

func TestAppendUserIdentitySection_NoopWhenEmpty(t *testing.T) {
	setUserIdentity(t, model.DefaultUserNickname, "")
	assert.Equal(t, "base prompt", AppendUserIdentitySection("base prompt"))
	assert.Equal(t, "", AppendUserIdentitySection(""))
}

func TestAppendUserIdentitySection_AppendsWithBlankLine(t *testing.T) {
	setUserIdentity(t, "老板", "做后端")
	out := AppendUserIdentitySection("base prompt")

	assert.True(t, strings.HasPrefix(out, "base prompt\n\n## About the User"),
		"the section must be appended after a blank line, got %q", out)
	// An empty base prompt still yields a usable section (no leading blank lines).
	assert.True(t, strings.HasPrefix(AppendUserIdentitySection(""), "## About the User"))
}

// A chat turn must carry the identity in the request it builds — not merely in a
// helper nobody calls.
func TestResolveAgentConfig_InjectsUserIdentity(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"identity-agent": {
			ID:                  "identity-agent",
			Name:                "Agent",
			Backend:             "claude",
			RuntimeSystemPrompt: "BASE PROMPT",
		},
	}
	t.Cleanup(func() { model.Agents = origAgents })
	setUserIdentity(t, "老板", "做后端，偏好简洁回答")

	systemPrompt, _, _, _, _ := resolveAgentConfig("identity-agent", "", "", "", "", "", false)

	assert.Contains(t, systemPrompt, "BASE PROMPT")
	assert.Contains(t, systemPrompt, "## About the User")
	assert.Contains(t, systemPrompt, "The user's name is 老板.")
	assert.Contains(t, systemPrompt, "做后端，偏好简洁回答")
}

// Both system-prompt producers must call the injector. Scheduled tasks bypass
// BuildChatRequest entirely, so a helper wired only into the chat path would
// silently leave every scheduled execution without the user's identity.
//
// A source guard (rather than a behavioral one) because the scheduler path
// builds its request inside a large function that is expensive to drive here;
// the contract is exactly "this call site exists".
func TestUserIdentity_WiredIntoBothSystemPromptProducers(t *testing.T) {
	for _, file := range []string{"chat_request.go", "scheduler.go"} {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Contains(t, string(src), "AppendUserIdentitySection(",
			"%s must inject the user identity into the system prompt", file)
	}

	// Group member turns flip Resume AFTER BuildChatRequest, so they must
	// re-evaluate which carrier gets the identity.
	src, err := os.ReadFile("group_member_request.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "EnsureUserIdentityInPrompt(",
		"applyMemberResumeOverrides must re-evaluate the identity carrier after changing Resume")
}

// The helper decides between the two carriers and must be idempotent: the
// group path calls it twice (BuildChatRequest, then after resume overrides).
func TestEnsureUserIdentityInPrompt(t *testing.T) {
	setUserIdentity(t, "老板", "做后端")

	// System prompt will be sent → prompt untouched.
	fresh := ai.ChatRequest{Prompt: "hello", SystemPrompt: "BASE"}
	EnsureUserIdentityInPrompt(&fresh)
	assert.Equal(t, "hello", fresh.Prompt)

	// Resumed → the prompt carries it, exactly once even if called twice.
	resumed := ai.ChatRequest{Prompt: "hello", SystemPrompt: "BASE", Resume: true, AssistantMessageCount: 1}
	EnsureUserIdentityInPrompt(&resumed)
	EnsureUserIdentityInPrompt(&resumed)
	assert.Equal(t, 1, strings.Count(resumed.Prompt, "## About the User"))
	assert.True(t, strings.HasPrefix(resumed.Prompt, "## About the User"))
	assert.True(t, strings.HasSuffix(resumed.Prompt, "hello"))

	// Slash commands keep their leading "/".
	slash := ai.ChatRequest{Prompt: "/compact", SystemPrompt: "BASE", Resume: true, AssistantMessageCount: 1}
	EnsureUserIdentityInPrompt(&slash)
	assert.Equal(t, "/compact", slash.Prompt)
}

// Group member turns mutate Resume AFTER BuildChatRequest (they resume the
// member's own session), so that function must re-evaluate the carrier.
func TestApplyMemberResumeOverrides_CarriesIdentityIntoPrompt(t *testing.T) {
	setUserIdentity(t, "老板", "做后端")

	req := ai.ChatRequest{Prompt: "hello", SystemPrompt: "BASE"}
	applyMemberResumeOverrides(&req, "ext-1", true)

	require.True(t, req.Resume)
	assert.True(t, strings.HasPrefix(req.Prompt, "## About the User"),
		"a resumed member turn must carry the identity in the prompt, got %q", req.Prompt)
}

// A resumed session does not re-send the system prompt, so the identity must
// ride the prompt on those turns — otherwise a profile edited mid-session would
// not reach the agent until a compaction. On a fresh session it must NOT be in
// the prompt (that would duplicate the system-prompt copy).
func TestBuildChatRequest_UserIdentityFollowsInjectionPoint(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"identity-agent": {
			ID:                  "identity-agent",
			Name:                "Agent",
			Backend:             "codebuddy",
			RuntimeSystemPrompt: "BASE PROMPT",
		},
	}
	t.Cleanup(func() { model.Agents = origAgents })
	setUserIdentity(t, "老板", "做后端，偏好简洁回答")

	// New session: the system prompt is sent, so the identity lives there.
	fresh := BuildChatRequest("hello", "sess-identity-new", "/proj", "codebuddy", "identity-agent", "", "", "", "", "/proj", false)
	assert.Contains(t, fresh.SystemPrompt, "## About the User")
	assert.NotContains(t, fresh.Prompt, "## About the User",
		"the prompt must not duplicate what the system prompt already carries")

	// A session only counts as resumable when it already has an assistant
	// message — BuildChatRequest refuses to "resume" an empty one.
	seedResumable := func(sessionID string) {
		t.Helper()
		_, err := db.Exec(
			`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type) VALUES (?, 1, 'codebuddy', 'T', 'identity-agent', 'chat')`,
			sessionID,
		)
		require.NoError(t, err)
		_, err = db.Exec(
			`INSERT INTO chat_history (project_id, session_id, role, content, backend) VALUES (1, ?, 'assistant', '{"blocks":[{"type":"text","text":"hello"}]}', 'codebuddy')`,
			sessionID,
		)
		require.NoError(t, err)
	}

	seedResumable("sess-identity-resume")
	resumed := BuildChatRequest("hello", "sess-identity-resume", "/proj", "codebuddy", "identity-agent", "", "", "", "", "/proj", true)
	require.True(t, resumed.Resume)
	assert.True(t, strings.HasPrefix(resumed.Prompt, "## About the User"),
		"a resumed turn must carry the identity in the prompt, got %q", resumed.Prompt)
	assert.Contains(t, resumed.Prompt, "做后端，偏好简洁回答")
	assert.Contains(t, resumed.Prompt, "hello", "the user's message must survive the prefix")

	// Slash commands must keep their leading "/" or the ACP dispatcher will
	// route them to the LLM instead of the command executor.
	seedResumable("sess-identity-slash")
	slash := BuildChatRequest("/compact", "sess-identity-slash", "/proj", "codebuddy", "identity-agent", "", "", "", "", "/proj", true)
	require.True(t, slash.Resume, "the slash-command case must actually exercise the resumed path")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(slash.Prompt), "/compact"),
		"a slash command must not be prefixed, got %q", slash.Prompt)
}
