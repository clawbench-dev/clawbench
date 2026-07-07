package ssh

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ipRecord tracks failed authentication attempts from a single IP.
type ipRecord struct {
	failCount    int
	lastFail     time.Time
	blockedUntil time.Time
}

// authTracker tracks failed SSH authentication attempts per IP address
// and temporarily blocks IPs with too many failures.
type authTracker struct {
	mu      sync.Mutex
	records map[string]*ipRecord // key: IP address
}

const (
	maxAuthFails    = 5 // Block after this many consecutive failures
	initialBlockDur = 5 * time.Minute
	maxBlockDur     = 1 * time.Hour
	cleanupInterval = 10 * time.Minute
	recordTTL       = 30 * time.Minute // Purge records idle this long
)

func newAuthTracker() *authTracker {
	return &authTracker{records: make(map[string]*ipRecord)}
}

// isBlocked returns true if the IP is currently blocked.
func (a *authTracker) isBlocked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.records[ip]
	if !ok {
		return false
	}
	if rec.blockedUntil.IsZero() || time.Now().Before(rec.blockedUntil) {
		return !rec.blockedUntil.IsZero()
	}
	// Block expired, clear it
	rec.blockedUntil = time.Time{}
	rec.failCount = 0
	return false
}

// recordFailure increments the failure counter for an IP.
// If the counter exceeds maxAuthFails, the IP is blocked with exponential backoff.
func (a *authTracker) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.records[ip]
	if !ok {
		rec = &ipRecord{}
		a.records[ip] = rec
	}
	rec.failCount++
	rec.lastFail = time.Now()
	if rec.failCount >= maxAuthFails {
		// Exponential backoff: initialBlockDur * 2^(infractions - 1), capped at maxBlockDur
		infractions := rec.failCount / maxAuthFails
		dur := initialBlockDur * time.Duration(1<<uint(infractions-1))
		if dur > maxBlockDur {
			dur = maxBlockDur
		}
		rec.blockedUntil = rec.lastFail.Add(dur)
	}
}

// reset clears the failure counter for an IP after successful authentication.
func (a *authTracker) reset(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.records, ip)
}

// cleanup removes expired records. Called periodically.
func (a *authTracker) cleanup() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for ip, rec := range a.records {
		// Remove records that are unblocked and idle past TTL
		if (rec.blockedUntil.IsZero() || now.After(rec.blockedUntil)) &&
			now.Sub(rec.lastFail) > recordTTL {
			delete(a.records, ip)
		}
	}
}

// extractIP extracts the IP address from a net.Addr.
func extractIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// Server is an SSH server that supports local port forwarding (direct-tcpip channels).
// It allows authenticated clients to create `-L` tunnels to forward local ports
// to any reachable host:port (localhost, LAN, or remote).
type Server struct {
	mu               sync.Mutex
	listener         net.Listener
	hostKey          gossh.Signer
	password         string
	passwordIsSHA256 bool
	portReg          *service.ProxyRegistry
	done             chan struct{}
	closeOnce        sync.Once
	fingerprint      string
	addr             string
	cfg              model.PortForwardConfig
	connCount        int
	activeChannels   int
	lastConnected    time.Time
	authTracker      *authTracker
}

// NewServer creates a new SSH tunnel server.
// When cfg.Port is 0 (unset), defaults to mainPort+1 so SSH runs on an
// adjacent port without requiring explicit configuration.
func NewServer(cfg model.PortForwardConfig, mainPort int, password string, portReg *service.ProxyRegistry) *Server {
	sshPort := cfg.Port
	if sshPort == 0 {
		sshPort = mainPort + 1
	}

	return &Server{
		password:         password,
		passwordIsSHA256: model.IsSHA256Password(password),
		portReg:          portReg,
		done:             make(chan struct{}),
		addr:             fmt.Sprintf("0.0.0.0:%d", sshPort),
		cfg:              cfg,
		authTracker:      newAuthTracker(),
	}
}

