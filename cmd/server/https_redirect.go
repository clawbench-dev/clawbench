package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPS redirect listener.
//
// When this instance serves HTTPS, the main listener speaks TLS only: a plain
// http:// request never reaches the handler (the handshake fails and the
// connection is dropped), so no 301 is possible on that port. This listener is
// the companion: it accepts PLAINTEXT traffic on the same port number and does
// exactly one thing — answer every request with a redirect to the https URL.
//
// Why the same port instead of a second one: a second port would mean clients
// on :20000 (http) get bounced to :20001 (https), i.e. a URL that differs from
// the one everyone already has bookmarked and the one the banner prints. Here
// users type the same host:port either way and land on TLS.
//
// How it stays honest: the mux only CLASSIFIES connections by their first
// bytes and then hands the raw socket to the real servers. TLS termination and
// ALPN/HTTP2 negotiation are still done by srv.ServeTLS, and the redirect side
// is an ordinary http.Server. Nothing about the TLS path is reimplemented.
const (
	// TLS record content types (RFC 8446 §5.1). A connection whose first byte
	// is not one of these is treated as plaintext.
	tlsRecordChangeCipherSpec = 0x14
	tlsRecordAlert            = 0x15
	tlsRecordHandshake        = 0x16
	tlsRecordApplicationData  = 0x17

	// sniffBytes is how many bytes the classifier needs: content type plus the
	// two version bytes. One byte is not enough — 0x16 is also the first byte
	// of no HTTP method, but any single byte would be a coin flip.
	sniffBytes = 3

	// sniffTimeout bounds how long a silent connection may pin a goroutine.
	// Real clients send their request line immediately; this only catches port
	// scanners and half-open sockets.
	sniffTimeout = 10 * time.Second

	// redirectHeaderTimeout bounds reading the request head of a plaintext
	// connection we are about to bounce.
	redirectHeaderTimeout = 10 * time.Second
)

