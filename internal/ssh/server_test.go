package ssh

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	crypto_sha256 "crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// testServerHelper creates and starts an SSH server on a random port for testing.
func testServerHelper(t *testing.T, password string, portReg *service.ProxyRegistry) *Server {
	t.Helper()
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, password, portReg)

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	srv.addr = fmt.Sprintf("127.0.0.1:%d", port)

	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	return srv
}

// testSSHClient connects to an SSH server with the given credentials.
func testSSHClient(t *testing.T, addr, user, password string) *gossh.Client { //nolint:unparam // user param kept for API clarity
	t.Helper()
	clientCfg := &gossh.ClientConfig{
		User: user,
		Auth: []gossh.AuthMethod{
			gossh.Password(password),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := gossh.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("failed to connect to SSH server %s: %v", addr, err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// startEchoServer starts a TCP echo server on a random port and returns the port.
func startEchoServer(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo server: %v", err)
	}
	addr := listener.Addr().String()
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return port
}

func newTestRegistry(t *testing.T) *service.ProxyRegistry {
	t.Helper()
	r := service.NewProxyRegistry(0)
	t.Cleanup(func() { r.Stop() })
	return r
}

// --- SHA-256 Password Auth Tests ---

func TestSSHServer_SHA256PasswordAuth_Success(t *testing.T) {
	portReg := newTestRegistry(t)
	// Store password as SHA-256 hash
	sha256Password := "sha256:" + sha256Hex("my-secret-password")
	srv := testServerHelper(t, sha256Password, portReg)

	// Authenticate with the plaintext password — server should hash and compare
	client := testSSHClient(t, srv.addr, "clawbench", "my-secret-password")

	// Verify connection works
	if err := client.Close(); err != nil {
		t.Errorf("failed to close client: %v", err)
	}
}

func TestSSHServer_SHA256PasswordAuth_WrongPassword(t *testing.T) {
	portReg := newTestRegistry(t)
	sha256Password := "sha256:" + sha256Hex("correct-password")
	srv := testServerHelper(t, sha256Password, portReg)

	clientCfg := &gossh.ClientConfig{
		User: "clawbench",
		Auth: []gossh.AuthMethod{
			gossh.Password("wrong-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth failure for wrong password with SHA-256 stored hash")
	}
}

func TestSSHServer_SHA256PasswordAuth_WrongUser(t *testing.T) {
	portReg := newTestRegistry(t)
	sha256Password := "sha256:" + sha256Hex("my-secret-password")
	srv := testServerHelper(t, sha256Password, portReg)

	clientCfg := &gossh.ClientConfig{
		User: "root",
		Auth: []gossh.AuthMethod{
			gossh.Password("my-secret-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth failure for wrong username with SHA-256 stored hash")
	}
}

// sha256Hex returns the SHA-256 hex digest of (s + "clawbench-salt"), matching the server's derivation.
func sha256Hex(s string) string {
	h := crypto_sha256.Sum256([]byte(s + "clawbench-salt"))
	return hex.EncodeToString(h[:])
}

// --- Connection & Auth Tests ---

func TestSSHServerConnectAndDisconnect(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	if srv.Fingerprint() == "" {
		t.Error("expected non-empty fingerprint")
	}
	if srv.Port() == 0 {
		t.Error("expected non-zero port")
	}

	// Close and verify
	if err := client.Close(); err != nil {
		t.Errorf("failed to close client: %v", err)
	}
}

func TestSSHServerAuthFailure_WrongPassword(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "correct-password", portReg)

	clientCfg := &gossh.ClientConfig{
		User: "clawbench",
		Auth: []gossh.AuthMethod{
			gossh.Password("wrong-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth failure for wrong password")
	}
}

func TestSSHServerAuthFailure_WrongUser(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	clientCfg := &gossh.ClientConfig{
		User: "root",
		Auth: []gossh.AuthMethod{
			gossh.Password("test-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth failure for wrong username")
	}
}

// --- Port Forwarding Tests ---

func TestSSHPortForward_AllowedButUnregisteredPortWorks(t *testing.T) {
	// SSH tunnels only check IsPortAllowed (port range), not IsPortRegistered.
	// Registration is for the HTTP reverse-proxy (protocol metadata), not SSH tunnels.
	portReg := newTestRegistry(t)
	echoPort := startEchoServer(t)
	// Deliberately do NOT register echoPort — it's in the allowed range (1024-65535)

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	conn, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort))
	if err != nil {
		t.Fatalf("expected port forwarding to work for allowed-but-unregistered port %d, got: %v", echoPort, err)
	}
	defer conn.Close()

	testMsg := []byte("hello unregistered port")
	_, err = conn.Write(testMsg)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if !bytes.Equal(buf[:n], testMsg) {
		t.Errorf("echo mismatch: got %q, want %q", string(buf[:n]), string(testMsg))
	}
}

func TestSSHPortForward_DisallowedPortRejectedByTunnel(t *testing.T) {
	// Create a registry that only allows specific ports
	r := service.NewProxyRegistry(0)
	r.SetAllowedPorts("3000-4000")
	defer r.Stop()

	srv := testServerHelper(t, "test-password", r)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Port 9999 is outside the allowed range (3000-4000)
	_, err := client.Dial("tcp", "127.0.0.1:9999")
	if err == nil {
		t.Error("expected port forwarding to be rejected for port outside allowed range")
	}
}

func TestSSHPortForward_RegisteredPortWorks(t *testing.T) {
	portReg := newTestRegistry(t)
	echoPort := startEchoServer(t)
	portReg.RegisterPort(echoPort, "", "echo", "http", "")

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	conn, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort))
	if err != nil {
		t.Fatalf("expected port forwarding to work for registered port %d, got: %v", echoPort, err)
	}
	defer conn.Close()

	testMsg := []byte("hello ssh tunnel")
	_, err = conn.Write(testMsg)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if !bytes.Equal(buf[:n], testMsg) {
		t.Errorf("echo mismatch: got %q, want %q", string(buf[:n]), string(testMsg))
	}
}

func TestSSHPortForward_MultiplePorts(t *testing.T) {
	portReg := newTestRegistry(t)

	// Start two echo servers
	echoPort1 := startEchoServer(t)
	echoPort2 := startEchoServer(t)
	portReg.RegisterPort(echoPort1, "", "echo1", "http", "")
	portReg.RegisterPort(echoPort2, "", "echo2", "http", "")

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Forward to port 1
	conn1, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort1))
	if err != nil {
		t.Fatalf("failed to forward to port %d: %v", echoPort1, err)
	}
	defer conn1.Close()

	// Forward to port 2
	conn2, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort2))
	if err != nil {
		t.Fatalf("failed to forward to port %d: %v", echoPort2, err)
	}
	defer conn2.Close()

	// Test both connections
	testMsg1 := []byte("port1")
	conn1.Write(testMsg1)
	buf1 := make([]byte, 1024)
	conn1.SetReadDeadline(time.Now().Add(5 * time.Second))
	n1, _ := conn1.Read(buf1)
	if !bytes.Equal(buf1[:n1], testMsg1) {
		t.Errorf("port1 echo mismatch: got %q", string(buf1[:n1]))
	}

	testMsg2 := []byte("port2")
	conn2.Write(testMsg2)
	buf2 := make([]byte, 1024)
	conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	n2, _ := conn2.Read(buf2)
	if !bytes.Equal(buf2[:n2], testMsg2) {
		t.Errorf("port2 echo mismatch: got %q", string(buf2[:n2]))
	}
}