// ListenAndServe starts the SSH server.
func (s *Server) ListenAndServe() error {
	// Initialize host key (idempotent if already called)
	if err := s.InitHostKey(); err != nil {
		return err
	}

	// Configure SSH server
	config := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			remoteIP := extractIP(c.RemoteAddr())

			if s.authTracker.isBlocked(remoteIP) {
				return nil, fmt.Errorf("ssh: too many authentication failures")
			}

			if c.User() == "clawbench" {
				if s.passwordIsSHA256 {
					// Password is stored as SHA-256 hash — hash the submitted password and compare
					hash := sha256.Sum256([]byte(string(pass) + "clawbench-salt"))
					candidate := hex.EncodeToString(hash[:])
					if subtle.ConstantTimeCompare([]byte(candidate), []byte(s.password[len("sha256:"):])) == 1 {
						s.authTracker.reset(remoteIP)
						return nil, nil
					}
				} else if subtle.ConstantTimeCompare(pass, []byte(s.password)) == 1 {
					s.authTracker.reset(remoteIP)
					return nil, nil
				}
			}

			s.authTracker.recordFailure(remoteIP)
			return nil, fmt.Errorf("ssh: authentication failed")
		},
	}
	config.AddHostKey(s.hostKey)

	// Start TCP listener
	//nolint:noctx // SSH server uses net.Listener directly
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ssh: failed to listen on %s: %w", s.addr, err)
	}
	s.listener = listener

	// Periodically cleanup expired auth records
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.authTracker.cleanup()
			}
		}
	}()

	slog.Info(
		"SSH tunnel server started",
		slog.String("addr", s.addr),
		slog.String("fingerprint", s.fingerprint),
	)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil // graceful shutdown
			default:
				slog.Error("ssh: accept error", slog.String("err", err.Error()))
				continue
			}
		}

		go s.handleConn(conn, config)
	}
}

// Close shuts down the SSH server. Safe to call multiple times.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		slog.Info("SSH tunnel server stopped")
	})
}

// Fingerprint returns the SSH host key fingerprint.
// Returns empty string if the server has not been started yet.
func (s *Server) Fingerprint() string {
	return s.fingerprint
}

// InitHostKey generates or loads the host key and computes the fingerprint.
// This is called automatically by ListenAndServe, but can be called explicitly
// to populate Fingerprint() without starting the TCP listener.
func (s *Server) InitHostKey() error {
	signer, err := s.loadOrGenerateHostKey()
	if err != nil {
		return fmt.Errorf("ssh: failed to setup host key: %w", err)
	}
	s.hostKey = signer
	s.fingerprint = gossh.FingerprintSHA256(signer.PublicKey())
	return nil
}

// Port returns the SSH server port number.
func (s *Server) Port() int {
	_, portStr, _ := net.SplitHostPort(s.addr)
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return port
}

// SSHConnectionStats represents the current state of SSH client connections.
type SSHConnectionStats struct {
	Connected       bool   `json:"connected"`
	ClientCount     int    `json:"clientCount"`
	ActiveChannels  int    `json:"activeChannels"`
	LastConnectedAt string `json:"lastConnectedAt,omitempty"`
}

// ConnectionStats returns the current SSH connection statistics.
func (s *Server) ConnectionStats() SSHConnectionStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := SSHConnectionStats{
		Connected:      s.connCount > 0,
		ClientCount:    s.connCount,
		ActiveChannels: s.activeChannels,
	}
	if !s.lastConnected.IsZero() {
		stats.LastConnectedAt = s.lastConnected.Format(time.RFC3339)
	}
	return stats
}

// handleConn handles a single SSH connection.
func (s *Server) handleConn(conn net.Conn, config *gossh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	sshConn, chans, reqs, err := gossh.NewServerConn(conn, config)
	if err != nil {
		slog.Debug("ssh: handshake failed", slog.String("err", err.Error()))
		return
	}
	defer func() { _ = sshConn.Close() }()

	slog.Info(
		"ssh: client connected",
		slog.String("remote", sshConn.RemoteAddr().String()),
		slog.String("user", sshConn.User()),
	)

	s.mu.Lock()
	s.connCount++
	s.lastConnected = time.Now()
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.connCount--
		s.mu.Unlock()
		slog.Info("ssh: client disconnected", slog.String("remote", sshConn.RemoteAddr().String()))
	}()

	// Discard global requests (keep-alive, etc.)
	go gossh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "direct-tcpip" {
			slog.Debug("ssh: rejecting unknown channel type", slog.String("type", newChannel.ChannelType()))
			_ = newChannel.Reject(gossh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", newChannel.ChannelType()))
			continue
		}

		go s.handleDirectTCPIP(newChannel)
	}
}