// redirectToHTTPS builds the handler for the plaintext side: every request gets
// a permanent redirect to the same host on https.
//
// The path and query are preserved; the method is not (301 turns a POST into a
// GET). That is the standard "you are on the wrong scheme" bounce and correct
// here: nothing is served over http in this mode, so no POST legitimately
// arrives.
func redirectToHTTPS(port int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		// Strip any port from Host, then re-add ours: a client reaching us as
		// "http://box:20000" must go to "https://box:20000", not to
		// "https://box:20000:20000" and not to the default 443.
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "" {
			slog.Warn("https redirect: request has no Host header")
			http.Error(w, "HTTPS is required on this port.", http.StatusBadRequest)
			return
		}
		target := "https://" + net.JoinHostPort(host, itoa(port)) + r.URL.RequestURI()
		// no-store: a browser must not cache this bounce, or turning HTTPS off
		// later would leave users stuck on a redirect to a scheme we no longer
		// serve.
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// itoa formats a small non-negative int without importing strconv (keeps this
// file's imports to what it actually uses).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// sniffTLS reports whether the leading bytes are a TLS record header.
//
// Checking all three bytes (content type AND a 0x03 0x0N version pair) is what
// keeps a plaintext request from being misrouted: "GET" starts with 0x47, but
// even a crafted first byte alone would not also match the version bytes.
func sniffTLS(head []byte) bool {
	if len(head) < sniffBytes {
		return false
	}
	switch head[0] {
	case tlsRecordChangeCipherSpec, tlsRecordAlert, tlsRecordHandshake, tlsRecordApplicationData:
	default:
		return false
	}
	return head[1] == 0x03 && head[2] <= 0x04
}

// prefixConn is a net.Conn whose first bytes were already consumed by the
// classifier. Reads drain those first, then fall through to the real socket.
type prefixConn struct {
	net.Conn
	pre []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.pre) > 0 {
		n := copy(p, c.pre)
		c.pre = c.pre[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// redirectMux owns the raw listener and hands connections to the TLS server or
// the redirect server.
type redirectMux struct {
	raw     net.Listener
	tlsCh   chan net.Conn
	redirCh chan net.Conn

	// ctx is cancelled by Close so in-flight classifier goroutines give up
	// instead of blocking on a channel nobody reads anymore. done is closed when
	// the accept loop has exited.
	ctx       context.Context
	cancelCtx context.CancelFunc
	done      chan struct{}

	// tracked holds every accepted-but-not-yet-delivered raw connection, so
	// Close can drop them. A connection being sniffed is not visible to
	// srv.Shutdown (net/http has not seen it yet), so without this the main port
	// would stay held until the sniff timeout expired.
	mu      sync.Mutex
	pending map[net.Conn]struct{}
}

// newRedirectMux starts routing connections from raw.
//
// The returned TLS listener is what srv.ServeTLS should serve; the redirect
// handler runs on its own http.Server inside the mux.
func newRedirectMux(raw net.Listener, port int) (*redirectMux, net.Listener) {
	ctx, cancelCtx := context.WithCancel(context.Background())
	m := &redirectMux{
		raw:       raw,
		tlsCh:     make(chan net.Conn),
		redirCh:   make(chan net.Conn),
		ctx:       ctx,
		cancelCtx: cancelCtx,
		done:      make(chan struct{}),
		pending:   make(map[net.Conn]struct{}),
	}

	redirSrv := &http.Server{
		Handler:           redirectToHTTPS(port),
		ReadHeaderTimeout: redirectHeaderTimeout,
	}
	// Closing either listener stops the whole mux: both share one ctx, and the
	// raw listener is what actually frees the port. http.Server.Shutdown closes
	// the listener it was given, so this hook is the ONLY thing that makes
	// Shutdown return — a no-op Close here hangs the process on Ctrl+C.
	closeMux := &sync.Once{}
	stopAll := func() {
		cancelCtx()
		_ = raw.Close()
	}
	redirLn := &chanListener{ch: m.redirCh, done: ctx.Done(), addr: raw.Addr(), closeOnce: closeMux, onClose: stopAll}
	go func() {
		// A real error here means the redirect side died; the TLS side keeps
		// working, so this is a warning, not a fatal. The ctx check filters out
		// the listener closing on shutdown, which surfaces as a plain
		// "use of closed network connection" rather than ErrServerClosed.
		if err := redirSrv.Serve(redirLn); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			slog.Warn("https redirect listener stopped", slog.String("err", err.Error()))
		}
	}()
	go func() {
		<-ctx.Done()
		// Close, not Shutdown: an unanswered redirect is not worth draining.
		_ = redirSrv.Close()
	}()

	go m.acceptLoop()

	tlsLn := &chanListener{ch: m.tlsCh, done: ctx.Done(), addr: raw.Addr(), closeOnce: closeMux, onClose: stopAll}
	return m, tlsLn
}

func (m *redirectMux) acceptLoop() {
	defer close(m.done)
	defer close(m.tlsCh)
	defer close(m.redirCh)
	for {
		conn, err := m.raw.Accept()
		if err != nil {
			if !isClosedErr(err) && m.ctx.Err() == nil {
				slog.Error("redirect mux accept failed", slog.String("err", err.Error()))
			}
			return
		}
		m.track(conn)
		go m.classify(conn)
	}
}

// Close stops accepting and drops everything still being sniffed, then closes
// the raw listener — which is what actually frees the main port.
func (m *redirectMux) Close() error {
	m.cancelCtx()
	err := m.raw.Close()
	<-m.done

	m.mu.Lock()
	for conn := range m.pending {
		_ = conn.Close()
		delete(m.pending, conn)
	}
	m.mu.Unlock()
	return err
}

func (m *redirectMux) track(conn net.Conn) {
	m.mu.Lock()
	m.pending[conn] = struct{}{}
	m.mu.Unlock()
}

func (m *redirectMux) classify(conn net.Conn) {
	// Bound the sniff: a silent client must not hold this goroutine forever.
	_ = conn.SetReadDeadline(time.Now().Add(sniffTimeout))
	head := make([]byte, sniffBytes)
	n, _ := readAtLeast(conn, head)
	_ = conn.SetReadDeadline(time.Time{})

	if n == 0 {
		// The peer connected and sent nothing (or closed). There is nothing to
		// serve, and guessing a side would only waste a handler slot.
		m.drop(conn)
		return
	}

	wrapped := &prefixConn{Conn: conn, pre: bytes.Clone(head[:n])}
	if sniffTLS(head[:n]) {
		m.deliver(wrapped, conn, m.tlsCh)
		return
	}
	m.deliver(wrapped, conn, m.redirCh)
}

func (m *redirectMux) deliver(wrapped *prefixConn, raw net.Conn, ch chan<- net.Conn) {
	select {
	case ch <- wrapped:
		// Ownership passed to an http.Server; stop tracking it.
		m.untrack(raw)
	case <-m.cancelC():
		m.drop(raw)
	}
}

func (m *redirectMux) drop(conn net.Conn) {
	_ = conn.Close()
	m.untrack(conn)
}

func (m *redirectMux) untrack(conn net.Conn) {
	m.mu.Lock()
	delete(m.pending, conn)
	m.mu.Unlock()
}

func (m *redirectMux) cancelC() <-chan struct{} { return m.ctx.Done() }

// readAtLeast reads until buf is full or the peer errors. Returning the short
// count lets a client that half-closed still be classified by what it sent.
func readAtLeast(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// chanListener exposes a channel of pre-accepted connections as a net.Listener,
// so net/http's Serve loop can own them unchanged.
//
// Close MUST actually stop Accept. http.Server.Shutdown closes every listener
// and then waits on the accept loop it started; a no-op Close leaves Accept
// blocked forever, so Shutdown never returns, the process hangs on Ctrl+C and
// the main port stays bound. closeOnce is shared by both listeners so the
// cancel runs exactly once even though Shutdown closes both.
type chanListener struct {
	ch        <-chan net.Conn
	done      <-chan struct{}
	addr      net.Addr
	closeOnce *sync.Once
	onClose   func()
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c, ok := <-l.ch:
		if !ok {
			return nil, net.ErrClosed
		}
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	l.closeOnce.Do(l.onClose)
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

// isClosedErr reports whether an accept error means "listener was closed",
// including Windows' differently-worded error.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "network connection was aborted")
}
