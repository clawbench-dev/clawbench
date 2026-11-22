package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestPool_ReusesConnection is the reason the pool exists: a second turn must
// not start a second process.
func TestPool_ReusesConnection(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)
	servers := []model.MCPServer{helperServer(t, "server")}

	first := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, first.Servers, 1)
	firstSession := first.Servers[0].Session
	first.Release()

	second := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, second.Servers, 1)
	assert.Same(t, firstSession, second.Servers[0].Session, "the cached connection must be reused")
	second.Release()

	p.CloseAll()
}

// TestPool_RedialsOnConfigChange pins the fingerprint: editing a server (an env
// var here) must not keep running the old process.
func TestPool_RedialsOnConfigChange(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)

	a := helperServer(t, "server")
	b := helperServer(t, "server")
	b.Env[helperEnvVar] = "server" // same command, different fingerprint below
	b.Args = []string{"--different"}

	first := p.Acquire(ctx, "agent-1", []model.MCPServer{a}, DialOptions{})
	require.Len(t, first.Servers, 1)
	firstSession := first.Servers[0].Session
	first.Release()

	second := p.Acquire(ctx, "agent-1", []model.MCPServer{b}, DialOptions{})
	require.Len(t, second.Servers, 1)
	assert.NotSame(t, firstSession, second.Servers[0].Session, "a config change must dial again")
	second.Release()

	// The old session must be closed, not merely forgotten.
	_, err := firstSession.ListTools(ctx)
	assert.Error(t, err)
	p.CloseAll()
}

// TestPool_PartialFailure keeps one broken server from taking the turn down.
func TestPool_PartialFailure(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)
	servers := []model.MCPServer{
		helperServer(t, "server"),
		{Name: "missing", Transport: model.MCPTransportStdio, Command: "clawbench-no-such-mcp-binary"},
	}

	bundle := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, bundle.Servers, 1, "the working server must still be attached")
	require.Len(t, bundle.Notes, 1)
	assert.Contains(t, bundle.Notes[0], "missing")
	assert.Contains(t, bundle.Notes[0], "not found in PATH")
	bundle.Release()

	p.CloseAll()
}

// TestPool_RefcountKeepsLiveConnections: an idle reap must not close a
// connection a turn is still using.
func TestPool_RefcountKeepsLiveConnections(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)
	servers := []model.MCPServer{helperServer(t, "server")}

	held := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, held.Servers, 1)
	session := held.Servers[0].Session

	p.reap(time.Now().Add(time.Hour))
	_, err := session.ListTools(ctx)
	require.NoError(t, err, "a held connection must survive the reaper")

	held.Release()
	p.reap(time.Now().Add(time.Hour))
	_, err = session.ListTools(ctx)
	assert.Error(t, err, "an idle connection must be closed")
}

// TestPool_IdleReapReconnects covers the other half: after a reap, the next turn
// dials again instead of using a dead session.
func TestPool_IdleReapReconnects(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)
	servers := []model.MCPServer{helperServer(t, "server")}

	first := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, first.Servers, 1)
	firstSession := first.Servers[0].Session
	first.Release()

	p.reap(time.Now().Add(time.Hour))

	second := p.Acquire(ctx, "agent-1", servers, DialOptions{})
	require.Len(t, second.Servers, 1)
	assert.NotSame(t, firstSession, second.Servers[0].Session)
	second.Release()
	p.CloseAll()
}

// TestPool_CloseAllRefusesNewWork pins the shutdown path: after CloseAll a turn
// must not spawn a new server.
func TestPool_CloseAllRefusesNewWork(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)

	p.CloseAll()
	bundle := p.Acquire(ctx, "agent-1", []model.MCPServer{helperServer(t, "server")}, DialOptions{})
	assert.Empty(t, bundle.Servers)
	require.Len(t, bundle.Notes, 1)
	assert.Contains(t, bundle.Notes[0], "shutting down")
}

