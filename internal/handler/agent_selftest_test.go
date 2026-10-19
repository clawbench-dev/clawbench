package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findCheck returns the check with the given key, or nil.
func findCheck(res AgentSelfTestResult, key string) *AgentSelfTestCheck {
	for i := range res.Checks {
		if res.Checks[i].Key == key {
			return &res.Checks[i]
		}
	}
	return nil
}

// statusOf returns the status of a check, or "" when absent.
func statusOf(res AgentSelfTestResult, key string) string {
	if c := findCheck(res, key); c != nil {
		return c.Status
	}
	return ""
}

// A healthy CLI agent must pass every check. This is the baseline the other
// cases are read against: without it, a self-test that failed everything would
// look "correct" in the failure cases.
func TestAgentSelfTest_HealthyCLIAgentPasses(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := model.GetAgent("claude")
	require.NotNil(t, agent)
	// `go` is guaranteed to exist on the machine running the tests, so the
	// executable probe has a real target instead of depending on a vendor CLI.
	agent.Command = "go"
	t.Cleanup(func() { agent.Command = "claude" })

	res := runAgentSelfTest(context.Background(), agent, "/project")

	assert.True(t, res.OK, "checks: %+v", res.Checks)
	assert.Equal(t, 0, res.Failed)
	assert.Contains(t, statusOf(res, "enabled"), selfTestOK)
	assert.Contains(t, statusOf(res, "transport"), selfTestOK)
	assert.Contains(t, statusOf(res, "cli_command"), selfTestOK)
	assert.Contains(t, statusOf(res, "models"), selfTestOK)
}

// An ACP agent with no command cannot spawn anything: that must be a hard
// failure, not a warning, because every turn would fail at backend creation.
func TestAgentSelfTest_AcpTransportWithoutCommandFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:        "selftest-acp-nocmd",
		Name:      "ACP No Command",
		Backend:   model.CustomACPBackend,
		Transport: "acp-stdio",
		// AcpCommand deliberately empty — the contradictory state.
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.False(t, res.OK)
	assert.Equal(t, selfTestFail, statusOf(res, "transport"))
	assert.Contains(t, findCheck(res, "transport").Detail, "no ACP command")
}

// A CLI-transport agent on a backend with no CLI implementation is likewise
// unspawnable.
func TestAgentSelfTest_CliTransportOnAcpOnlyBackendFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-cli-acponly",
		Name:                "CLI on ACP-only",
		Backend:             "grok", // registered spec, no CLI factory
		Transport:           "cli",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.False(t, res.OK)
	assert.Equal(t, selfTestFail, statusOf(res, "transport"))
	assert.Contains(t, findCheck(res, "transport").Detail, "no CLI implementation")
}

// An unknown backend with neither a spec nor a CLI factory cannot be resolved at
// all — the case a hand-edited database row would produce.
func TestAgentSelfTest_UnknownBackendFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-unknown",
		Name:                "Unknown",
		Backend:             "not-a-backend",
		Transport:           "cli",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.False(t, res.OK)
	assert.Equal(t, selfTestFail, statusOf(res, "backend_known"))
}

// A backend with no registry entry but a working ACP command must NOT be
// reported as an unknown backend: NewACPBackend only needs the command, and both
// YAML-defined agents (the E2E acp-mock) and user-created custom agents live in
// exactly that state. Reporting them as broken would make the test cry wolf on
// the agents that work.
func TestAgentSelfTest_CustomAcpBackendIsKnown(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-custom",
		Name:                "Custom",
		Backend:             model.CustomACPBackend,
		Transport:           "acp-stdio",
		AcpCommand:          "go version",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.Equal(t, selfTestOK, statusOf(res, "backend_known"))
	// `go` resolves, so the command probe passes even though the arguments are
	// nonsense — the probe only verifies the executable, which is the part a
	// health check can answer without spawning.
	assert.Equal(t, selfTestOK, statusOf(res, "acp_command"))
}

// A YAML-defined agent (no registry entry, not the custom-acp backend id) is the
// other real shape of "valid but unregistered" — the E2E acp-mock. It must pass
// for the same reason.
func TestAgentSelfTest_YamlDefinedBackendIsKnown(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-yaml",
		Name:                "YAML Agent",
		Backend:             "acp-mock",
		Transport:           "acp-stdio",
		AcpCommand:          "go version",
		Models:              []model.AgentModel{{ID: "mock-pro", Name: "Mock Pro", Default: true}},
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.Equal(t, selfTestOK, statusOf(res, "backend_known"),
		"an unregistered backend with an ACP command is runnable, not unknown")
	assert.True(t, res.OK, "checks: %+v", res.Checks)
}

