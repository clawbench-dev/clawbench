package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// AgentSelfTestCheck is one assertion in the agent integration self-test.
//
// The endpoint answers "would this agent actually work?" without spending a
// single model token: every check inspects persisted state, the registry, or the
// filesystem. A real prompt is deliberately NOT sent — it would be slow, cost
// money, and (for an ACP agent) spawn a process just to answer a health check.
// The one check that does touch the outside world is the binary/PATH probe,
// which is what actually breaks in practice.
type AgentSelfTestCheck struct {
	// Key is a stable machine-readable id (also the i18n key suffix on the
	// client), e.g. "backend_known".
	Key string `json:"key"`
	// Status is "ok" | "warn" | "fail".
	Status string `json:"status"`
	// Detail is the human-readable explanation. For a failure it names the
	// concrete value that is wrong (command, backend, model id) so the user can
	// act without reading logs.
	Detail string `json:"detail,omitempty"`
}

// AgentSelfTestResult is the JSON response for POST /api/agents/{id}/self-test.
type AgentSelfTestResult struct {
	AgentID string `json:"agentId"`
	// OK is true when no check failed. Warnings do not flip it: they describe a
	// degraded-but-usable state (e.g. an ACP agent whose runtime has not
	// reported its models yet, which is normal before the first turn).
	OK     bool                 `json:"ok"`
	Passed int                  `json:"passed"`
	Failed int                  `json:"failed"`
	Warned int                  `json:"warned"`
	Checks []AgentSelfTestCheck `json:"checks"`
}

const (
	selfTestOK   = "ok"
	selfTestWarn = "warn"
	selfTestFail = "fail"
	// transportCLI mirrors the stored transport value. transportACP already
	// exists in session_sync.go for the ACP side.
	transportCLI = "cli"
)

// Check keys. Constants (not literals) because they are also the client's i18n
// key suffixes, so a typo would silently render the raw key in the UI.
const (
	selfTestKeyEnabled        = "enabled"
	selfTestKeyProjectScope   = "project_scope"
	selfTestKeyBackendKnown   = "backend_known"
	selfTestKeyTransport      = "transport"
	selfTestKeyAcpCommand     = "acp_command"
	selfTestKeyCliCommand     = "cli_command"
	selfTestKeyModels         = "models"
	selfTestKeyPreferredModel = "preferred_model"
	selfTestKeyAcpState       = "acp_state"
	selfTestKeyPreferredMode  = "preferred_mode"
	selfTestKeySystemPrompt   = "system_prompt"
	selfTestKeyUsage          = "usage"
)

// ServeAgentSelfTest handles POST /api/agents/{id}/self-test.
//
// It is the backend behind the settings page's "run integration test" button.
// The checks run from "is this agent even a thing" to "can it spawn", so the
// first failure already tells the user where the problem is.
func ServeAgentSelfTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/self-test")
	if !isValidAgentID(agentID) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.RLock()
	agent := model.GetAgent(agentID)
	configMutex.RUnlock()
	if agent == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	writeJSON(w, http.StatusOK, runAgentSelfTest(r.Context(), agent, projectPath))
}

// selfTestRun accumulates checks and the derived counters.
//
// A tiny accumulator instead of a bare slice keeps the "passed/failed/warned
// must sum to len(checks)" invariant in one place: the client renders the totals
// next to the rows, so a miscount would contradict what the user can see.
type selfTestRun struct {
	res AgentSelfTestResult
}

func (s *selfTestRun) add(c AgentSelfTestCheck) {
	switch c.Status {
	case selfTestFail:
		s.res.Failed++
	case selfTestWarn:
		s.res.Warned++
	default:
		s.res.Passed++
	}
	s.res.Checks = append(s.res.Checks, c)
}

func (s *selfTestRun) ok(key string) { s.add(AgentSelfTestCheck{Key: key, Status: selfTestOK}) }

func (s *selfTestRun) warn(key, detail string) {
	s.add(AgentSelfTestCheck{Key: key, Status: selfTestWarn, Detail: detail})
}

func (s *selfTestRun) fail(key, detail string) {
	s.add(AgentSelfTestCheck{Key: key, Status: selfTestFail, Detail: detail})
}

// effectiveTransport resolves the transport the turn would actually use.
func effectiveTransport(agent *model.Agent) string {
	if agent.Transport != "" {
		return agent.Transport
	}
	if agent.AcpCommand != "" {
		return transportACP
	}
	return transportCLI
}

// runAgentSelfTest evaluates every check and aggregates the outcome.
//
// Split from the HTTP handler so the rules are unit-testable without a request,
// and so a future caller (a startup diagnostic, /cb-agent) can reuse them.
// Broken into one helper per concern to keep each branch shallow enough to read.
func runAgentSelfTest(ctx context.Context, agent *model.Agent, projectPath string) AgentSelfTestResult {
	run := &selfTestRun{res: AgentSelfTestResult{AgentID: agent.ID}}
	transport := effectiveTransport(agent)
	hasCLI := model.BackendSupportsCLI(agent.Backend)

	checkEnabled(run, agent)
	checkProjectScope(run, agent, projectPath)
	checkBackendKnown(run, agent, hasCLI)
	checkTransport(run, agent, transport, hasCLI)
	checkSpawnTarget(ctx, run, agent, transport)
	checkModels(run, agent, transport)
	checkAcpState(run, agent, transport)
	checkPreferredMode(run, agent, transport)
	checkSystemPrompt(run, agent)
	checkUsage(run, agent)

	run.res.OK = run.res.Failed == 0
	return run.res
}

