package service

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// TestResolveAgentID covers the resolution order shared by every turn entry
// point (handler, enqueue, drain, scheduler-adjacent paths):
//
//	explicit request → session's persisted agent → default agent
//
// The ACP/CLI backend decision keys off this value, so an empty result here is
// what makes a pure-ACP backend fail with "unsupported backend type".
func TestResolveAgentID(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	origList := model.AgentList
	origDefault := model.DefaultAgentID
	model.Agents = map[string]*model.Agent{
		"session-agent": {ID: "session-agent"},
		"default-agent": {ID: "default-agent"},
	}
	model.AgentList = []*model.Agent{{ID: "session-agent"}, {ID: "default-agent"}}
	model.DefaultAgentID = "default-agent"
	defer func() {
		model.Agents = origAgents
		model.AgentList = origList
		model.DefaultAgentID = origDefault
	}()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES (?, 1, 'claude', 'T', 'session-agent')`,
		"sess-with-agent",
	)
	assert.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES (?, 1, 'claude', 'T', '')`,
		"sess-empty-agent",
	)
	assert.NoError(t, err)

	t.Run("explicit request wins over the session", func(t *testing.T) {
		assert.Equal(t, "requested", ResolveAgentID("sess-with-agent", "requested"))
	})

	t.Run("empty request inherits the session agent", func(t *testing.T) {
		assert.Equal(t, "session-agent", ResolveAgentID("sess-with-agent", ""))
	})

	t.Run("empty request + empty session agent falls back to default", func(t *testing.T) {
		assert.Equal(t, "default-agent", ResolveAgentID("sess-empty-agent", ""))
	})

	t.Run("unknown session falls back to default", func(t *testing.T) {
		assert.Equal(t, "default-agent", ResolveAgentID("no-such-session", ""))
	})

	t.Run("empty session id falls back to default", func(t *testing.T) {
		assert.Equal(t, "default-agent", ResolveAgentID("", ""))
	})
}