func TestSSHPortForward_DisallowedPortRejected(t *testing.T) {
	// Create a registry that only allows specific ports
	r := service.NewProxyRegistry(0)
	r.SetAllowedPorts("3000-4000")
	defer r.Stop()

	// RegisterPort should reject a port outside the allowed range
	_, err := r.RegisterPort(8080, "", "outside-range", "http", "")
	if err == nil {
		t.Error("expected RegisterPort to reject port 8080 (outside allowed range 3000-4000)")
	}
	for _, p := range r.ListPorts() {
		if p.Port == 8080 {
			t.Error("port 8080 should not be registered since it's outside allowed range")
		}
	}
}

func TestSSHPortForward_LargeDataTransfer(t *testing.T) {
	portReg := newTestRegistry(t)
	echoPort := startEchoServer(t)
	portReg.RegisterPort(echoPort, "", "echo", "http", "")

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	conn, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort))
	if err != nil {
		t.Fatalf("failed to forward: %v", err)
	}
	defer conn.Close()

	// Send 64KB of data
	largeMsg := make([]byte, 64*1024)
	for i := range largeMsg {
		largeMsg[i] = byte(i % 256)
	}

	_, err = conn.Write(largeMsg)
	if err != nil {
		t.Fatalf("failed to write large data: %v", err)
	}

	// Read back all data
	received := make([]byte, 0, len(largeMsg))
	buf := make([]byte, 32*1024)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for len(received) < len(largeMsg) {
		n, err := conn.Read(buf)
		if n > 0 {
			received = append(received, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	if len(received) != len(largeMsg) {
		t.Errorf("large data mismatch: got %d bytes, want %d", len(received), len(largeMsg))
	}
}

// --- Host Key Tests ---

func TestSSHServer_HostKeyPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "ssh_host_key")

	portReg := newTestRegistry(t)

	// First server: generates and saves host key
	srv1 := NewServer(model.PortForwardConfig{Enabled: true, Port: 0, HostKey: keyPath}, 0, "test", portReg)

	// Find port
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := listener.Addr().String()
	listener.Close()
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	srv1.addr = fmt.Sprintf("127.0.0.1:%d", port)

	go srv1.ListenAndServe()
	time.Sleep(100 * time.Millisecond)

	fingerprint1 := srv1.Fingerprint()
	srv1.Close()

	// Verify key file was created
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatal("host key file should have been created")
	}

	// Second server: loads existing host key
	srv2 := NewServer(model.PortForwardConfig{Enabled: true, Port: 0, HostKey: keyPath}, 0, "test", portReg)
	listener2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := listener2.Addr().String()
	listener2.Close()
	_, portStr2, _ := net.SplitHostPort(addr2)
	fmt.Sscanf(portStr2, "%d", &port)
	srv2.addr = fmt.Sprintf("127.0.0.1:%d", port)

	go srv2.ListenAndServe()
	time.Sleep(100 * time.Millisecond)

	fingerprint2 := srv2.Fingerprint()
	srv2.Close()

	// Fingerprints should match (same host key)
	if fingerprint1 != fingerprint2 {
		t.Errorf("fingerprints should match for persisted key: first=%s, second=%s", fingerprint1, fingerprint2)
	}
}

