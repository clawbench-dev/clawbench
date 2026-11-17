package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// androidResolvConf is the file the Go resolver reads on Android. It is a
// variable so tests can point it at fixtures.
var androidResolvConf = "/etc/resolv.conf"

// runtimeGOOS is a variable so tests can exercise the Android branch on Linux.
var runtimeGOOS = runtime.GOOS

// maxAndroidNameservers matches the cap the Go resolver applies when it reads a
// resolv.conf, so the list we install never carries servers Go would ignore.
const maxAndroidNameservers = 3

// setupAndroidResolver gives the server a working resolver when it runs in
// Termux, where Go cannot read the system DNS configuration.
//
// Why this is needed (issue #679): the binary is built with GOOS=android and
// CGO_ENABLED=0, so package net always uses the pure Go resolver, and Go's
// Android build takes the nameserver list from Android's property storage. A
// cross-compiled binary sees that as /etc/resolv.conf, but its content is
// produced by bionic and only the zygote (uid 1000) may ask init for it.
// Termux's shell is uid 10123, the read is denied, Go falls back to the
// built-in [::1]:53, and every lookup fails with "connection refused" — mail,
// the model catalog and the wallpaper fetch all died that way.
//
// Termux keeps a perfectly good resolv.conf in $PREFIX/etc. The fix is to read
// the nameservers from there and hand them to the Go resolver directly, through
// net.DefaultResolver. No privileges, no filesystem changes, and it covers
// every lookup the server makes: net.Dial, http.Transport and tls.Dial all use
// the default resolver.
//
// It does nothing — and returns nil — everywhere the problem cannot occur:
// non-Android builds, the Android app itself (uid 1000, whose /etc/resolv.conf
// is populated) and any host whose system resolv.conf already lists a server.
// An error is returned only when the situation looks like Termux but no usable
// nameserver can be found; the caller reports it and keeps running, because a
// server with broken DNS still serves local data.
func setupAndroidResolver() error {
	if runtimeGOOS != "android" {
		return nil
	}
	if len(usableNameservers(androidResolvConf)) > 0 {
		// The system file works: this is the app, or a future Android that
		// serves the same content to every uid. Nothing to do.
		return nil
	}
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		return nil
	}
	source := filepath.Join(prefix, "etc", "resolv.conf")
	servers := readNameservers(source)
	if len(servers) == 0 {
		return fmt.Errorf("%s has no usable nameserver", source)
	}

	net.DefaultResolver = &net.Resolver{
		// Required for Dial to be used at all: the cgo resolver ignores it, and
		// on Android it is the pure Go resolver we are replacing.
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialFirstServer(ctx, network, servers, (&net.Dialer{}).DialContext)
		},
	}
	slog.Info("android resolver: using Termux DNS configuration",
		"source", source, "nameservers", len(servers))
	return nil
}

// dialFirstServer dials the servers in order and returns the first connection.
//
// The address Go passes to Dial is ignored on purpose: it comes from the
// /etc/resolv.conf Go failed to load, i.e. the unusable [::1]:53. Go calls Dial
// once per server in its own config and retries within its lookup budget, so a
// second retry loop here would only multiply timeouts — hence no retries.
func dialFirstServer(ctx context.Context, network string, servers []string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	var lastErr error
	for _, server := range servers {
		conn, err := dial(ctx, network, server)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// readNameservers returns the nameserver addresses in a resolv.conf, each with
// port 53, capped like the Go resolver caps them. Entries that are not an IP
// address are skipped, as the Go resolver skips them; an unreadable file yields
// nothing.
func readNameservers(path string) []string {
	parsed := parseNameserverAddrs(path)
	servers := make([]string, 0, len(parsed))
	for _, addr := range parsed {
		servers = append(servers, net.JoinHostPort(addr.String(), "53"))
	}
	return servers
}

// usableNameservers returns the addresses in path that a working system would
// actually use: parsed like the Go resolver parses them, minus loopback and
// unspecified addresses, which is what a broken configuration degrades to.
func usableNameservers(path string) []netip.Addr {
	var out []netip.Addr
	for _, addr := range parseNameserverAddrs(path) {
		if addr.IsLoopback() || addr.IsUnspecified() {
			continue
		}
		out = append(out, addr)
	}
	return out
}

func parseNameserverAddrs(path string) []netip.Addr {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var addrs []netip.Addr
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		// resolv.conf writes IPv6 servers bare; the bracketed form belongs to
		// host:port syntax and would make ParseAddr reject the entry.
		addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(f[1], "["), "]"))
		if err != nil {
			continue
		}
		addrs = append(addrs, addr)
		if len(addrs) == maxAndroidNameservers {
			break
		}
	}
	return addrs
}