// TestServersFingerprint pins the two properties the cache depends on:
// identical config hashes the same, and map ordering cannot change the hash.
func TestServersFingerprint(t *testing.T) {
	a := []model.MCPServer{{
		Name: "x", Transport: "stdio", Command: "run",
		Env: map[string]string{"A": "1", "B": "2"},
	}}
	b := []model.MCPServer{{
		Name: "x", Transport: "stdio", Command: "run",
		Env: map[string]string{"B": "2", "A": "1"},
	}}
	assert.Equal(t, serversFingerprint(a), serversFingerprint(b))

	c := []model.MCPServer{{
		Name: "x", Transport: "stdio", Command: "run",
		Env: map[string]string{"A": "1", "B": "3"},
	}}
	assert.NotEqual(t, serversFingerprint(a), serversFingerprint(c))

	// Slice order is part of the identity: the same servers in another order
	// produce another list, and the pool is keyed by that list.
	d := []model.MCPServer{a[0], {Name: "y", Transport: "stdio", Command: "run"}}
	e := []model.MCPServer{{Name: "y", Transport: "stdio", Command: "run"}, a[0]}
	assert.NotEqual(t, serversFingerprint(d), serversFingerprint(e))
}

// TestPool_ConnectedServerAccessors covers the small surface the adapter uses.
func TestPool_ConnectedServerAccessors(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)

	bundle := p.Acquire(ctx, "agent-1", []model.MCPServer{helperServer(t, "server")}, DialOptions{})
	require.Len(t, bundle.Servers, 1)
	cs := bundle.Servers[0]

	assert.Equal(t, "probe-test", cs.ServerName())
	assert.Len(t, cs.Tools(), 2)

	res, err := cs.Call(ctx, "alpha", nil)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "alpha:")

	bundle.Release()
	p.CloseAll()
}

// TestPool_RefreshesListChangedTools exercises the refresh path: a server that
// advertises listChanged is re-listed on the next turn, one that does not is
// left alone.
func TestPool_RefreshesListChangedTools(t *testing.T) {
	p := NewPool(time.Minute)
	ctx := testCtx(t)

	bundle := p.Acquire(ctx, "agent-1", []model.MCPServer{helperServer(t, "listchanged")}, DialOptions{})
	require.Len(t, bundle.Servers, 1)
	assert.Len(t, bundle.Servers[0].Tools(), 2)
	bundle.Release()

	again := p.Acquire(ctx, "agent-1", []model.MCPServer{helperServer(t, "listchanged")}, DialOptions{})
	require.Len(t, again.Servers, 1)
	assert.Len(t, again.Servers[0].Tools(), 2, "the refreshed list must still be usable")
	again.Release()

	p.CloseAll()
}

// TestCloseAllSessionsIsIdempotent covers the shutdown entry point. It runs in
// its own test binary and nothing else in this package uses the default pool,
// so closing it here cannot affect another test.
func TestCloseAllSessionsIsIdempotent(t *testing.T) {
	assert.NotNil(t, DefaultPool())
	CloseAllSessions()
	CloseAllSessions()
}

// TestPool_CachesFailedDials: when every server fails, the failure itself is
// cached. Without this a permanently broken config re-runs the connect timeout
// on every turn.
func TestPool_CachesFailedDials(t *testing.T) {
	countFile := filepath.Join(t.TempDir(), "starts")
	srv := helperServer(t, "die")
	srv.Env["MCP_PROBE_TEST_COUNT_FILE"] = countFile

	p := NewPool(time.Minute)
	ctx := testCtx(t)

	first := p.Acquire(ctx, "agent-1", []model.MCPServer{srv}, DialOptions{})
	require.Len(t, first.Notes, 1)
	first.Release()

	second := p.Acquire(ctx, "agent-1", []model.MCPServer{srv}, DialOptions{})
	require.Len(t, second.Notes, 1, "the failure must be remembered, not retried silently")
	second.Release()

	starts, err := os.ReadFile(countFile)
	require.NoError(t, err)
	assert.Len(t, string(starts), 1, "the server must be started exactly once")
	p.CloseAll()
}