func TestSSHServer_EphemeralKeyChangesOnRestart(t *testing.T) {
	portReg := newTestRegistry(t)

	// First server with ephemeral key
	srv1 := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := listener.Addr().String()
	listener.Close()
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	srv1.addr = fmt.Sprintf("127.0.0.1:%d", port)

	go srv1.ListenAndServe()
	time.Sleep(100 * time.Millisecond)

	fp1 := srv1.Fingerprint()
	srv1.Close()

	// Second server with ephemeral key (should be different)
	srv2 := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	listener2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := listener2.Addr().String()
	listener2.Close()
	_, portStr2, _ := net.SplitHostPort(addr2)
	fmt.Sscanf(portStr2, "%d", &port)
	srv2.addr = fmt.Sprintf("127.0.0.1:%d", port)

	go srv2.ListenAndServe()
	time.Sleep(100 * time.Millisecond)

	fp2 := srv2.Fingerprint()
	srv2.Close()

	// Ephemeral keys should be different across restarts
	if fp1 == fp2 {
		t.Error("ephemeral host keys should differ between server instances")
	}
}

// --- Auto Port Tests ---

func TestSSHServer_AutoPortAssignment(t *testing.T) {
	portReg := newTestRegistry(t)

	// Port 0 should auto-assign to mainPort+1
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 20000, "test", portReg)
	if srv.Port() != 20001 {
		t.Errorf("expected auto-assigned port 20001, got %d", srv.Port())
	}

	// Explicit port should be used as-is
	srv2 := NewServer(model.PortForwardConfig{Enabled: true, Port: 22222}, 20000, "test", portReg)
	if srv2.Port() != 22222 {
		t.Errorf("expected explicit port 22222, got %d", srv2.Port())
	}
}

// --- Connection Stats Tests ---

func TestSSHServer_ConnectionStats_NoConnections(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	stats := srv.ConnectionStats()
	if stats.Connected {
		t.Error("expected Connected=false when no clients are connected")
	}
	if stats.ClientCount != 0 {
		t.Errorf("expected ClientCount=0, got %d", stats.ClientCount)
	}
	if stats.ActiveChannels != 0 {
		t.Errorf("expected ActiveChannels=0, got %d", stats.ActiveChannels)
	}
	if stats.LastConnectedAt != "" {
		t.Errorf("expected empty LastConnectedAt, got %q", stats.LastConnectedAt)
	}
}

func TestSSHServer_ConnectionStats_ClientConnected(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	// Before connecting
	stats := srv.ConnectionStats()
	if stats.Connected {
		t.Error("expected Connected=false before client connects")
	}

	// Connect a client
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Give the server a moment to update stats
	time.Sleep(50 * time.Millisecond)

	stats = srv.ConnectionStats()
	if !stats.Connected {
		t.Error("expected Connected=true after client connects")
	}
	if stats.ClientCount != 1 {
		t.Errorf("expected ClientCount=1, got %d", stats.ClientCount)
	}
	if stats.LastConnectedAt == "" {
		t.Error("expected non-empty LastConnectedAt after client connects")
	}

	// Disconnect
	client.Close()

	// Wait for server to detect disconnect
	time.Sleep(200 * time.Millisecond)

	stats = srv.ConnectionStats()
	if stats.Connected {
		t.Error("expected Connected=false after client disconnects")
	}
	if stats.ClientCount != 0 {
		t.Errorf("expected ClientCount=0 after disconnect, got %d", stats.ClientCount)
	}
}

func TestSSHServer_ConnectionStats_MultipleClients(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	client1 := testSSHClient(t, srv.addr, "clawbench", "test-password")
	client2 := testSSHClient(t, srv.addr, "clawbench", "test-password")

	time.Sleep(50 * time.Millisecond)

	stats := srv.ConnectionStats()
	if stats.ClientCount != 2 {
		t.Errorf("expected ClientCount=2, got %d", stats.ClientCount)
	}

	client1.Close()
	time.Sleep(200 * time.Millisecond)

	stats = srv.ConnectionStats()
	if stats.ClientCount != 1 {
		t.Errorf("expected ClientCount=1 after one disconnect, got %d", stats.ClientCount)
	}

	client2.Close()
	time.Sleep(200 * time.Millisecond)

	stats = srv.ConnectionStats()
	if stats.Connected {
		t.Error("expected Connected=false after all clients disconnect")
	}
}

