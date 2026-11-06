package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// extraConfigRecorder captures every set_config_option the handler forwards.
// The forward is asynchronous, so positive assertions poll and the negative one
// waits on the channel with a timeout.
type extraConfigRecorder struct {
	mu   sync.Mutex
	sent [][2]string
	ch   chan [2]string
}

func (r *extraConfigRecorder) all() [][2]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][2]string, len(r.sent))
	copy(out, r.sent)
	return out
}

// installExtraConfigRecorder wires a fake ACP connection onto the session and
// returns the recorder.
func installExtraConfigRecorder(t *testing.T, agent *model.Agent, sessionID string) *extraConfigRecorder {
	t.Helper()
	conn := ai.NewACPConnForTest(agent, sessionID)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(sessionID, "acp-sid-extra-"+sessionID)
	// The handler only accepts ids the agent advertised for THIS session, so the
	// fixture must look like a codex connection that offered Fast mode.
	conn.SetExtraConfigOptions([]ai.ExtraConfigOption{{
		ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", CurrentID: "off",
		Values: []ai.ConfigOptionValue{{ID: "off", Name: "Off"}, {ID: "on", Name: "On"}},
	}})
	ai.GetACPConnManager().SetConnForTest(sessionID, conn)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sessionID) })

	rec := &extraConfigRecorder{ch: make(chan [2]string, 4)}
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, configID, value string) error {
		rec.mu.Lock()
		rec.sent = append(rec.sent, [2]string{configID, value})
		rec.mu.Unlock()
		select {
		case rec.ch <- [2]string{configID, value}:
		default:
		}
		return nil
	})
	return rec
}

// The generic option channel must reach the agent under the id IT advertised
// ("fast-mode"), with the value the user picked. A category-mapped id would be
// rejected as an unknown option and the toggle would silently do nothing.
func TestServeAISessionUpdate_ConfigOptions_ForwardsWireID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "patch-extra-cfg", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"configOptions": map[string]string{"fast-mode": "on"},
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	select {
	case got := <-rec.ch:
		assert.Equal(t, [2]string{"fast-mode", "on"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH must forward the generic option to the ACP agent")
	}
}

// The settings endpoint (used by the drawer for group MEMBER rows) must honor
// the same field — otherwise the toggle works in single chat and silently does
// nothing when opened on a member.
func TestServeSessionSettingsUpdate_ConfigOptions_Forwards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-member-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "settings-extra-cfg", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId":     sessionID,
		"configOptions": map[string]string{"fast-mode": "on"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	assertOK(t, w)

	select {
	case got := <-rec.ch:
		assert.Equal(t, [2]string{"fast-mode", "on"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("settings PATCH must forward the generic option too")
	}
}

// Empty ids/values are dropped rather than sent: set_config_option with an
// empty id is a protocol error that would surface as a failed turn.
func TestApplyExtraConfigOptions_SkipsEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-empty-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-empty", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"": "on", "fast-mode": ""})

	select {
	case got := <-rec.ch:
		t.Fatalf("empty id/value must not reach the agent, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// The generic channel must not be a back door into the dedicated settings: a
// request naming "mode"/"model" would otherwise reach set_config_option without
// persistence, per-model effort validation, or auto-approve bookkeeping.
func TestApplyExtraConfigOptions_RejectsUnadvertisedIDs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-guard-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-guard", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"mode": "bypassPermissions", "model": "gpt-5"})

	select {
	case got := <-rec.ch:
		t.Fatalf("only agent-advertised ids may pass, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}