// Without a spec, a CLI factory AND an ACP command there is genuinely nothing
// that can start the agent.
func TestAgentSelfTest_BackendWithNothingFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-nothing",
		Name:                "Nothing",
		Backend:             "totally-unknown",
		Transport:           "cli",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.Equal(t, selfTestFail, statusOf(res, "backend_known"))
	assert.Contains(t, findCheck(res, "backend_known").Detail, "no ACP command")
}

// A missing executable is the most common real breakage, so it must be reported
// with the actual name the user typed.
func TestAgentSelfTest_MissingExecutableFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-missing",
		Name:                "Missing",
		Backend:             model.CustomACPBackend,
		Transport:           "acp-stdio",
		AcpCommand:          "definitely-not-installed-acp --stdio",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.False(t, res.OK)
	check := findCheck(res, "acp_command")
	require.NotNil(t, check)
	assert.Equal(t, selfTestFail, check.Status)
	assert.Contains(t, check.Detail, "definitely-not-installed-acp",
		"the detail must name the executable that is missing")
}

// A disabled agent is switched off on purpose: that is a warning, not a failure.
func TestAgentSelfTest_DisabledIsWarningNotFailure(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID:                  "selftest-disabled",
		Name:                "Disabled",
		Backend:             model.CustomACPBackend,
		Transport:           "acp-stdio",
		AcpCommand:          "go version",
		Disabled:            true,
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.Equal(t, selfTestWarn, statusOf(res, "enabled"))
	assert.True(t, res.OK, "a disabled agent is not broken")
	// Other warnings are legitimate here (no handshake yet, no models reported),
	// so assert on this check specifically rather than the total.
	assert.GreaterOrEqual(t, res.Warned, 1)
	assert.Equal(t, 0, res.Failed)
}

// An empty model list is normal for an ACP agent before its first turn but fatal
// for a CLI agent, which should have discovered its models at startup.
func TestAgentSelfTest_EmptyModelsDiffersByTransport(t *testing.T) {
	defer setupAgentTestEnv(t)()

	acpAgent := &model.Agent{
		ID: "selftest-acp-models", Name: "ACP",
		Backend: model.CustomACPBackend, Transport: "acp-stdio",
		AcpCommand: "go version", RuntimeSystemPrompt: "prompt",
	}
	acpRes := runAgentSelfTest(context.Background(), acpAgent, "")
	assert.Equal(t, selfTestWarn, statusOf(acpRes, "models"),
		"an ACP agent reports its models at session/new, so empty is expected early")

	cliAgent := &model.Agent{
		ID: "selftest-cli-models", Name: "CLI",
		Backend: "claude", Transport: "cli",
		Command: "go", RuntimeSystemPrompt: "prompt",
	}
	cliRes := runAgentSelfTest(context.Background(), cliAgent, "")
	assert.Equal(t, selfTestFail, statusOf(cliRes, "models"),
		"a CLI agent with no discovered models cannot pick one for a turn")
}

// A preferred model that is not in the list would be dropped silently at turn
// time, so the self-test must flag it.
func TestAgentSelfTest_PreferredModelMustExist(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{
		ID: "selftest-pref", Name: "Pref",
		Backend: "claude", Transport: "cli",
		Command:             "go",
		Models:              []model.AgentModel{{ID: "model-a", Name: "A", Default: true}},
		PreferredModel:      "model-gone",
		RuntimeSystemPrompt: "prompt",
	}
	res := runAgentSelfTest(context.Background(), agent, "")

	assert.Equal(t, selfTestFail, statusOf(res, "preferred_model"))
	assert.Contains(t, findCheck(res, "preferred_model").Detail, "model-gone")

	// The ACP runtime can report models the CLI list does not know about; the
	// guard accepts those, so the self-test must not contradict it.
	agent.PreferredModel = "runtime-only-model"
	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModels(agent.ID, []model.AgentModel{{ID: "runtime-only-model", Name: "R"}})
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	res = runAgentSelfTest(context.Background(), agent, "")
	assert.Equal(t, selfTestOK, statusOf(res, "preferred_model"))
}