func TestSSHServer_ConnectionStats_ActiveChannels(t *testing.T) {
	portReg := newTestRegistry(t)
	echoPort := startEchoServer(t)
	portReg.RegisterPort(echoPort, "", "echo", "http", "")

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	time.Sleep(50 * time.Millisecond)

	// Before opening any channels
	stats := srv.ConnectionStats()
	if stats.ActiveChannels != 0 {
		t.Errorf("expected ActiveChannels=0 before port forward, got %d", stats.ActiveChannels)
	}

	// Open a port forward channel
	conn, err := client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", echoPort))
	if err != nil {
		t.Fatalf("failed to dial forwarded port: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	stats = srv.ConnectionStats()
	if stats.ActiveChannels < 1 {
		t.Errorf("expected ActiveChannels>=1 after opening channel, got %d", stats.ActiveChannels)
	}

	// Close the forwarded connection
	conn.Close()
	time.Sleep(200 * time.Millisecond)

	stats = srv.ConnectionStats()
	if stats.ActiveChannels != 0 {
		t.Errorf("expected ActiveChannels=0 after closing channel, got %d", stats.ActiveChannels)
	}
}

// --- Auth Rate Limiting Tests ---

func TestSSHServer_BruteForceProtection(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "correct-password", portReg)

	// Make multiple failed auth attempts from the same client
	for range 5 {
		clientCfg := &gossh.ClientConfig{
			User: "clawbench",
			Auth: []gossh.AuthMethod{
				gossh.Password("wrong-password"),
			},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}
		gossh.Dial("tcp", srv.addr, clientCfg)
	}

	// After 5 failures, even the correct password should be rejected (IP is blocked)
	clientCfg := &gossh.ClientConfig{
		User: "clawbench",
		Auth: []gossh.AuthMethod{
			gossh.Password("correct-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth to be blocked after 5 failed attempts, but connection succeeded")
	}
}

func TestSSHServer_AuthFailureNoUsernameLeak(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	// Wrong username should not leak which usernames exist
	clientCfg := &gossh.ClientConfig{
		User: "root",
		Auth: []gossh.AuthMethod{
			gossh.Password("test-password"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	_, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err == nil {
		t.Error("expected auth failure for wrong username")
	}
	// Error message should NOT contain the username "root"
	if err != nil && strings.Contains(err.Error(), "root") {
		t.Errorf("error message should not leak username: %v", err)
	}
}

func TestSSHServer_SuccessfulAuthResetsCounter(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "correct-password", portReg)

	// Make 3 failed attempts (below the threshold of 5)
	for range 3 {
		clientCfg := &gossh.ClientConfig{
			User: "clawbench",
			Auth: []gossh.AuthMethod{
				gossh.Password("wrong-password"),
			},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}
		gossh.Dial("tcp", srv.addr, clientCfg)
	}

	// Successful login should reset the counter
	client := testSSHClient(t, srv.addr, "clawbench", "correct-password")
	client.Close()

	// Now 3 more failures should NOT trigger block (counter was reset)
	for range 3 {
		clientCfg := &gossh.ClientConfig{
			User: "clawbench",
			Auth: []gossh.AuthMethod{
				gossh.Password("wrong-password"),
			},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}
		gossh.Dial("tcp", srv.addr, clientCfg)
	}

	// Should still be able to connect with correct password
	client2 := testSSHClient(t, srv.addr, "clawbench", "correct-password")
	client2.Close()
}

// --- IPv6 / JoinHostPort Tests ---

func TestSSHServer_JoinHostPort_LocalhostTarget(t *testing.T) {
	// Verify that the SSH server correctly uses net.JoinHostPort for localhost targets.
	// This tests the fix from fmt.Sprintf("127.0.0.1:%d") → net.JoinHostPort.
	portReg := newTestRegistry(t)
	echoPort := startEchoServer(t)
	portReg.RegisterPort(echoPort, "", "echo", "http", "")

	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Connect using "localhost" as the target — should be normalized to 127.0.0.1
	conn, err := client.Dial("tcp", fmt.Sprintf("localhost:%d", echoPort))
	if err != nil {
		t.Fatalf("expected port forwarding via localhost to work, got: %v", err)
	}
	defer conn.Close()

	testMsg := []byte("hello via localhost")
	_, err = conn.Write(testMsg)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if !bytes.Equal(buf[:n], testMsg) {
		t.Errorf("echo mismatch: got %q, want %q", string(buf[:n]), string(testMsg))
	}
}

// --- Host Key Error Path Tests ---

func TestSSHServer_LoadHostKey_PermissivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: admin privileges bypass file permissions")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root bypasses filesystem permissions")
	}
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "ssh_host_key")

	// Generate a fresh key and save with permissive permissions
	keyBytes := marshalSignerKey(t)
	require.NoError(t, os.WriteFile(keyPath, keyBytes, 0o644)) // permissive

	// loadHostKey should fix permissions and succeed
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", nil)
	loadedSigner, err := srv.loadHostKey(keyPath)
	if err != nil {
		t.Fatalf("expected loadHostKey to succeed, got: %v", err)
	}
	if loadedSigner == nil {
		t.Error("expected non-nil signer")
	}

	// Verify permissions were fixed
	info, statErr := os.Stat(keyPath)
	if statErr == nil {
		perm := info.Mode().Perm()
		if perm&0o077 != 0 {
			t.Errorf("expected permissions to be fixed, got %04o", perm)
		}
	}
}

func TestSSHServer_LoadHostKey_ParseError(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "ssh_host_key")

	// Write invalid key data
	require.NoError(t, os.WriteFile(keyPath, []byte("not a valid key"), 0o600))

	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", nil)
	_, err := srv.loadHostKey(keyPath)
	if err == nil {
		t.Error("expected error for invalid key data")
	}
	if !strings.Contains(err.Error(), "failed to parse host key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSSHServer_GenerateAndSaveHostKey_WriteFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: admin privileges bypass file permissions")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root can write anywhere")
	}
	// Use a read-only directory to trigger write failure
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o555))
	defer func() { _ = os.Chmod(readOnlyDir, 0o755) }()

	keyPath := filepath.Join(readOnlyDir, "ssh_host_key")
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", nil)

	// Should fall back to ephemeral key when save fails
	signer, err := srv.generateAndSaveHostKey(keyPath)
	if err != nil {
		t.Fatalf("expected fallback to ephemeral key, got: %v", err)
	}
	if signer == nil {
		t.Error("expected non-nil signer from fallback")
	}
}

func TestSSHServer_UnknownChannelType(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Open a "session" channel (not direct-tcpip) — should be rejected
	channel, _, err := client.OpenChannel("session", nil)
	if err == nil {
		channel.Close()
		t.Error("expected session channel to be rejected")
	}
}

// --- Auth Tracker Comprehensive Tests ---

func TestAuthTracker_NewAuthTracker(t *testing.T) {
	tracker := newAuthTracker()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
		return
	}
	if len(tracker.records) != 0 {
		t.Errorf("expected empty records map, got %d entries", len(tracker.records))
	}
}

