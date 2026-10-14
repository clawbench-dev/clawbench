package main

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestListenMainWithRetry_BindsImmediatelyWhenFree pins the happy path: a free
// port binds on the first attempt, so the retry adds no latency in the common
// case.
func TestListenMainWithRetry_BindsImmediatelyWhenFree(t *testing.T) {
	// Reserve a port, then release it so the bind below is the only one.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := probe.Addr().String()
	require.NoError(t, probe.Close())

	start := time.Now()
	ln, err := listenMainWithRetry(addr, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, err)
	defer ln.Close()

	require.Less(t, time.Since(start), 500*time.Millisecond,
		"a free port must bind on the first attempt, not after the retry window")
}

// TestListenMainWithRetry_RecoversAfterPortFreed pins the actual bug: the
// outgoing process still holds :20000 during its shutdown drain (until
// srv.Shutdown, which runs after the stream-finalize grace). A replacement
// launched in that window used to bind-fail and os.Exit(1) immediately,
// leaving the service down — this is the WSL-after-Windows symptom in issue
// #538. With the retry, the replacement waits out the drain and binds as soon
// as the port is released.
func TestListenMainWithRetry_RecoversAfterPortFreed(t *testing.T) {
	// Hold the port like the outgoing process does during its drain. Bind the
	// wildcard address the server actually uses so the conflict is real on
	// every platform (a 127.0.0.1 blocker can coexist with a wildcard bind on
	// BSD/macOS because SO_REUSEADDR is set there).
	blocker, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	port := blocker.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("0.0.0.0:%d", port)

	// Release the port shortly after the retry loop starts, standing in for
	// the outgoing process finishing its drain.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = blocker.Close()
	}()

	ln, err := listenMainWithRetry(addr, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, err, "bind must succeed once the outgoing process releases the port")
	defer ln.Close()
	require.Equal(t, port, ln.Addr().(*net.TCPAddr).Port)
}

// TestListenMainWithRetry_FailsAfterWindowWhenPortStaysHeld pins the bounded
// half: a genuinely occupied port must still fail rather than retry forever,
// so the port-conflict error reaches the operator in reasonable time.
func TestListenMainWithRetry_FailsAfterWindowWhenPortStaysHeld(t *testing.T) {
	blocker, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer blocker.Close()
	addr := fmt.Sprintf("0.0.0.0:%d", blocker.Addr().(*net.TCPAddr).Port)

	start := time.Now()
	ln, err := listenMainWithRetry(addr, 200*time.Millisecond, 20*time.Millisecond)
	require.Error(t, err, "a permanently held port must fail, not block forever")
	require.Nil(t, ln)
	require.Less(t, time.Since(start), 2*time.Second,
		"failure must come within the retry window, not hang")
}
