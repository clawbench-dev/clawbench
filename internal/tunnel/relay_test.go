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

// TestRelayDrainGrace_DefaultIsBounded pins the production default. The exact
// value is a judgement call (see RelayDuplex), but it must be non-zero — a zero
// grace would silently restore the pre-fix behavior of dropping the client's
// in-flight bytes — and small enough that a short-lived target cannot park the
// handler for long.
func TestRelayDrainGrace_DefaultIsBounded(t *testing.T) {
	got := RelayDrainGrace()
	if got <= 0 {
		t.Fatalf("default drain grace must be positive, got %v", got)
	}
	if got > 30*time.Second {
		t.Fatalf("default drain grace %v is too large to bound the handler", got)
	}
}

// TestRelayDuplex_ClientHalfCloseFirstSkipsGrace is the equivalence half of the
// contract: when the client half-closes first (the ordinary request/response
// shape, and exactly what relayBidir handles), the relay waits for the
// response with NO deadline and does NOT pay the drain grace. The grace is set
// long here; returning well inside it proves the grace is only applied to the
// target-ended-first path.
func TestRelayDuplex_ClientHalfCloseFirstSkipsGrace(t *testing.T) {
	restore := SetRelayDrainGraceForTest(30 * time.Second)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	clientIn, clientInWriter := io.Pipe()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The client half-closes first.
	if _, err := clientInWriter.Write([]byte("req")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if err := clientInWriter.Close(); err != nil {
		t.Fatalf("client half-close: %v", err)
	}

	// The target sees EOF on read and answers, then closes.
	buf := make([]byte, 3)
	if _, err := io.ReadFull(targetSide, buf); err != nil {
		t.Fatalf("target read: %v", err)
	}
	if string(buf) != "req" {
		t.Fatalf("target got %q, want req", buf)
	}
	if _, err := targetSide.Write([]byte("resp")); err != nil {
		t.Fatalf("target write: %v", err)
	}
	_ = targetSide.Close()

	start := time.Now()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return after the target closed")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("relay took %v; the client-half-close-first path must not wait for the grace", elapsed)
	}
	if out.String() != "resp" {
		t.Fatalf("client got %q, want resp", out.String())
	}
}

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

// TestRelayDuplex_TargetCloseEndsRelayEvenWhileClientBodyStaysOpen pins the
// deadlock fix. When the target closes its side (the normal end of a short
// request/response, and exactly what a reverse-tunnel visitor does when it
// hangs up), the relay must return even though the client's request body is
// still open — the client is waiting for the response to end, so waiting for
// both directions would park the handler forever and the response would never
// terminate.
//
// The return is now bounded by relayDrainGrace rather than immediate: the
// grace exists so a client with bytes in flight still gets them delivered (see
// RelayDuplex). The assertion is that the relay ends within the grace plus
// margin, i.e. that the deadlock is still broken.
func TestRelayDuplex_TargetCloseEndsRelayEvenWhileClientBodyStaysOpen(t *testing.T) {
	restore := SetRelayDrainGraceForTest(150 * time.Millisecond)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}

	// A body that never reaches EOF: models a client that keeps the stream open.
	clientIn, clientInWriter := io.Pipe()
	defer func() { _ = clientInWriter.Close() }()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	if _, err := targetSide.Write([]byte("answer")); err != nil {
		t.Fatalf("target write: %v", err)
	}
	// The target closes, ending the response direction.
	_ = targetSide.Close()

	start := time.Now()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RelayDuplex must return once the target closes, even with the client body still open")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("relay took %v to end after the target closed; the grace must be bounded", elapsed)
	}

	if out.String() != "answer" {
		t.Fatalf("client got %q, want answer", out.String())
	}
}

// TestRelayDuplex_ClientDisconnectEndsRelay covers the other direction: a client
// that goes away must not leave the relay blocked on the target.
func TestRelayDuplex_ClientDisconnectEndsRelay(t *testing.T) {
	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	clientIn, clientInWriter := io.Pipe()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The client abandons the stream. The caller closes the target (as the
	// handlers do on request-context cancellation), which unblocks the reader.
	_ = clientInWriter.CloseWithError(errors.New("client gone"))
	_ = relaySide.Close()
	_ = targetSide.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return after the connection was closed")
	}
}