func TestAuthTracker_IsBlocked_Empty(t *testing.T) {
	tracker := newAuthTracker()
	if tracker.isBlocked("1.2.3.4") {
		t.Error("expected isBlocked=false for unknown IP")
	}
}

func TestAuthTracker_IsBlocked_ExpiredBlock(t *testing.T) {
	tracker := newAuthTracker()
	tracker.records["1.2.3.4"] = &ipRecord{
		failCount:    5,
		lastFail:     time.Now().Add(-10 * time.Minute),
		blockedUntil: time.Now().Add(-1 * time.Minute), // expired
	}
	if tracker.isBlocked("1.2.3.4") {
		t.Error("expected isBlocked=false for expired block")
	}
	rec := tracker.records["1.2.3.4"]
	if !rec.blockedUntil.IsZero() {
		t.Error("expected blockedUntil to be zeroed after block expiry")
	}
	if rec.failCount != 0 {
		t.Error("expected failCount to be reset after block expiry")
	}
}

func TestAuthTracker_IsBlocked_ActiveBlock(t *testing.T) {
	tracker := newAuthTracker()
	tracker.records["1.2.3.4"] = &ipRecord{
		failCount:    5,
		lastFail:     time.Now(),
		blockedUntil: time.Now().Add(5 * time.Minute), // still active
	}
	if !tracker.isBlocked("1.2.3.4") {
		t.Error("expected isBlocked=true for active block")
	}
}

func TestAuthTracker_IsBlocked_NotBlockedRecord(t *testing.T) {
	tracker := newAuthTracker()
	// Record exists but no block set
	tracker.records["1.2.3.4"] = &ipRecord{
		failCount:    2,
		lastFail:     time.Now(),
		blockedUntil: time.Time{},
	}
	if tracker.isBlocked("1.2.3.4") {
		t.Error("expected isBlocked=false for record with no block")
	}
}

func TestAuthTracker_RecordFailure_UnderThreshold(t *testing.T) {
	tracker := newAuthTracker()
	for i := range maxAuthFails - 1 {
		tracker.recordFailure("1.2.3.4")
		rec := tracker.records["1.2.3.4"]
		if rec.failCount != i+1 {
			t.Errorf("expected failCount=%d, got %d", i+1, rec.failCount)
		}
		if !rec.blockedUntil.IsZero() {
			t.Error("expected no block before reaching threshold")
		}
	}
}

func TestAuthTracker_RecordFailure_AtThreshold(t *testing.T) {
	tracker := newAuthTracker()
	for range maxAuthFails {
		tracker.recordFailure("1.2.3.4")
	}
	rec := tracker.records["1.2.3.4"]
	if rec.failCount != maxAuthFails {
		t.Errorf("expected failCount=%d, got %d", maxAuthFails, rec.failCount)
	}
	if rec.blockedUntil.IsZero() {
		t.Error("expected block to be set after reaching threshold")
	}
	// First block should be initialBlockDur (5 minutes)
	expectedDur := initialBlockDur
	actualDur := rec.blockedUntil.Sub(rec.lastFail)
	if actualDur != expectedDur {
		t.Errorf("expected block duration=%v, got %v", expectedDur, actualDur)
	}
}

func TestAuthTracker_RecordFailure_ExponentialBackoff(t *testing.T) {
	tracker := newAuthTracker()
	// First infraction: at failCount=5, block = initialBlockDur * 2^0 = 5min
	for range maxAuthFails {
		tracker.recordFailure("1.2.3.4")
	}
	rec := tracker.records["1.2.3.4"]
	dur1 := rec.blockedUntil.Sub(rec.lastFail)
	if dur1 != initialBlockDur {
		t.Errorf("first block: expected %v, got %v", initialBlockDur, dur1)
	}

	// Second infraction: at failCount=10, block = initialBlockDur * 2^1 = 10min
	for range maxAuthFails {
		tracker.recordFailure("1.2.3.4")
	}
	rec = tracker.records["1.2.3.4"]
	dur2 := rec.blockedUntil.Sub(rec.lastFail)
	expectedDur2 := initialBlockDur * 2
	if dur2 != expectedDur2 {
		t.Errorf("second block: expected %v, got %v", expectedDur2, dur2)
	}

	// Third infraction: at failCount=15, block = initialBlockDur * 2^2 = 20min
	for range maxAuthFails {
		tracker.recordFailure("1.2.3.4")
	}
	rec = tracker.records["1.2.3.4"]
	dur3 := rec.blockedUntil.Sub(rec.lastFail)
	expectedDur3 := initialBlockDur * 4
	if dur3 != expectedDur3 {
		t.Errorf("third block: expected %v, got %v", expectedDur3, dur3)
	}
}

func TestAuthTracker_RecordFailure_MaxCap(t *testing.T) {
	tracker := newAuthTracker()
	// Drive failCount high enough that exponential backoff exceeds maxBlockDur
	// At failCount=50, infractions=10, dur = initialBlockDur * 2^9 = 5min * 512 = 2560min > 1hr
	for range 50 {
		tracker.recordFailure("1.2.3.4")
	}
	rec := tracker.records["1.2.3.4"]
	dur := rec.blockedUntil.Sub(rec.lastFail)
	if dur != maxBlockDur {
		t.Errorf("expected block duration capped at %v, got %v", maxBlockDur, dur)
	}
}

func TestAuthTracker_Reset(t *testing.T) {
	tracker := newAuthTracker()
	tracker.recordFailure("1.2.3.4")
	tracker.recordFailure("1.2.3.4")

	if _, exists := tracker.records["1.2.3.4"]; !exists {
		t.Fatal("expected record to exist after failures")
	}

	tracker.reset("1.2.3.4")

	if _, exists := tracker.records["1.2.3.4"]; exists {
		t.Error("expected record to be deleted after reset")
	}
}