// The HTTP layer must reject a wrong method and an unknown agent rather than
// silently answering OK for something that was never tested.
func TestServeAgentSelfTest_RejectsBadRequests(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// GET is not allowed.
	req := newRequest(t, http.MethodGet, "/api/agents/claude/self-test", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSelfTest, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// Unknown agent.
	req = newRequest(t, http.MethodPost, "/api/agents/ghost/self-test", nil)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeAgentSelfTest, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// The route must be reachable through the shared sub-route dispatcher, not just
// as a direct handler call.
func TestAgentSelfTest_RoutedThroughSubRoutes(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPost, "/api/agents/claude/self-test", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSubRoutes, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res AgentSelfTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, "claude", res.AgentID)
	assert.NotEmpty(t, res.Checks)
	// Counts must add up to the number of checks, otherwise the summary lies.
	assert.Equal(t, len(res.Checks), res.Passed+res.Failed+res.Warned)
	assert.Equal(t, res.Failed == 0, res.OK)
}

// Verify the handler reads the agent under the config lock and therefore cannot
// be fooled by a concurrently deleted agent: the lookup happens once, and a nil
// result is a 404 rather than a panic.
func TestServeAgentSelfTest_DeletedAgentIs404(t *testing.T) {
	defer setupAgentTestEnv(t)()

	require.NoError(t, service.DeleteAgent("claude"))
	model.DeleteAgent("claude")

	req := newRequest(t, http.MethodPost, "/api/agents/claude/self-test", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSubRoutes, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- deep self-test ---

// parseSelfTestReply must read the text out of the persisted content document
// and pull transport/stopReason from the same place, so the deep report reflects
// what the turn DID rather than what the config said it would do.
func TestParseSelfTestReply(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"Hello! I am a mock ACP agent."},` +
		`{"type":"text","text":" second"}],` +
		`"metadata":{"transport":"acp-stdio","stopReason":"end_turn"}}`

	reply, transport, stop := parseSelfTestReply(content)
	assert.Equal(t, "Hello! I am a mock ACP agent. second", reply)
	assert.Equal(t, "acp-stdio", transport)
	assert.Equal(t, "end_turn", stop)
}

// A warning block is not a reply: treating its text as one would let a failed
// turn pass the deep check.
func TestParseSelfTestReply_IgnoresWarningBlocks(t *testing.T) {
	content := `{"blocks":[{"type":"warning","text":"The agent did not run this request"}],` +
		`"metadata":{"transport":"acp-stdio"}}`

	reply, _, _ := parseSelfTestReply(content)
	assert.Empty(t, reply, "a warning must never count as an assistant reply")
}

func TestParseSelfTestReply_MalformedIsEmpty(t *testing.T) {
	reply, transport, stop := parseSelfTestReply("not json")
	assert.Empty(t, reply)
	assert.Empty(t, transport)
	assert.Empty(t, stop)
}

// The deep route must refuse to spend a full turn when the cheap checks already
// failed, and it must report WHY rather than a bare false.
func TestServeAgentSelfTestDeep_RefusesOnStaticFailure(t *testing.T) {
	defer setupAgentTestEnv(t)()
	projectDir := t.TempDir()

	// An agent whose command does not exist: the static check fails on the
	// executable probe, so no turn may be attempted.
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "deep-broken", Name: "Deep Broken",
		Backend: model.CustomACPBackend, Transport: "acp-stdio",
		AcpCommand: "not-installed-anywhere-deep --stdio",
	}))
	model.AddAgent(&model.Agent{
		ID: "deep-broken", Name: "Deep Broken",
		Backend: model.CustomACPBackend, Transport: "acp-stdio",
		AcpCommand:          "not-installed-anywhere-deep --stdio",
		RuntimeSystemPrompt: "prompt",
	})
	t.Cleanup(func() { model.DeleteAgent("deep-broken") })

	req := newRequest(t, http.MethodPost, "/api/agents/deep-broken/self-test-deep", nil)
	withAuthCookie(req, model.SessionToken)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: projectDir})
	w := callHandler(ServeAgentSelfTestDeep, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		OK     bool                 `json:"ok"`
		Error  string               `json:"error"`
		Static *AgentSelfTestResult `json:"static"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.False(t, body.OK)
	assert.Contains(t, body.Error, "configuration checks failed")
	require.NotNil(t, body.Static)
	assert.Greater(t, body.Static.Failed, 0)
}

// Without a project there is nowhere to run a turn.
func TestServeAgentSelfTestDeep_RequiresProject(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPost, "/api/agents/claude/self-test-deep", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSelfTestDeep, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