// handleDirectTCPIP handles a direct-tcpip channel request (SSH -L port forwarding).
func (s *Server) handleDirectTCPIP(newChannel gossh.NewChannel) {
	// Parse the channel data per RFC 4254 Section 7.2
	type directTCPIPData struct {
		HostToConnect     string
		PortToConnect     uint32
		OriginatorAddress string
		OriginatorPort    uint32
	}

	var d directTCPIPData
	if err := gossh.Unmarshal(newChannel.ExtraData(), &d); err != nil {
		slog.Debug("ssh: failed to parse direct-tcpip data", slog.String("err", err.Error()))
		_ = newChannel.Reject(gossh.Prohibited, "failed to parse channel data")
		return
	}

	targetPort := int(d.PortToConnect)

	// Resolve the target host — normalize "localhost"/empty, resolve DNS hostnames
	targetHost := d.HostToConnect
	if targetHost == "" || targetHost == "localhost" {
		targetHost = "127.0.0.1"
	}

	// Validate the target port — only check allowed range.
	// SSH tunnels operate at the transport layer and don't need URL-rewriting
	// metadata, so IsPortRegistered (which tracks protocol info for the HTTP
	// reverse-proxy) is irrelevant here.
	if s.portReg == nil || !s.portReg.IsPortAllowed(targetPort) {
		slog.Debug("ssh: port not allowed", slog.Int("port", targetPort))
		_ = newChannel.Reject(gossh.Prohibited, fmt.Sprintf("port %d is not allowed", targetPort))
		return
	}

	// Accept the channel
	channel, requests, err := newChannel.Accept()
	if err != nil {
		slog.Debug("ssh: could not accept channel", slog.String("err", err.Error()))
		return
	}
	defer func() { _ = channel.Close() }()

	s.mu.Lock()
	s.activeChannels++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.activeChannels--
		s.mu.Unlock()
	}()

	// Discard channel-specific requests
	go gossh.DiscardRequests(requests)

	// Connect to the target host:port
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	//nolint:noctx // SSH tunnel dials target in goroutine per connection
	backend, err := net.Dial("tcp", targetAddr)
	if err != nil {
		slog.Debug("ssh: backend unreachable", slog.String("target", targetAddr), slog.String("err", err.Error()))
		return
	}
	defer func() { _ = backend.Close() }()

	slog.Debug(
		"ssh: forwarding connection",
		slog.String("target", targetAddr),
		slog.String("originator", net.JoinHostPort(d.OriginatorAddress, strconv.FormatUint(uint64(d.OriginatorPort), 10))),
	)

	// Bidirectional relay
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(channel, backend)
		// Signal the SSH channel that we're done writing
		if ch, ok := channel.(interface{ CloseWrite() error }); ok {
			_ = ch.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(backend, channel)
		if tcpConn, ok := backend.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
	}()

	wg.Wait()
}

// loadOrGenerateHostKey loads the host key from file or generates a new one.
func (s *Server) loadOrGenerateHostKey() (gossh.Signer, error) {
	if s.cfg.HostKey != "" {
		return s.loadHostKey(s.cfg.HostKey)
	}
	return s.generateHostKey()
}

// loadHostKey loads an existing host key from a file, or generates and saves one if it doesn't exist.
func (s *Server) loadHostKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		// Security: check that the host key file has restrictive permissions (ISS-021).
		// Private key files must not be readable by other users.
		if info, statErr := os.Stat(path); statErr == nil {
			perm := info.Mode().Perm()
			if perm&0o077 != 0 {
				slog.Warn(
					"ssh: host key file has overly permissive permissions, fixing",
					slog.String("path", path),
					slog.String("mode", fmt.Sprintf("%04o", perm)),
				)
				if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
					slog.Warn("ssh: could not fix host key file permissions", slog.String("err", chmodErr.Error()))
				}
			}
		}

		signer, parseErr := gossh.ParsePrivateKey(data)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse host key from %s: %w", path, parseErr)
		}
		slog.Info("ssh: loaded host key from file", slog.String("path", path))
		return signer, nil
	}

	// File doesn't exist, generate and save
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read host key file %s: %w", path, err)
	}

	return s.generateAndSaveHostKey(path)
}

// generateAndSaveHostKey generates a new ECDSA host key and saves it to the specified path.
func (s *Server) generateAndSaveHostKey(path string) (gossh.Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ECDSA key: %w", err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	if writeErr := os.WriteFile(path, pemData, 0o600); writeErr != nil {
		// Can't save, fall back to ephemeral key
		slog.Warn("ssh: could not save host key, using ephemeral key", slog.String("path", path), slog.String("err", writeErr.Error()))
		return s.generateHostKey()
	}

	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer from generated key: %w", err)
	}

	slog.Info("ssh: generated and saved new host key", slog.String("path", path))
	return signer, nil
}

// generateHostKey generates an ephemeral ECDSA host key (not persisted).
func (s *Server) generateHostKey() (gossh.Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer from generated key: %w", err)
	}

	slog.Info("ssh: using ephemeral host key (will change on restart)")
	return signer, nil
}