func TestAuthTracker_Reset_NonexistentIP(t *testing.T) {
	tracker := newAuthTracker()
	// Should not panic on nonexistent IP
	tracker.reset("9.9.9.9")
}

func TestAuthTracker_Cleanup_ExpiredRecords(t *testing.T) {
	tracker := newAuthTracker()
	tracker.records["1.1.1.1"] = &ipRecord{
		failCount:    1,
		lastFail:     time.Now().Add(-31 * time.Minute), // past recordTTL
		blockedUntil: time.Time{},
	}
	tracker.records["2.2.2.2"] = &ipRecord{
		failCount:    1,
		lastFail:     time.Now(),
		blockedUntil: time.Time{},
	}
	tracker.cleanup()
	if _, exists := tracker.records["1.1.1.1"]; exists {
		t.Error("expected expired record to be cleaned up")
	}
	if _, exists := tracker.records["2.2.2.2"]; !exists {
		t.Error("expected non-expired record to be preserved")
	}
}

func TestAuthTracker_Cleanup_BlockedRecord(t *testing.T) {
	tracker := newAuthTracker()
	tracker.records["3.3.3.3"] = &ipRecord{
		failCount:    5,
		lastFail:     time.Now().Add(-31 * time.Minute),
		blockedUntil: time.Now().Add(5 * time.Minute), // still blocked
	}
	tracker.cleanup()
	if _, exists := tracker.records["3.3.3.3"]; !exists {
		t.Error("expected still-blocked record to be preserved")
	}
}

func TestAuthTracker_Cleanup_ExpiredBlockOldRecord(t *testing.T) {
	tracker := newAuthTracker()
	// Block has expired AND lastFail is past TTL — should be removed
	tracker.records["4.4.4.4"] = &ipRecord{
		failCount:    5,
		lastFail:     time.Now().Add(-31 * time.Minute),
		blockedUntil: time.Now().Add(-1 * time.Minute), // expired block
	}
	tracker.cleanup()
	if _, exists := tracker.records["4.4.4.4"]; exists {
		t.Error("expected record with expired block and past-TTL lastFail to be cleaned up")
	}
}

func TestExtractIP_HostPort(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 22}
	result := extractIP(addr)
	if result != "192.168.1.1" {
		t.Errorf("expected '192.168.1.1', got %q", result)
	}
}

func TestExtractIP_IPv6HostPort(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 22}
	result := extractIP(addr)
	if result != "::1" {
		t.Errorf("expected '::1', got %q", result)
	}
}

func TestExtractIP_NoPort(t *testing.T) {
	addr := &net.IPAddr{IP: net.ParseIP("1.2.3.4")}
	result := extractIP(addr)
	// net.IPAddr.String() has no port, SplitHostPort fails → fallback to addr.String()
	if result != addr.String() {
		t.Errorf("expected fallback to addr.String(), got %q", result)
	}
}

// --- Server Utility Comprehensive Tests ---

func TestNewServer_PortZeroDefaultsToMainPortPlusOne(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 20000, "test", portReg)
	if srv.Port() != 20001 {
		t.Errorf("expected port 20001, got %d", srv.Port())
	}
}

func TestNewServer_ExplicitPort(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 22222}, 20000, "test", portReg)
	if srv.Port() != 22222 {
		t.Errorf("expected port 22222, got %d", srv.Port())
	}
}

func TestNewServer_AuthTrackerInitialized(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	if srv.authTracker == nil {
		t.Error("expected authTracker to be initialized")
	}
}

func TestNewServer_PasswordIsSHA256(t *testing.T) {
	portReg := newTestRegistry(t)
	sha256PW := "sha256:" + sha256Hex("mypassword")
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, sha256PW, portReg)
	if !srv.passwordIsSHA256 {
		t.Error("expected passwordIsSHA256=true for sha256:-prefixed password")
	}

	srv2 := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "plaintext", portReg)
	if srv2.passwordIsSHA256 {
		t.Error("expected passwordIsSHA256=false for plaintext password")
	}
}

func TestServer_Port(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 12345}, 0, "test", portReg)
	if srv.Port() != 12345 {
		t.Errorf("expected port 12345, got %d", srv.Port())
	}
}

func TestServer_ConnectionStats_NoConnections(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	stats := srv.ConnectionStats()
	if stats.Connected {
		t.Error("expected Connected=false")
	}
	if stats.ClientCount != 0 {
		t.Errorf("expected ClientCount=0, got %d", stats.ClientCount)
	}
	if stats.ActiveChannels != 0 {
		t.Errorf("expected ActiveChannels=0, got %d", stats.ActiveChannels)
	}
	if stats.LastConnectedAt != "" {
		t.Errorf("expected empty LastConnectedAt, got %q", stats.LastConnectedAt)
	}
}

func TestServer_ConnectionStats_WithConnections(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	// Simulate connection state
	now := time.Now()
	srv.mu.Lock()
	srv.connCount = 2
	srv.activeChannels = 3
	srv.lastConnected = now
	srv.mu.Unlock()

	stats := srv.ConnectionStats()
	if !stats.Connected {
		t.Error("expected Connected=true")
	}
	if stats.ClientCount != 2 {
		t.Errorf("expected ClientCount=2, got %d", stats.ClientCount)
	}
	if stats.ActiveChannels != 3 {
		t.Errorf("expected ActiveChannels=3, got %d", stats.ActiveChannels)
	}
	if stats.LastConnectedAt != now.Format(time.RFC3339) {
		t.Errorf("expected LastConnectedAt=%s, got %s", now.Format(time.RFC3339), stats.LastConnectedAt)
	}
}

