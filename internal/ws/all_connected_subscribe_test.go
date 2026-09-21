package ws

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllConnectedClientsSubscribe_NoClients covers the empty-server case: with
// nobody connected the answer must be false so callers still persist the event
// (there is no one online who could have received it).
func TestAllConnectedClientsSubscribe_NoClients(t *testing.T) {
	mgr := NewManagerForTest()
	assert.False(t, mgr.AllConnectedClientsSubscribe("s1"),
		"no connected clients must report false, not 'everyone is subscribed'")
}

// TestAllConnectedClientsSubscribe_EmptySessionID covers the guard: an empty
// session can never be "fully covered".
func TestAllConnectedClientsSubscribe_EmptySessionID(t *testing.T) {
	mgr := NewManagerForTest()
	var writeMu sync.Mutex
	mgr.Subscribe(acceptRealConn(t), &writeMu, "c1", "")
	assert.False(t, mgr.AllConnectedClientsSubscribe(""))
}

// TestAllConnectedClientsSubscribe_AllSubscribed is the positive case: the one
// connected client is subscribed, so the event could not have been missed.
func TestAllConnectedClientsSubscribe_AllSubscribed(t *testing.T) {
	mgr := NewManagerForTest()
	var writeMu sync.Mutex
	mgr.Subscribe(acceptRealConn(t), &writeMu, "c1", "")
	mgr.StreamHub().Subscribe("c1", "s1")

	assert.True(t, mgr.AllConnectedClientsSubscribe("s1"),
		"the only connected client is subscribed → nobody could have missed it")
}

// TestAllConnectedClientsSubscribe_ConnectedButNotSubscribed is the regression
// test for why this is per-session rather than HasDisconnectedClients: a client
// can be connected yet viewing a different session. Its event for s1 is dropped
// live, so this must report false even though no client is "disconnected".
func TestAllConnectedClientsSubscribe_ConnectedButNotSubscribed(t *testing.T) {
	mgr := NewManagerForTest()
	var writeMu sync.Mutex
	mgr.Subscribe(acceptRealConn(t), &writeMu, "c1", "")
	mgr.StreamHub().Subscribe("c1", "other-session")

	assert.False(t, mgr.AllConnectedClientsSubscribe("s1"),
		"a connected client viewing another session has NOT seen s1's events")
}

// TestAllConnectedClientsSubscribe_OneOfTwoMissing covers the mixed case: one
// client is subscribed and another is not, so the answer must be false.
func TestAllConnectedClientsSubscribe_OneOfTwoMissing(t *testing.T) {
	mgr := NewManagerForTest()
	var w1, w2 sync.Mutex
	require.NotNil(t, mgr.Subscribe(acceptRealConn(t), &w1, "c1", ""))
	require.NotNil(t, mgr.Subscribe(acceptRealConn(t), &w2, "c2", ""))
	mgr.StreamHub().Subscribe("c1", "s1")
	mgr.StreamHub().Subscribe("c2", "other-session")

	assert.False(t, mgr.AllConnectedClientsSubscribe("s1"),
		"one subscribed + one not → not everyone is covered")
}

// TestAllConnectedClientsSubscribe_DisconnectedIgnored pins the "connected"
// filter: a subscription with no live conn is skipped entirely, so a stale
// disconnected subscription does not make the answer false (the client is
// offline and the persistence path handles it separately).
func TestAllConnectedClientsSubscribe_DisconnectedIgnored(t *testing.T) {
	mgr := NewManagerForTest()

	// A subscription that was never given a connection (conn == nil).
	mgr.mu.Lock()
	mgr.subscriptions["ghost"] = &ClientSubscription{clientID: "ghost"}
	mgr.mu.Unlock()

	var writeMu sync.Mutex
	mgr.Subscribe(acceptRealConn(t), &writeMu, "c1", "")
	mgr.StreamHub().Subscribe("c1", "s1")

	assert.True(t, mgr.AllConnectedClientsSubscribe("s1"),
		"the disconnected subscription must be skipped, leaving only the live subscriber")
}