// checkEnabled reports a disabled agent as a warning: it is switched off on
// purpose, which is not the same as broken.
func checkEnabled(run *selfTestRun, agent *model.Agent) {
	if agent.Disabled {
		run.warn(selfTestKeyEnabled, "This agent is disabled and will not appear in any picker.")
		return
	}
	run.ok(selfTestKeyEnabled)
}

// checkProjectScope reports an agent that cannot be used from this project.
func checkProjectScope(run *selfTestRun, agent *model.Agent, projectPath string) {
	if projectPath != "" && !agentVisibleInProject(agent, projectPath) {
		run.fail(selfTestKeyProjectScope, "This agent is limited to another project and cannot run here.")
		return
	}
	run.ok(selfTestKeyProjectScope)
}

// checkBackendKnown reports a backend the runtime has no way to construct.
//
// The real question is "can anything start this agent?", and three things can:
// a registered spec (drives discovery), a CLI factory (drives CLI spawn), or a
// stored ACP command (drives ACP spawn — NewACPBackend only needs the command).
//
// That third case is why the check must not be "is the backend in the registry".
// A YAML-defined agent (the E2E acp-mock) and a user-created custom agent both
// have no registry entry and are perfectly valid; calling them "unknown" would
// make the test cry wolf on exactly the agents that work fine.
func checkBackendKnown(run *selfTestRun, agent *model.Agent, hasCLI bool) {
	switch {
	case model.FindSpecByBackend(agent.Backend) != nil || hasCLI:
		run.ok(selfTestKeyBackendKnown)
	case agent.AcpCommand != "":
		run.add(AgentSelfTestCheck{
			Key: selfTestKeyBackendKnown, Status: selfTestOK,
			Detail: fmt.Sprintf("No registry entry for backend %q, but it has an ACP command, which is all the ACP transport needs.", agent.Backend),
		})
	default:
		run.fail(selfTestKeyBackendKnown,
			fmt.Sprintf("Unknown backend %q: it has no registry spec, no CLI factory and no ACP command.", agent.Backend))
	}
}

// checkTransport reports a transport the agent cannot actually run on.
func checkTransport(run *selfTestRun, agent *model.Agent, transport string, hasCLI bool) {
	switch {
	case transport == transportACP && agent.AcpCommand == "":
		run.fail(selfTestKeyTransport,
			"Transport is acp-stdio but no ACP command is configured, so nothing can be spawned.")
	case transport != transportACP && !hasCLI:
		run.fail(selfTestKeyTransport,
			fmt.Sprintf("Transport is cli but backend %q has no CLI implementation.", agent.Backend))
	default:
		run.add(AgentSelfTestCheck{
			Key: selfTestKeyTransport, Status: selfTestOK,
			Detail: "Effective transport: " + transport,
		})
	}
}

// checkSpawnTarget verifies the executable the agent would spawn is resolvable.
// This is the check that catches the most common real-world breakage: a command
// that was renamed, moved, or never installed.
func checkSpawnTarget(ctx context.Context, run *selfTestRun, agent *model.Agent, transport string) {
	if transport == transportACP {
		run.add(probeCommand(ctx, selfTestKeyAcpCommand, agent.AcpCommand))
		return
	}
	cmd := cliProbeCommand(agent, model.FindSpecByBackend(agent.Backend))
	if cmd == "" {
		run.warn(selfTestKeyCliCommand,
			"No CLI binary name is known for this backend, so the install could not be verified.")
		return
	}
	run.add(probeCommand(ctx, selfTestKeyCliCommand, cmd))
}

// checkModels reports the model list. Empty is normal for an ACP agent before
// its first turn (the agent reports them at session/new), so it is a warning
// there and a failure only for a CLI agent that should have discovered some.
func checkModels(run *selfTestRun, agent *model.Agent, transport string) {
	if len(agent.Models) > 0 {
		run.add(AgentSelfTestCheck{
			Key: selfTestKeyModels, Status: selfTestOK,
			Detail: fmt.Sprintf("%d model(s) available.", len(agent.Models)),
		})
		checkPreferredModel(run, agent)
		return
	}
	if transport == transportACP {
		run.warn(selfTestKeyModels,
			"No models reported yet. An ACP agent reports them on its first session; send one message to populate the list.")
		return
	}
	run.fail(selfTestKeyModels,
		"No models available: the CLI discovery returned nothing, so a turn cannot pick a model.")
}

// checkPreferredModel flags a preference that would be silently dropped at turn
// time because the agent no longer offers that model.
func checkPreferredModel(run *selfTestRun, agent *model.Agent) {
	if agent.PreferredModel != "" && !agentHasModel(agent, agent.PreferredModel) {
		run.fail(selfTestKeyPreferredModel,
			fmt.Sprintf("Preferred model %q is not in this agent's model list.", agent.PreferredModel))
		return
	}
	run.ok(selfTestKeyPreferredModel)
}