func TestServer_ConnectionStats_ZeroTimeNoLastConnected(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	// Even with connections but zero lastConnected time
	srv.mu.Lock()
	srv.connCount = 1
	srv.lastConnected = time.Time{}
	srv.mu.Unlock()

	stats := srv.ConnectionStats()
	if stats.LastConnectedAt != "" {
		t.Errorf("expected empty LastConnectedAt for zero time, got %q", stats.LastConnectedAt)
	}
}

func TestServer_Fingerprint_BeforeInitHostKey(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	if srv.Fingerprint() != "" {
		t.Error("expected empty fingerprint before InitHostKey")
	}
}

func TestServer_Fingerprint_AfterInitHostKey(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)
	err := srv.InitHostKey()
	if err != nil {
		t.Fatalf("InitHostKey failed: %v", err)
	}
	fp := srv.Fingerprint()
	if fp == "" {
		t.Error("expected non-empty fingerprint after InitHostKey")
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("expected fingerprint to start with SHA256:, got %q", fp)
	}
}

func TestServer_InitHostKey(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	err := srv.InitHostKey()
	if err != nil {
		t.Fatalf("InitHostKey failed: %v", err)
	}
	if srv.hostKey == nil {
		t.Error("expected hostKey to be set after InitHostKey")
	}
	if srv.fingerprint == "" {
		t.Error("expected fingerprint to be set after InitHostKey")
	}
}

func TestServer_InitHostKey_Idempotent(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	err := srv.InitHostKey()
	if err != nil {
		t.Fatalf("first InitHostKey failed: %v", err)
	}
	_ = srv.Fingerprint()

	err = srv.InitHostKey()
	if err != nil {
		t.Fatalf("second InitHostKey failed: %v", err)
	}
	fp := srv.Fingerprint()

	// Note: InitHostKey always generates a new ephemeral key when no HostKey path is set,
	// so fingerprints may differ. This test just verifies it doesn't error.
	if fp == "" {
		t.Error("expected non-empty fingerprint after second InitHostKey")
	}
}

func TestServer_GenerateHostKey(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	signer, err := srv.generateHostKey()
	if err != nil {
		t.Fatalf("generateHostKey failed: %v", err)
	}
	if signer == nil {
		t.Error("expected non-nil signer")
	}
	// Verify the signer has a public key
	pubKey := signer.PublicKey()
	if pubKey == nil {
		t.Error("expected non-nil public key")
	}
}

func TestServer_GenerateHostKey_UniqueKeys(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 0, "test", portReg)

	signer1, _ := srv.generateHostKey()
	signer2, _ := srv.generateHostKey()

	fp1 := gossh.FingerprintSHA256(signer1.PublicKey())
	fp2 := gossh.FingerprintSHA256(signer2.PublicKey())

	if fp1 == fp2 {
		t.Error("expected different ephemeral keys on each generation")
	}
}

// marshalSignerKey generates a fresh ECDSA key and returns PEM-encoded bytes for saving to disk.
func marshalSignerKey(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	keyBytes, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

// --- Reverse port forwarding (ssh -R / tcpip-forward) tests ---

// reverseEchoRelay serves a client-side listener by relaying each accepted
// connection to a local echo server, emulating what a real client does when the
// server hands it a forwarded-tcpip channel. It returns a stop function.
func reverseEchoRelay(t *testing.T, ln net.Listener, echoPort int) {
	t.Helper()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				upstream, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(echoPort)))
				if err != nil {
					return
				}
				defer upstream.Close()
				go func() { _, _ = io.Copy(upstream, c) }()
				_, _ = io.Copy(c, upstream)
			}(c)
		}
	}()
}

// dialReversePort connects to the server-side bound port and verifies the echo
// round trip, proving the whole chain works: server listener → forwarded-tcpip
// channel → client relay → client's local target.
func assertReverseEcho(t *testing.T, serverPort int, payload string) {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(serverPort)), 500*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to reach server-side reverse port %d: %v", serverPort, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write to reverse port failed: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read from reverse port failed: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo mismatch: got %q, want %q", string(buf), payload)
	}
}

// waitPortFree polls until a port can be bound again, proving it was released.
func waitPortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
		if err == nil {
			_ = ln.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("port %d was never released", port)
}

func TestSSHReverseForward_EndToEnd(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	echoPort := startEchoServer(t)

	// Ask the server to bind an ephemeral loopback port and hand connections back.
	ln, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reverse forward request failed: %v", err)
	}
	defer ln.Close()

	serverPort := ln.Addr().(*net.TCPAddr).Port
	if serverPort <= 0 {
		t.Fatalf("expected a real allocated port, got %d", serverPort)
	}
	reverseEchoRelay(t, ln, echoPort)

	assertReverseEcho(t, serverPort, "hello reverse")
}

func TestSSHReverseForward_ExplicitPort(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	echoPort := startEchoServer(t)

	// Reserve a free port, then ask for exactly that one.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to probe free port: %v", err)
	}
	wantPort := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	ln, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", wantPort))
	if err != nil {
		t.Fatalf("reverse forward on explicit port failed: %v", err)
	}
	defer ln.Close()

	if got := ln.Addr().(*net.TCPAddr).Port; got != wantPort {
		t.Fatalf("requested port %d but got %d", wantPort, got)
	}
	reverseEchoRelay(t, ln, echoPort)
	assertReverseEcho(t, wantPort, "explicit port")
}