// TestRelayDuplex_TargetHalfCloseStillDrainsClientWrites is the counterexample
// an independent verifier found: a target that shuts down only its WRITE side
// (sends FIN) but keeps READING must still receive the client's subsequent
// bytes. The SSH relay (internal/ssh/server.go relayBidir) delivers them
// because it waits for BOTH directions; RelayDuplex used to abandon the
// client->target copy the instant the target->client direction ended, so the
// bytes were dropped as soon as the caller closed the target on return.
//
// Two assertions, in order:
//  1. the relay must NOT return immediately after the target half-closes —
//     returning is what makes the caller close the target and lose the data;
//  2. a write that arrives afterwards must actually reach the target.
func TestRelayDuplex_TargetHalfCloseStillDrainsClientWrites(t *testing.T) {
	// A generous grace so assertion 1 is about the drain window existing, not
	// about its length; assertion 2 completes well inside it.
	restore := SetRelayDrainGraceForTest(2 * time.Second)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	clientIn, clientInWriter := io.Pipe()
	defer func() { _ = clientInWriter.Close() }()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The target finishes its response but keeps its read side open.
	targetTCP, ok := targetSide.(*net.TCPConn)
	if !ok {
		t.Fatalf("expected a *net.TCPConn target, got %T", targetSide)
	}
	if err := targetTCP.CloseWrite(); err != nil {
		t.Fatalf("target CloseWrite: %v", err)
	}

	// Assertion 1: the relay must hold the stream open for the client->target
	// direction instead of returning as soon as the target's FIN arrives.
	select {
	case <-done:
		t.Fatal("RelayDuplex returned immediately after the target half-closed; the client->target direction was abandoned")
	case <-time.After(100 * time.Millisecond):
	}

	// Assertion 2: a late client write must reach the still-reading target.
	if _, err := clientInWriter.Write([]byte("late-write")); err != nil {
		t.Fatalf("client late write: %v", err)
	}

	got := make([]byte, len("late-write"))
	_ = targetSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(targetSide, got); err != nil {
		t.Fatalf("target did not receive the client's late write: %v", err)
	}
	if string(got) != "late-write" {
		t.Fatalf("target got %q, want late-write", got)
	}

	// Ending the client body lets the relay finish promptly.
	_ = clientInWriter.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return after the client half-closed")
	}
}

// TestRelayDuplex_GraceExpiryReleasesEverything pins the upper bound: when the
// target ends its response and the client never ends its body, the relay must
// still return once the grace expires, and the request pump must then exit as
// soon as the caller releases the body. The second half matters: the bound is
// only useful if returning does not strand a goroutine blocked in io.Copy —
// in the server that goroutine is released by net/http closing r.Body when the
// handler returns (measured: the reader gets "stream error: NO_ERROR"), and
// this test models that release.
func TestRelayDuplex_GraceExpiryReleasesEverything(t *testing.T) {
	restore := SetRelayDrainGraceForTest(100 * time.Millisecond)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}

	// A body whose Read blocks until Close is called, and which reports when
	// the pump has passed that blocking point.
	clientIn := newBlockingReader()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The target closes; the client body stays open forever.
	_ = targetSide.Close()

	start := time.Now()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay must end when the grace expires")
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("relay returned after %v, before the 100ms grace could have elapsed", elapsed)
	}

	// The pump is parked on the body read, exactly as it is in the handler
	// while the grace runs.
	select {
	case <-clientIn.reading:
	case <-time.After(time.Second):
		t.Fatal("the request pump never read from the client body")
	}

	// Release the body, as net/http does when the handler returns (measured:
	// the reader gets "stream error: NO_ERROR"), and the pump must leave its
	// blocking read rather than strand a goroutine per abandoned stream.
	clientIn.Close()
	select {
	case <-clientIn.released:
	case <-time.After(time.Second):
		t.Fatal("the request pump is still blocked on the client body after the handler released it")
	}

	// The relay must not close the target (the caller owns it), but the caller
	// must be able to.
	_ = relaySide.Close()
}

// blockingReader models an HTTP request body: Read blocks until Close, then
// returns io.EOF. reading is closed the first time Read is entered and
// released when a Read returns, so a test can observe both the pump parking on
// the body and the pump leaving it.
type blockingReader struct {
	reading  chan struct{}
	closed   chan struct{}
	released chan struct{}
	once     sync.Once
}

func newBlockingReader() *blockingReader {
	return &blockingReader{
		reading:  make(chan struct{}),
		closed:   make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (r *blockingReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.reading) })
	<-r.closed
	select {
	case <-r.released:
	default:
		close(r.released)
	}
	return 0, io.EOF
}

func (r *blockingReader) Close() {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
}

