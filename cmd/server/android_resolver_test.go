package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// withAndroidEnv installs the environment of one Termux-like run: an Android
// build, a system resolv.conf with the given body, and a Termux prefix holding
// another. It restores everything afterwards, including net.DefaultResolver.
func withAndroidEnv(t *testing.T, systemConf, prefixConf string) {
	t.Helper()
	dir := t.TempDir()
	system := filepath.Join(dir, "system-resolv.conf")
	if systemConf != "" {
		if err := os.WriteFile(system, []byte(systemConf), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prefix := filepath.Join(dir, "prefix")
	if err := os.MkdirAll(filepath.Join(prefix, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if prefixConf != "" {
		if err := os.WriteFile(filepath.Join(prefix, "etc", "resolv.conf"), []byte(prefixConf), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	origConf, origGOOS, origResolver := androidResolvConf, runtimeGOOS, net.DefaultResolver
	androidResolvConf = system
	runtimeGOOS = "android"
	t.Setenv("PREFIX", prefix)
	t.Cleanup(func() {
		androidResolvConf, runtimeGOOS, net.DefaultResolver = origConf, origGOOS, origResolver
	})
}

func TestSetupAndroidResolver_InstallsResolver(t *testing.T) {
	withAndroidEnv(t, "", "nameserver 119.29.29.29\nnameserver 223.5.5.5\n")
	before := net.DefaultResolver

	if err := setupAndroidResolver(); err != nil {
		t.Fatalf("setupAndroidResolver: %v", err)
	}
	if net.DefaultResolver == before {
		t.Fatal("resolver must be replaced")
	}
	if !net.DefaultResolver.PreferGo {
		t.Error("PreferGo is required, otherwise Dial is never called")
	}
}

// TestDialFirstServer covers the behaviour the installed resolver relies on:
// which servers are tried, in which order, and that the last error is the one
// reported. The dial function is injected, so this asserts on the addresses
// rather than on Go's lookup machinery.
func TestDialFirstServer(t *testing.T) {
	ctx := context.Background()
	servers := []string{"119.29.29.29:53", "223.5.5.5:53"}

	t.Run("first server wins", func(t *testing.T) {
		var tried []string
		conn, err := dialFirstServer(ctx, "udp", servers, func(_ context.Context, network, addr string) (net.Conn, error) {
			tried = append(tried, network+" "+addr)
			return nil, errors.New("unreachable")
		})
		if conn != nil || err == nil {
			t.Fatalf("want no conn and an error, got %v %v", conn, err)
		}
		if len(tried) != 2 || tried[0] != "udp 119.29.29.29:53" || tried[1] != "udp 223.5.5.5:53" {
			t.Errorf("tried %v, want both servers in order over the given network", tried)
		}
	})

	t.Run("stops at the first success", func(t *testing.T) {
		var tried []string
		want, wantPeer := net.Pipe()
		defer want.Close()
		defer wantPeer.Close()
		got, err := dialFirstServer(ctx, "tcp", servers, func(_ context.Context, _, addr string) (net.Conn, error) {
			tried = append(tried, addr)
			return want, nil
		})
		if err != nil || got != want {
			t.Fatalf("want the successful conn, got %v / %v", got, err)
		}
		if len(tried) != 1 || tried[0] != servers[0] {
			t.Errorf("tried %v, want only the first server", tried)
		}
	})

	t.Run("keeps the last error", func(t *testing.T) {
		second := errors.New("second")
		_, err := dialFirstServer(ctx, "udp", servers, func(_ context.Context, _, addr string) (net.Conn, error) {
			if addr == servers[1] {
				return nil, second
			}
			return nil, errors.New("first")
		})
		if !errors.Is(err, second) {
			t.Errorf("got %v, want the last error", err)
		}
	})
}

func TestSetupAndroidResolver_NoopOutsideAndroid(t *testing.T) {
	withAndroidEnv(t, "", "nameserver 1.1.1.1\n")
	runtimeGOOS = "linux"

	before := net.DefaultResolver
	if err := setupAndroidResolver(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if net.DefaultResolver != before {
		t.Error("desktop builds must not touch the resolver")
	}
}

func TestSetupAndroidResolver_NoopWhenSystemConfigWorks(t *testing.T) {
	// The Android app runs as uid 1000 and its /etc/resolv.conf is populated by
	// bionic; replacing the resolver there would only add a dependency on
	// $PREFIX, which the app does not have.
	withAndroidEnv(t, "nameserver 10.0.0.1\n", "nameserver 1.1.1.1\n")

	before := net.DefaultResolver
	if err := setupAndroidResolver(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if net.DefaultResolver != before {
		t.Error("a working system resolver must be left alone")
	}
}

func TestSetupAndroidResolver_TreatsLoopbackAsBroken(t *testing.T) {
	// A resolv.conf that only names a loopback server is what a broken Android
	// configuration degrades to; taking it as "the system works" would leave
	// every lookup failing.
	withAndroidEnv(t, "nameserver [::1]\nnameserver 127.0.0.1\n", "nameserver 1.1.1.1\n")

	before := net.DefaultResolver
	if err := setupAndroidResolver(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if net.DefaultResolver == before {
		t.Error("loopback-only configuration must be replaced")
	}
}

func TestSetupAndroidResolver_NoopWithoutTermuxPrefix(t *testing.T) {
	withAndroidEnv(t, "", "")
	t.Setenv("PREFIX", "")

	before := net.DefaultResolver
	if err := setupAndroidResolver(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if net.DefaultResolver != before {
		t.Error("without a Termux prefix there is nothing to install")
	}
}

func TestSetupAndroidResolver_ReportsMissingTermuxConfig(t *testing.T) {
	// Looks like Termux but $PREFIX/etc/resolv.conf is missing: report it so
	// the operator sees why DNS is still broken, and leave the resolver alone.
	withAndroidEnv(t, "", "")
	before := net.DefaultResolver

	if err := setupAndroidResolver(); err == nil {
		t.Fatal("want an error naming the missing file")
	}
	if net.DefaultResolver != before {
		t.Error("a failed setup must not install a resolver")
	}
}

func TestReadNameservers(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "resolv.conf")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "comments, options and CRLF",
			body: "# generated\nsearch lan\noptions ndots:2\n" +
				"nameserver 8.8.8.8\n" +
				"nameserver 119.29.29.29 # primary\r\n",
			want: []string{"8.8.8.8:53", "119.29.29.29:53"},
		},
		{
			name: "capped like the Go resolver",
			body: "nameserver 1.1.1.1\nnameserver 2.2.2.2\nnameserver 3.3.3.3\nnameserver 4.4.4.4\n",
			want: []string{"1.1.1.1:53", "2.2.2.2:53", "3.3.3.3:53"},
		},
		{
			name: "IPv6 bare and bracketed",
			body: "nameserver 2606:4700:4700::1111\nnameserver [2001:4860:4860::8888]\n",
			want: []string{"[2606:4700:4700::1111]:53", "[2001:4860:4860::8888]:53"},
		},
		{
			// Go skips anything that is not an IP address; a host:port form here
			// would otherwise reach net.JoinHostPort and produce garbage.
			name: "not an address",
			body: "nameserver dns.example.com\nnameserver 1.1.1.1#53\nnameserver\n",
		},
		{name: "nothing usable", body: "search lan\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readNameservers(write(t, tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}

	if got := readNameservers(filepath.Join(t.TempDir(), "absent")); got != nil {
		t.Errorf("missing file must yield nothing, got %v", got)
	}
}
