package tunnel

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// Bind errors, mapped by the handler onto bind_err.code values.
var (
	// ErrBindNotAllowed means the port is outside allowed_ports (code 2).
	ErrBindNotAllowed = errors.New("port not allowed")
	// ErrBindReservedOrTaken means the port is reserved for ClawBench itself or
	// already bound by another control stream (code 3).
	ErrBindReservedOrTaken = errors.New("port reserved or already in use")
	// ErrBindListenFailed is a net.Listen failure that is not address-in-use
	// (code 4).
	ErrBindListenFailed = errors.New("listen failed")
)

// BindRegistry is the process-wide reverse-bind table: which server ports are
// currently listening on behalf of which authenticated connection, and the
// listeners themselves.
//
// It is global (not per control stream) for two reasons:
//
//   - Duplicate-bind rejection has to be global: two different clients asking
//     for the same port must be told apart deterministically, which a
//     per-stream table cannot express.
//   - Ownership must survive independently of the goroutine that created it, so
//     cleanup can be driven by the connection identity the design specifies
//     ("release everything under this connection").
//
// It owns the listeners so closing a listener and forgetting it are one atomic
// step: a half-released bind cannot leave an Accept loop running against an
// entry that is no longer in the table.
type BindRegistry struct {
	mu    sync.Mutex
	binds map[int]boundListener
}

// boundListener is one reverse listener plus the connection that owns it.
type boundListener struct {
	owner Binding
	ln    net.Listener
}

// NewBindRegistry returns an empty registry.
func NewBindRegistry() *BindRegistry {
	return &BindRegistry{binds: make(map[int]boundListener)}
}

// Reserve records port as owned by binding WITHOUT a listener. Used by tests
// and by callers that bind outside ListenReverse; the production path is
// ListenReverse, which reserves and listens together.
func (r *BindRegistry) Reserve(binding Binding, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reserveLocked(binding, port)
}

// reserveLocked claims port for binding, reporting ErrBindReservedOrTaken if it
// is taken. The caller must hold r.mu.
func (r *BindRegistry) reserveLocked(binding Binding, port int) error {
	if _, taken := r.binds[port]; taken {
		return fmt.Errorf("%w: port %d", ErrBindReservedOrTaken, port)
	}
	r.binds[port] = boundListener{owner: binding}
	return nil
}

// Release drops a reservation for binding, closing its listener, and reports
// whether it did. It is a no-op (returning false) if the port is not reserved
// or is owned by a different connection, so a late unbind from an old stream
// can neither steal a port a new stream has taken nor clear that stream's
// registry Active flag.
func (r *BindRegistry) Release(binding Binding, port int) bool {
	r.mu.Lock()
	entry, ok := r.binds[port]
	if !ok || entry.owner != binding {
		r.mu.Unlock()
		return false
	}
	delete(r.binds, port)
	r.mu.Unlock()

	if entry.ln != nil {
		_ = entry.ln.Close()
	}
	return true
}

// ReleaseBinding drops every reservation owned by binding, closes their
// listeners, and returns the released ports so the caller can clear the
// registry's Active flags. Used when a control stream ends.
func (r *BindRegistry) ReleaseBinding(binding Binding) []int {
	r.mu.Lock()
	var ports []int
	var listeners []net.Listener
	for port, entry := range r.binds {
		if entry.owner == binding {
			ports = append(ports, port)
			if entry.ln != nil {
				listeners = append(listeners, entry.ln)
			}
			delete(r.binds, port)
		}
	}
	r.mu.Unlock()

	// Close outside the lock: a listener whose Accept is mid-flight must not be
	// able to block an unrelated bind.
	for _, ln := range listeners {
		_ = ln.Close()
	}
	return ports
}

// IsBound reports whether any connection currently owns port.
func (r *BindRegistry) IsBound(port int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.binds[port]
	return ok
}

// Len reports the number of active reservations. Used by tests.
func (r *BindRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.binds)
}

// BindRequest is a reverse bind request from a control stream.
type BindRequest struct {
	// Port is the requested server port; 0 means "let the OS choose".
	Port int
	// Binding is the authenticated connection making the request.
	Binding Binding
}

// ListenReverse performs the full reverse-bind sequence and returns the
// listener plus the port it is actually bound to.
//
// Order matters, so a rejection never leaves a half-established bind:
//
//  1. Pre-listen guard: a non-zero port is validated before anything is bound.
//  2. Pre-listen duplicate check: a non-zero port already owned by any
//     connection is rejected here, so a sequential duplicate gets the
//     deterministic ErrBindReservedOrTaken rather than depending on the
//     kernel's error for a double bind.
//  3. net.Listen on loopback (port 0 lets the OS assign).
//  4. Post-listen guard: the ACTUAL port is re-validated. For a port-0 request
//     this is the only policy check that runs — the OS picked the port, so it
//     must be verified before it is exposed. For a non-zero request it is a
//     cheap re-check.
//  5. Authoritative reservation under the registry lock, storing the listener
//     atomically. This is what closes the race between step 2 and step 3: two
//     concurrent binds of the same port cannot both reach this point.
//
// Any failure after the listener exists closes it, so a failed bind is
// indistinguishable from one that never started.
//
// A kernel-level bind failure (including a port held by an unrelated process)
// is reported as ErrBindListenFailed, not ErrBindReservedOrTaken: detecting
// address-in-use portably would need per-OS errno handling (syscall.EADDRINUSE
// is an invented value on Windows and never matches the real WSAEADDRINUSE),
// and the client's reaction is identical — pick another port.
func ListenReverse(guard PortGuard, reg *BindRegistry, req BindRequest) (net.Listener, int, error) {
	if req.Port != 0 {
		if err := guard.ReverseBindDenied(req.Port); err != nil {
			return nil, 0, err
		}
		if reg.IsBound(req.Port) {
			return nil, 0, fmt.Errorf("%w: port %d", ErrBindReservedOrTaken, req.Port)
		}
	}

	//nolint:noctx // listener is owned by the control stream, not a request
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(req.Port)))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrBindListenFailed, err)
	}

	actual := listenerPort(ln)
	if actual == 0 {
		_ = ln.Close()
		return nil, 0, ErrBindListenFailed
	}

	if deniedErr := guard.ReverseBindDenied(actual); deniedErr != nil {
		_ = ln.Close()
		return nil, 0, deniedErr
	}

	reg.mu.Lock()
	err = reg.reserveLocked(req.Binding, actual)
	if err == nil {
		reg.binds[actual] = boundListener{owner: req.Binding, ln: ln}
	}
	reg.mu.Unlock()
	if err != nil {
		_ = ln.Close()
		return nil, 0, err
	}

	return ln, actual, nil
}

// listenerPort extracts the numeric port from a listener's address.
func listenerPort(ln net.Listener) int {
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		return tcp.Port
	}
	return 0
}
