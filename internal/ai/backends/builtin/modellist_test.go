package builtin

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// withModellessProvider registers a provider whose curated list is EMPTY, so
// an agent bound to it has only its own pick to report.
func withModellessProvider(t *testing.T, defaultModel string) int64 {
	t.Helper()
	const id = int64(71)
	model.ReplaceProviders([]*model.Provider{{
		ID: id, Name: "modelless-test", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "http://127.0.0.1:1", APIKey: "k",
		DefaultModel: defaultModel,
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })
	return id
}

func addBuiltinAgent(t *testing.T, id string, providerID *int64, preferred string) {
	t.Helper()
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	a := &model.Agent{
		ID: id, Backend: BackendID, BuiltinBackend: true,
		ProviderID: providerID, PreferredModel: preferred,
	}
	model.Agents[id] = a
	t.Cleanup(func() { delete(model.Agents, id) })
}

// TestModelListState_ReportsTheBoundProvidersList pins the whole point of the
// hook: before the first message the picker had nothing to render for the
// built-in agent, because nothing ever writes a model list into the capability
// registry for an in-process backend. The bound provider's curated list is what
// the turn can run, so that is the list.
func TestModelListState_ReportsTheBoundProvidersList(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1")

	state := (&Backend{}).ModelListState(agentID)
	if state == nil {
		t.Fatal("a bound built-in agent must report its provider's models")
	}
	if state.AgentID != agentID {
		t.Fatalf("state must name the agent it was resolved for, got %q", state.AgentID)
	}
	if len(state.Models) != 1 || state.Models[0].ID != "edge-model" {
		t.Fatalf("models = %+v, want the provider's curated list", state.Models)
	}
	if !state.Models[0].Default {
		t.Fatal("the model a turn would run must be flagged default")
	}
	if state.CurrentModelID != "edge-model" {
		t.Fatalf("currentModelId = %q, want the model the turn runs", state.CurrentModelID)
	}
}

// TestModelListState_UnboundAgentHasNoList pins fail-closed: an agent with no
// provider is not known to run anything the user chose, so inventing a list
// would offer models the turn cannot resolve.
func TestModelListState_UnboundAgentHasNoList(t *testing.T) {
	agentID := "builtin-modellist-unbound-" + t.Name()
	addBuiltinAgent(t, agentID, nil, "")

	if state := (&Backend{}).ModelListState(agentID); state != nil {
		t.Fatalf("an unbound agent must report no list, got %+v", state)
	}
}

// TestModelListState_UncuratedProviderEchoesThePick pins the "custom…" escape
// hatch: a provider that publishes no model list can still have an explicit
// pick, and the chip must name it instead of showing "Default".
func TestModelListState_UncuratedProviderEchoesThePick(t *testing.T) {
	pid := withModellessProvider(t, "")
	agentID := "builtin-modellist-custom-" + t.Name()
	addBuiltinAgent(t, agentID, &pid, "hand-typed-model")

	state := (&Backend{}).ModelListState(agentID)
	if state == nil {
		t.Fatal("a hand-typed pick on an otherwise list-less provider must still be reported")
	}
	if len(state.Models) != 1 || state.Models[0].ID != "hand-typed-model" || !state.Models[0].Default {
		t.Fatalf("models = %+v, want exactly the picked model as default", state.Models)
	}
	if state.CurrentModelID != "hand-typed-model" {
		t.Fatalf("currentModelId = %q, want the pick", state.CurrentModelID)
	}
}

// TestModelListState_UncuratedProviderWithoutPickHasNoList pins that an empty
// curated list plus no pick yields nothing rather than an empty-state object:
// "no models" and "no list" must stay distinguishable for the picker.
func TestModelListState_UncuratedProviderWithoutPickHasNoList(t *testing.T) {
	pid := withModellessProvider(t, "")
	agentID := "builtin-modellist-empty-" + t.Name()
	addBuiltinAgent(t, agentID, &pid, "")

	if state := (&Backend{}).ModelListState(agentID); state != nil {
		t.Fatalf("nothing to offer must be nil, got %+v", state)
	}
}

// TestModelListState_UnknownOrForeignAgentIsNil pins the narrowing rule: several
// built-in agents share one backend instance, so an id this backend does not
// serve (unknown, or an ACP agent) must not receive some other agent's list.
func TestModelListState_UnknownOrForeignAgentIsNil(t *testing.T) {
	withProvider(t, "http://127.0.0.1:1")

	if state := (&Backend{}).ModelListState("nobody-at-all"); state != nil {
		t.Fatalf("an unknown agent must yield nil, got %+v", state)
	}

	acpID := "builtin-modellist-acp-" + t.Name()
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	model.Agents[acpID] = &model.Agent{ID: acpID, Backend: "claude", AcpCommand: "claude --acp"}
	t.Cleanup(func() { delete(model.Agents, acpID) })
	if state := (&Backend{}).ModelListState(acpID); state != nil {
		t.Fatalf("an ACP agent is not served by this backend, got %+v", state)
	}
}

// TestModelListStateReachesRegistry is the end-to-end seam that actually broke:
// every consumer asks the capability registry, not the backend. The registry
// must answer from the in-process backend, and the ACP gate must then treat the
// list as confirmed — otherwise handler.gateUnconfirmedACPModels withholds it
// and the picker is empty again.
func TestModelListStateReachesRegistry(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1")

	reg := ai.GetAgentCapabilityRegistry()
	state := reg.GetModelListState(agentID, "")
	if state == nil || len(state.Models) != 1 {
		t.Fatalf("registry must serve the in-process list, got %+v", state)
	}
	if !reg.HasModelList(agentID) {
		t.Fatal("HasModelList must agree with GetModelListState")
	}
	if !ai.HasConfirmedACPModels(agentID) {
		t.Fatal("an in-process model list needs no handshake, so it must count as confirmed")
	}

	// A session-level pick the list no longer contains is echoed back rather
	// than replaced: the turn does run it.
	got := reg.GetModelListState(agentID, "picked-elsewhere")
	if got == nil || got.CurrentModelID != "picked-elsewhere" {
		t.Fatalf("the session's pick must be echoed, got %+v", got)
	}
}

// TestModelListStateRegistryIgnoresUnknownAgents pins that the fallback did not
// turn the registry into an always-answer machine: an unknown id still has no
// list, and the gate still withholds.
func TestModelListStateRegistryIgnoresUnknownAgents(t *testing.T) {
	reg := ai.GetAgentCapabilityRegistry()
	if state := reg.GetModelListState("definitely-not-registered", ""); state != nil {
		t.Fatalf("an unknown agent must have no list, got %+v", state)
	}
	if reg.HasModelList("definitely-not-registered") {
		t.Fatal("HasModelList must stay false for an unknown agent")
	}
}
