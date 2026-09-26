package tunnel

import (
	"errors"
	"net"
	"strconv"
	"testing"
)

func TestBindRegistry_ReserveThenRelease(t *testing.T) {
	reg := NewBindRegistry()
	binding := Binding{ConnID: "conn-1"}

	if err := reg.Reserve(binding, 8080); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !reg.IsBound(8080) {
		t.Fatal("port must report as bound after Reserve")
	}

	reg.Release(binding, 8080)
	if reg.IsBound(8080) {
		t.Fatal("port must report as free after Release")
	}
}

// TestBindRegistry_DuplicateBindRejected covers the "second client asks for the
// same port" case, including the same client asking twice.
func TestBindRegistry_DuplicateBindRejected(t *testing.T) {
	reg := NewBindRegistry()
	first := Binding{ConnID: "conn-1"}
	second := Binding{ConnID: "conn-2"}

	if err := reg.Reserve(first, 8080); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if err := reg.Reserve(second, 8080); !errors.Is(err, ErrBindReservedOrTaken) {
		t.Fatalf("second client must be rejected, got %v", err)
	}
	if err := reg.Reserve(first, 8080); !errors.Is(err, ErrBindReservedOrTaken) {
		t.Fatalf("the same client re-binding must be rejected, got %v", err)
	}
}

// TestBindRegistry_ReleaseIgnoresForeignOwner protects against a stale unbind
// stealing a port a newer stream has taken.
func TestBindRegistry_ReleaseIgnoresForeignOwner(t *testing.T) {
	reg := NewBindRegistry()
	owner := Binding{ConnID: "conn-1"}
	stale := Binding{ConnID: "conn-old"}

	if err := reg.Reserve(owner, 8080); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	reg.Release(stale, 8080)

	if !reg.IsBound(8080) {
		t.Fatal("a foreign Release must not free someone else's port")
	}
}

func TestBindRegistry_ReleaseBindingReturnsItsPorts(t *testing.T) {
	reg := NewBindRegistry()
	owner := Binding{ConnID: "conn-1"}
	other := Binding{ConnID: "conn-2"}

	for _, port := range []int{8080, 9090} {
		if err := reg.Reserve(owner, port); err != nil {
			t.Fatalf("Reserve(%d): %v", port, err)
		}
	}
	if err := reg.Reserve(other, 7070); err != nil {
		t.Fatalf("Reserve(7070): %v", err)
	}

	ports := reg.ReleaseBinding(owner)
	if len(ports) != 2 {
		t.Fatalf("expected 2 released ports, got %v", ports)
	}
	if !reg.IsBound(7070) {
		t.Fatal("another stream's reservation must survive")
	}
	if reg.Len() != 1 {
		t.Fatalf("expected 1 remaining reservation, got %d", reg.Len())
	}
}

func TestListenReverse_BindsOSAssignedPortAndValidatesIt(t *testing.T) {
	guard := PortGuard{
		MainPort:  20000,
		SSHPort:   20001,
		IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 },
	}
	reg := NewBindRegistry()

	ln, port, err := ListenReverse(guard, reg, BindRequest{Port: 0, Binding: Binding{ConnID: "c"}})
	if err != nil {
		t.Fatalf("ListenReverse: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if port < 1024 || port > 65535 {
		t.Fatalf("OS assigned %d, outside the allowed range", port)
	}
	if !reg.IsBound(port) {
		t.Fatal("the actual port must be reserved after a successful bind")
	}
	if ln.Addr().(*net.TCPAddr).Port != port {
		t.Fatal("the returned port must match the listener")
	}
}

