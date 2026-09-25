package tunnel

import (
	"io"
	"net"
	"sync/atomic"
	"time"
)

// HalfCloseWrite closes only the write side of c when the underlying transport
// supports it (*net.TCPConn) and closes the whole connection otherwise.
//
// This is the difference between a graceful half-close and a full teardown:
// when the client's request body reaches EOF the target must observe EOF on
// its read side, but the response direction has to stay open so the target can
// still send back whatever it has left. A plain Close would kill that.
func HalfCloseWrite(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = c.Close()
}

// flushWriter flushes after every successful write. Without it the response
// bytes sit in net/http's buffered writer (and in the h2 framer's buffer) until
// the buffer fills or the handler returns — for a long-lived interactive
// stream that means the peer sees nothing.
type flushWriter struct {
	w     io.Writer
	flush func() error
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if n > 0 && fw.flush != nil {
		_ = fw.flush()
	}
	return n, err
}

// copyToClient is io.Copy with the flush contract made explicit.
//
// io.Copy short-circuits when its source implements io.WriterTo, which would
// bypass flushWriter.Write and silently drop the flushes. On Linux a *net.TCPConn
// only takes that fast path for *net.UnixConn destinations, but relying on that
// is fragile — so the fast path is suppressed here and every chunk goes through
// the flushing writer.
func copyToClient(dst flushWriter, src io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			if _, ew := dst.Write(buf[:nr]); ew != nil {
				return
			}
		}
		if er != nil {
			return
		}
	}
}

// relayDrainGrace is the bounded window during which the client -> target
// direction may keep draining after the target -> client direction has ended.
//
// It exists because SSH and HTTP/2 are asymmetric on half-close:
//
//   - In SSH, relayBidir (internal/ssh/server.go:685) does wg.Wait(), so it
//     waits for BOTH directions. When the target half-closes, the SSH channel's
//     write side is shut down and the channel -> backend copy keeps running
//     indefinitely, so a downstream client can still push bytes back.
//   - On an HTTP/2 stream the server cannot half-close its response. Go's
//     stdlib only emits END_STREAM when the handler returns
//     (net/http/h2_bundle.go:6704 / :6731), and returning tears the request
//     body down in the same step (closeStream(..., http2errHandlerComplete),
//     h2_bundle.go:5375). Response-EOF and request-body lifetime are therefore
//     coupled: flushing does NOT end the body (measured), and waiting for both
//     directions unconditionally parks the handler forever — the deadlock T3
//     found.
//
// The grace is the compromise. After the target's write side ends the handler
// stays alive (so the request body keeps accepting client bytes) for this
// window, then returns to signal response EOF. A client that has nothing to
// send but keeps its socket open pays the window once; a client that closes
// its socket — the normal keep-alive / connection-close case — ends it
// immediately, and a client that already half-closed skips it entirely.
//
// This is deliberately a package variable rather than a literal so the
// behavior is exercisable in tests without multi-second waits; see
// SetRelayDrainGraceForTest. It is an atomic because a relay goroutine can
// still be running when a test restores the value — reading a plain variable
// there is a data race.
var relayDrainGrace atomic.Int64

func init() {
	relayDrainGrace.Store(int64(5 * time.Second))
}

// RelayDrainGrace returns the current bounded drain window.
func RelayDrainGrace() time.Duration {
	return time.Duration(relayDrainGrace.Load())
}

// SetRelayDrainGraceForTest overrides the drain grace and returns a function
// that restores the previous value. Tests only.
func SetRelayDrainGraceForTest(d time.Duration) func() {
	prev := relayDrainGrace.Swap(int64(d))
	return func() { relayDrainGrace.Store(prev) }
}

// RelayDuplex pumps bytes in both directions between the client's HTTP
// request/response pair and the target connection.
//
//   - clientIn is the request body (client -> server bytes) and is copied into
//     target; on EOF the target's write side is half-closed so the target sees
//     the request end while still being able to answer.
//   - target is copied into clientOut (server -> client bytes), flushing after
//     every write via flush.
//
// Closing is bidirectional and bounded, mirroring internal/ssh/server.go's
// relayBidir as closely as an HTTP/2 stream allows:
//
//   - client -> target ends first (the client half-closes): the target is
//     half-closed and the response direction is then awaited WITHOUT a
//     deadline. This is the ordinary request/response shape and it is exactly
//     what relayBidir's wg.Wait() does.
//   - target -> client ends first (the target half-closes or closes): the
//     response side is flushed and the client -> target direction is given a
//     bounded drain window (relayDrainGrace) to deliver bytes still in flight.
//     When the window expires the handler returns, which ends the response
//     body and releases the stream. Without the bound a short-lived target (or
//     a reverse-tunnel visitor hanging up) would park the handler forever,
//     because the client has no reason to end its body while it is waiting for
//     the response to finish.
//
// The caller owns the lifetime of target: this function never closes it, so the
// caller can close it on client disconnect to unblock the reader. Errors from
// either direction are treated as end-of-stream, matching relayBidir.
func RelayDuplex(target net.Conn, clientIn io.Reader, clientOut io.Writer, flush func() error) {
	clientToTargetDone := make(chan struct{})
	targetToClientDone := make(chan struct{})

	go func() {
		defer close(clientToTargetDone)
		_, _ = io.Copy(target, clientIn)
		HalfCloseWrite(target)
	}()

	go func() {
		defer close(targetToClientDone)
		copyToClient(flushWriter{w: clientOut, flush: flush}, target)
	}()

	// Wait for the target's response direction first. When the client
	// half-closed before this point the request direction is already done and
	// the grace below is skipped, so the ordinary half-close path waits for the
	// response with no deadline.
	//
	// Every response byte has already been flushed by copyToClient's writer, so
	// there is nothing to flush here: the grace only delays the END_STREAM that
	// the handler's return will send.
	<-targetToClientDone

	// Bounded grace for the request direction. The timer is the ceiling; the
	// client ending its body (or disconnecting, which closes the body) returns
	// immediately.
	timer := time.NewTimer(RelayDrainGrace())
	defer timer.Stop()

	select {
	case <-clientToTargetDone:
	case <-timer.C:
	}
}
