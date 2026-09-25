package tunnel

import (
	"io"
	"net"
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

// RelayDuplex pumps bytes in both directions between the client's HTTP
// request/response pair and the target connection.
//
//   - clientIn is the request body (client -> server bytes) and is copied into
//     target; on EOF the target's write side is half-closed.
//   - target is copied into clientOut (server -> client bytes), flushing after
//     every write via flush.
//
// The relay finishes when the target -> client direction finishes, i.e. when
// the target closes its side. The reverse direction (clientIn -> target) is then
// abandoned and the target is closed by the caller.
//
// Waiting for BOTH directions deadlocks the common case: a target that closes
// after responding (any short-lived HTTP backend, or a reverse-tunnel visitor
// hanging up) leaves the request-body copy blocked on a read the client has no
// reason to end yet — it is still waiting for the response to finish, which
// cannot happen while the handler is parked in wg.Wait. Ending the handler
// closes the response stream, which is exactly the signal the client needs; the
// abandoned body read is reaped by net/http closing the request body on return.
//
// The caller owns the lifetime of target: this function never closes it, so the
// caller can close it on client disconnect to unblock the reader. Errors from
// either direction are treated as end-of-stream, matching
// internal/ssh/server.go's relayBidir.
func RelayDuplex(target net.Conn, clientIn io.Reader, clientOut io.Writer, flush func() error) {
	done := make(chan struct{})

	go func() {
		_, _ = io.Copy(target, clientIn)
		HalfCloseWrite(target)
	}()

	go func() {
		defer close(done)
		copyToClient(flushWriter{w: clientOut, flush: flush}, target)
	}()

	<-done
}