func TestSSHReverseForward_RejectsReservedSSHPort(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Binding the SSH port itself would take down the tunnel.
	_, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", srv.Port()))
	if err == nil {
		t.Fatal("expected the SSH port to be rejected for reverse forwarding")
	}
}

func TestSSHReverseForward_RejectsMainPort(t *testing.T) {
	// A stand-in for ClawBench's own HTTP port: hold it so nothing else can
	// take it, and tell the server it is reserved.
	mainListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to hold main port: %v", err)
	}
	defer mainListener.Close()
	mainPort := mainListener.Addr().(*net.TCPAddr).Port

	portReg := newTestRegistry(t)
	portReg.SetReservedPorts(mainPort)

	srv := NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, mainPort, "test-password", portReg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find ssh port: %v", err)
	}
	srv.addr = listener.Addr().String()
	listener.Close()
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })
	time.Sleep(100 * time.Millisecond)

	client := testSSHClient(t, srv.addr, "clawbench", "test-password")
	_, err = client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", mainPort))
	if err == nil {
		t.Fatal("expected ClawBench's own port to be rejected for reverse forwarding")
	}
}

func TestSSHReverseForward_RejectsDisallowedPort(t *testing.T) {
	portReg := newTestRegistry(t)
	portReg.SetAllowedPorts("3000-4000")
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	_, err := client.Listen("tcp", "127.0.0.1:9999")
	if err == nil {
		t.Fatal("expected a port outside allowed_ports to be rejected")
	}
}

func TestSSHReverseForward_CancelReleasesPort(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	ln, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reverse forward request failed: %v", err)
	}
	serverPort := ln.Addr().(*net.TCPAddr).Port

	// Closing the listener sends cancel-tcpip-forward.
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to cancel reverse forward: %v", err)
	}
	waitPortFree(t, serverPort)
}

func TestSSHReverseForward_ConnCloseReleasesPort(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	clientCfg := &gossh.ClientConfig{
		User:            "clawbench",
		Auth:            []gossh.AuthMethod{gossh.Password("test-password")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := gossh.Dial("tcp", srv.addr, clientCfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	ln, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reverse forward request failed: %v", err)
	}
	serverPort := ln.Addr().(*net.TCPAddr).Port

	// Dropping the whole connection (not just the listener) must release too.
	client.Close()
	waitPortFree(t, serverPort)
}

func TestSSHReverseForward_RegistryActiveLifecycle(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// Register the mapping first, so the server can flag it bound.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to probe free port: %v", err)
	}
	serverPort := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	allocated, err := portReg.RegisterPort(serverPort, "", "local svc", "http", model.DirectionReverse)
	if err != nil {
		t.Fatalf("failed to register reverse port: %v", err)
	}

	isActive := func() bool {
		for _, p := range portReg.ListPorts() {
			if p.LocalPort == allocated {
				return p.Active
			}
		}
		return false
	}
	if isActive() {
		t.Fatal("reverse mapping should start inactive")
	}

	ln, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort))
	if err != nil {
		t.Fatalf("reverse forward request failed: %v", err)
	}
	// The reply is synchronous, so the registry is updated by the time Listen returns.
	if !isActive() {
		t.Fatal("reverse mapping should be active once the client binds it")
	}

	_ = ln.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && isActive() {
		time.Sleep(20 * time.Millisecond)
	}
	if isActive() {
		t.Fatal("reverse mapping should be inactive after the client cancels it")
	}
}

func TestSSHReverseForward_DuplicatePortSecondClientRejected(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to probe free port: %v", err)
	}
	serverPort := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	client1 := testSSHClient(t, srv.addr, "clawbench", "test-password")
	ln, err := client1.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort))
	if err != nil {
		t.Fatalf("first reverse forward failed: %v", err)
	}
	defer ln.Close()

	client2 := testSSHClient(t, srv.addr, "clawbench", "test-password")
	if _, err := client2.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort)); err == nil {
		t.Fatal("expected the second client's request for the same port to be rejected")
	}
}

func TestSSHReverseForward_LocalForwardStillWorks(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	echoPort := startEchoServer(t)

	// Establish a reverse forward first...
	ln, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reverse forward request failed: %v", err)
	}
	defer ln.Close()
	reverseEchoRelay(t, ln, echoPort)

	// ...then confirm direct-tcpip (ssh -L) is unaffected on the same connection.
	conn, err := client.Dial("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(echoPort)))
	if err != nil {
		t.Fatalf("local forward broke after a reverse forward: %v", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("still works")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	buf := make([]byte, len("still works"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(buf) != "still works" {
		t.Fatalf("got %q", string(buf))
	}
}

func TestSSHReverseForward_UnknownGlobalRequestDoesNotHang(t *testing.T) {
	portReg := newTestRegistry(t)
	srv := testServerHelper(t, "test-password", portReg)
	client := testSSHClient(t, srv.addr, "clawbench", "test-password")

	// A global request we do not implement must be answered (or ignored) rather
	// than leaving the connection wedged. The old code discarded requests
	// wholesale; the new handler must not regress that for unknown types.
	ok, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		t.Fatalf("unknown global request errored: %v", err)
	}
	if ok {
		t.Fatal("expected an unimplemented global request to be rejected")
	}

	// The connection must still be usable afterwards.
	echoPort := startEchoServer(t)
	conn, err := client.Dial("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(echoPort)))
	if err != nil {
		t.Fatalf("connection unusable after an unknown global request: %v", err)
	}
	_ = conn.Close()
}