// TestRelayDuplex_LargeTransferThenHalfClose combines the two properties that
// interact here: a 64 KiB body must arrive intact and, once the client
// half-closes, the target's 64 KiB answer must come back in full. A relay that
// closed the target on client EOF would truncate the answer; one that returned
// early on target EOF would drop the tail of the request.
func TestRelayDuplex_LargeTransferThenHalfClose(t *testing.T) {
	restore := SetRelayDrainGraceForTest(2 * time.Second)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	answer := make([]byte, 64*1024)
	for i := range answer {
		answer[i] = byte((i + 7) % 251)
	}

	var out bytes.Buffer
	clientIn, clientInWriter := io.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The target drains the request and only then writes its answer, so the
	// write cannot begin until the client half-closes.
	targetErr := make(chan error, 1)
	go func() {
		got, err := io.ReadAll(targetSide)
		if err != nil {
			targetErr <- err
			return
		}
		if !bytes.Equal(got, payload) {
			targetErr <- errors.New("target received a different payload than the client sent")
			return
		}
		if _, err := targetSide.Write(answer); err != nil {
			targetErr <- err
			return
		}
		// The target finishes its response and closes, ending both directions.
		targetErr <- targetSide.Close()
	}()

	if _, err := clientInWriter.Write(payload); err != nil {
		t.Fatalf("client write: %v", err)
	}
	// Client half-closes: target must see EOF but keep its write side.
	if err := clientInWriter.Close(); err != nil {
		t.Fatalf("client half-close: %v", err)
	}

	if err := <-targetErr; err != nil {
		t.Fatalf("target side: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return after both directions finished")
	}

	if !bytes.Equal(out.Bytes(), answer) {
		t.Fatalf("client got %d bytes, want %d (answer truncated)", out.Len(), len(answer))
	}
}

// TestRelayDuplex_BidirectionalBulkTransfer drives both directions with 64 KiB
// concurrently in flight. The existing large-transfer test is strictly
// request-then-answer (the target reads to EOF before writing), so it never
// exercises the two copies running at the same time against each other. Both
// directions must still come out byte-exact.
func TestRelayDuplex_BidirectionalBulkTransfer(t *testing.T) {
	restore := SetRelayDrainGraceForTest(2 * time.Second)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	const size = 64 * 1024
	request := make([]byte, size)
	response := make([]byte, size)
	for i := range request {
		request[i] = byte(i % 251)
		response[i] = byte((i + 13) % 251)
	}

	clientIn, clientInWriter := io.Pipe()
	var out bytes.Buffer

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, clientIn, &out, nil)
	}()

	// The target answers while it is still reading the request, so the two
	// copies overlap rather than serializing.
	targetErr := make(chan error, 1)
	go func() {
		got := make([]byte, size)
		if _, err := io.ReadFull(targetSide, got); err != nil {
			targetErr <- err
			return
		}
		if !bytes.Equal(got, request) {
			targetErr <- errors.New("target received a different request payload")
			return
		}
		if _, err := targetSide.Write(response); err != nil {
			targetErr <- err
			return
		}
		targetErr <- targetSide.Close()
	}()

	// Write the request in chunks so the response copy starts before the request
	// copy has finished.
	for written := 0; written < size; {
		end := written + 4096
		if end > size {
			end = size
		}
		n, err := clientInWriter.Write(request[written:end])
		if err != nil {
			t.Fatalf("client write: %v", err)
		}
		written += n
	}
	if err := clientInWriter.Close(); err != nil {
		t.Fatalf("client half-close: %v", err)
	}

	if err := <-targetErr; err != nil {
		t.Fatalf("target side: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayDuplex did not return after both directions finished")
	}

	if !bytes.Equal(out.Bytes(), response) {
		t.Fatalf("client got %d bytes, want %d (response truncated or corrupted)", out.Len(), len(response))
	}
}

// failingWriter fails every write, modelling a client that has gone away
// mid-stream.
type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestRelayDuplex_ClientOutWriteErrorEndsRelay covers relay.go:54: a write
// error on the target -> client direction must end that copy (and thus the
// relay) rather than looping or hanging.
func TestRelayDuplex_ClientOutWriteErrorEndsRelay(t *testing.T) {
	restore := SetRelayDrainGraceForTest(200 * time.Millisecond)
	defer restore()

	relaySide, targetSide, err := tcpPair()
	if err != nil {
		t.Fatalf("tcpPair: %v", err)
	}
	defer targetSide.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayDuplex(relaySide, bytes.NewReader(nil), failingWriter{err: errors.New("client gone")}, nil)
	}()

	// The target sends data, which the clientOut writer will reject.
	if _, err := targetSide.Write([]byte("unwritable")); err != nil {
		t.Fatalf("target write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a clientOut write error must end the relay, not hang it")
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