// checkAcpState reports what the last ACP handshake taught us. Absence is a
// warning: capabilities only exist after the first connection.
func checkAcpState(run *selfTestRun, agent *model.Agent, transport string) {
	if transport != transportACP {
		return
	}
	reg := ai.GetAgentCapabilityRegistry()
	if agentCap := reg.Get(agent.ID); agentCap != nil && agentCap.HasData() {
		run.add(AgentSelfTestCheck{
			Key: selfTestKeyAcpState, Status: selfTestOK,
			Detail: fmt.Sprintf("Last handshake reported %d mode(s), %d model(s), %d command(s).",
				len(agentCap.AvailableModes), len(agentCap.AvailableModels), len(agentCap.AvailableCommands)),
		})
		return
	}
	run.warn(selfTestKeyAcpState,
		"No ACP handshake recorded yet — capabilities appear after the first connection.")
}

// checkPreferredMode flags a mode the agent no longer advertises.
func checkPreferredMode(run *selfTestRun, agent *model.Agent, transport string) {
	if transport != transportACP || agent.PreferredMode == "" {
		return
	}
	reg := ai.GetAgentCapabilityRegistry()
	if reg.HasAvailableModes(agent.ID) && !reg.IsModeAvailable(agent.ID, agent.PreferredMode) {
		run.warn(selfTestKeyPreferredMode,
			fmt.Sprintf("Preferred mode %q is not offered by this agent any more.", agent.PreferredMode))
		return
	}
	run.ok(selfTestKeyPreferredMode)
}

// checkSystemPrompt flags an agent that would run with no instructions at all.
func checkSystemPrompt(run *selfTestRun, agent *model.Agent) {
	if strings.TrimSpace(agent.RuntimeSystemPrompt) == "" {
		run.warn(selfTestKeySystemPrompt, "No system prompt is composed for this agent.")
		return
	}
	run.ok(selfTestKeySystemPrompt)
}

// checkUsage is informational: it tells the user whether the agent under test
// has real conversations behind it.
func checkUsage(run *selfTestRun, agent *model.Agent) {
	usage, err := service.GetAgentUsage(agent.ID)
	if err != nil {
		return
	}
	run.add(AgentSelfTestCheck{
		Key: selfTestKeyUsage, Status: selfTestOK,
		Detail: fmt.Sprintf("%d session(s), %d task(s), %d group membership(s).",
			usage.Sessions, usage.Tasks, usage.Memberships),
	})
}

// cliProbeCommand picks the binary to verify for a CLI-transport agent: the
// agent's own override wins (that is what spawn uses), then the spec default.
func cliProbeCommand(agent *model.Agent, spec *model.BackendSpec) string {
	if cmd := strings.TrimSpace(agent.Command); cmd != "" {
		if fields := strings.Fields(cmd); len(fields) > 0 {
			return fields[0]
		}
	}
	if spec != nil {
		return spec.DefaultCmd
	}
	return ""
}

// probeCommand verifies the executable named by a command line is resolvable.
//
// Uses the same split rule as the spawn path (strings.Fields) so a command that
// would fail to spawn cannot pass the test by looking correct. Only the FIRST
// token is probed: the rest are arguments, and whether the agent accepts them is
// exactly what a real turn would tell you.
func probeCommand(ctx context.Context, key, commandLine string) AgentSelfTestCheck {
	fields := strings.Fields(strings.TrimSpace(commandLine))
	if len(fields) == 0 {
		return AgentSelfTestCheck{
			Key: key, Status: selfTestFail,
			Detail: "No command configured, so the agent cannot be started.",
		}
	}

	// The probe has its own internal timeout; the derived context is used to
	// notice an already-cancelled request, so an abandoned test does not report
	// a green result.
	if ctx.Err() != nil {
		return AgentSelfTestCheck{
			Key: key, Status: selfTestFail,
			Detail: "The check was cancelled before it finished.",
		}
	}
	if err := model.CheckCLIExistsErr(fields[0]); err != nil {
		return AgentSelfTestCheck{
			Key: key, Status: selfTestFail,
			Detail: fmt.Sprintf("Executable %q was not found: %v", fields[0], err),
		}
	}
	return AgentSelfTestCheck{
		Key: key, Status: selfTestOK,
		Detail: fmt.Sprintf("Executable %q is available.", fields[0]),
	}
}

// agentHasModel reports whether id appears in the agent's model list.
func agentHasModel(agent *model.Agent, id string) bool {
	for _, m := range agent.Models {
		if m.ID == id {
			return true
		}
	}
	// The ACP runtime can report models the CLI discovery does not know about;
	// the preferred-model guard accepts those too, so the self-test must not
	// contradict it.
	reg := ai.GetAgentCapabilityRegistry()
	if mls := reg.GetModelListState(agent.ID, ""); mls != nil {
		for _, m := range mls.Models {
			if m.ID == id {
				return true
			}
		}
	}
	return false
}
