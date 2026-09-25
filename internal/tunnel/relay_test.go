package tunnel

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestRelayDuplex_HalfCloseLetsTargetKeepWriting is the core correctness
// property of the relay: when the client stops sending (request body EOF), the
// target must see EOF on read while STILL being able to write its answer back.
// A full Close instead of CloseWrite would truncate that answer.
func TestRelayDuplex_HalfCloseLetsTargetKeepWriting(t *testing.T) {
	// Fake "target": we hand RelayDuplex the server end of a real TCP pair so
	// CloseWrite is observable, and drive the other end ourselves.
	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	clientIn := bytes.NewReader([]byte("request-bytes"))
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The target reads the forwarded request, then observes EOF (half-close).
	got := make([]byte, len("request-bytes"))
	if _, err := io.ReadFull(targetSide, got); err != nil {
		t.Fatalf("target read: %v", err)
	}
	if string(got) != "request-bytes" {
		t.Fatalf("target got %q, want request-bytes", got)
	}

	targetSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := targetSide.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("target must observe EOF after client half-close, got err=%v", err)
	}

	// Crucially, the response direction is still open.
	if _, err := targetSide.Write([]byte("late-response")); err != nil {
		t.Fatalf("target must still be able to write after client EOF: %v", err)
	}

	// Now the target finishes, which ends the response direction.
	targetSide.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return")
	}

	if out.String() != "late-response" {
		t.Fatalf("client got %q, want late-response", out.String())
	}
}

// TestRelayDuplex_FlushesWhileStreaming guards the flush contract: without a
// flush after each write, a buffered clientOut would hold the bytes until
// RelayDuplex returns. The assertion is ordered rather than counted, because
// TCP may coalesce writes into a single read — what matters is that data
// reaches the client while the relay is still running.
func TestRelayDuplex_FlushesWhileStreaming(t *testing.T) {
	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	flushed := make(chan struct{}, 8)
	recorder := &recordingWriter{}

	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		RelayDuplex(relaySide, bytes.NewReader(nil), recorder, func() error {
			flushed <- struct{}{}
			return nil
		})
	}()

	if _, err := targetSide.Write([]byte("early")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Wait for the flush signal. Receiving it proves the flush hook ran while
	// the relay was still active (the target conn is still open below).
	select {
	case <-flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("no flush observed while streaming")
	}

	if got := recorder.String(); got != "early" {
		t.Fatalf("client got %q, want early (data must be visible before the relay ends)", got)
	}

	select {
	case <-relayDone:
		t.Fatal("relay must still be running: the target conn was never closed")
	default:
	}
}

func TestHalfCloseWrite_NonTCPFallsBackToClose(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	HalfCloseWrite(c1)

	// net.Pipe has no CloseWrite, so the whole connection is closed: a write
	// must now fail.
	c2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the non-TCP fallback to close the connection")
	}
}

// tcpPair returns the two ends of a real loopback TCP connection.
func tcpPair() (net.Conn, net.Conn, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer ln.Close()

	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.Accept()
		ch <- res{c, err}
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	r := <-ch
	if r.err != nil {
		client.Close()
		return nil, nil, r.err
	}
	return client, r.c, nil
}

// recordingWriter is a bytes.Buffer with a stable String method under -race.
type recordingWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *recordingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