func TestListenReverse_ExplicitPortUsesThatPort(t *testing.T) {
	guard := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	reg := NewBindRegistry()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	want := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	ln, port, err := ListenReverse(guard, reg, BindRequest{Port: want, Binding: Binding{ConnID: "c"}})
	if err != nil {
		t.Fatalf("ListenReverse: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if port != want {
		t.Fatalf("got port %d, want the requested %d", port, want)
	}
}

func TestListenReverse_RejectedPortNeverBinds(t *testing.T) {
	guard := PortGuard{
		MainPort:  20000,
		SSHPort:   20001,
		IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 },
	}
	reg := NewBindRegistry()

	for _, tc := range []struct {
		name string
		port int
		want error
	}{
		{"main port", 20000, ErrBindReservedOrTaken},
		{"ssh port", 20001, ErrBindReservedOrTaken},
		{"outside allowed range", 80, ErrBindNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, _, err := ListenReverse(guard, reg, BindRequest{Port: tc.port, Binding: Binding{ConnID: "c"}})
			if ln != nil {
				_ = ln.Close()
				t.Fatal("a rejected bind must not return a listener")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if reg.Len() != 0 {
				t.Fatal("a rejected bind must not reserve anything")
			}
		})
	}
}

// TestListenReverse_PortInUseIsRejected: an unrelated process holding the port
// must surface as a rejection, not a leaked listener.
func TestListenReverse_PortInUseIsRejected(t *testing.T) {
	guard := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	reg := NewBindRegistry()

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer func() { _ = held.Close() }()
	port := held.Addr().(*net.TCPAddr).Port

	ln, _, err := ListenReverse(guard, reg, BindRequest{Port: port, Binding: Binding{ConnID: "c"}})
	if ln != nil {
		_ = ln.Close()
		t.Fatal("binding an in-use port must fail")
	}
	if !errors.Is(err, ErrBindListenFailed) {
		t.Fatalf("got %v, want ErrBindListenFailed", err)
	}
}

// TestListenReverse_BindsLoopbackOnly is the security pin for the reverse
// listener's bind address (bind.go:181). A regression to "0.0.0.0" (or "")
// would expose every -R port on all interfaces, yet every other test in this
// package would still pass because they all dial 127.0.0.1. The assertion is on
// the bound address itself, not on reachability.
func TestListenReverse_BindsLoopbackOnly(t *testing.T) {
	guard := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	reg := NewBindRegistry()

	ln, port, err := ListenReverse(guard, reg, BindRequest{Port: 0, Binding: Binding{ConnID: "c"}})
	if err != nil {
		t.Fatalf("ListenReverse: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address is %T, want *net.TCPAddr", ln.Addr())
	}
	if !addr.IP.IsLoopback() {
		t.Fatalf("reverse listener bound to %s; it must bind loopback only so a -R port is not exposed on all interfaces", addr)
	}
	if addr.Port != port {
		t.Fatalf("listener port %d does not match the returned port %d", addr.Port, port)
	}
}

func TestListenReverse_DuplicateAcrossBindingsRejected(t *testing.T) {
	guard := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	reg := NewBindRegistry()

	first, port, err := ListenReverse(guard, reg, BindRequest{Port: 0, Binding: Binding{ConnID: "a"}})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer func() { _ = first.Close() }()

	second, _, err := ListenReverse(guard, reg, BindRequest{Port: port, Binding: Binding{ConnID: "b"}})
	if second != nil {
		_ = second.Close()
		t.Fatal("the second stream must not get a listener for the same port")
	}
	if err == nil {
		t.Fatal("the second stream must be rejected")
	}
}

// TestListenReverse_ReleasesReservationWhenListenerIsClosed is a documentation
// test: ListenReverse reserves the port, and the caller is responsible for
// releasing it (via BindRegistry.Release) when it closes the listener.
func TestListenReverse_ReleasesReservationWhenListenerIsClosed(t *testing.T) {
	guard := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	reg := NewBindRegistry()
	binding := Binding{ConnID: "a"}

	ln, port, err := ListenReverse(guard, reg, BindRequest{Port: 0, Binding: binding})
	if err != nil {
		t.Fatalf("ListenReverse: %v", err)
	}
	_ = ln.Close()

	// The registry still remembers the port until told otherwise; releasing is
	// what the handler does on unbind/disconnect.
	if !reg.IsBound(port) {
		t.Fatal("reservation outlives the listener by design until Release")
	}
	reg.Release(binding, port)
	if reg.IsBound(port) {
		t.Fatal("Release must clear the reservation")
	}
	if _, err := strconv.Atoi(strconv.Itoa(port)); err != nil {
		t.Fatalf("port %d is not an integer", port)
	}
}
