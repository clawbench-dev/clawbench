//nolint:noctx,goconst,rowserrcheck // db global and platform exec.Command calls, context not applicable; protocol/role strings are domain constants; legacy db.Query pattern
package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/proxy"
)

// ProxyRegistry manages forwarded ports: registration, health checks, and auto-detection.
type ProxyRegistry struct {
	mu       sync.RWMutex
	ports    map[int]*model.ForwardedPort // key = localPort (auto-assigned, unique)
	proxies  map[int]*proxy.ReverseProxy  // key = localPort, active HTTP reverse proxies for non-localhost targets
	cfg      model.ProxyConfig
	selfPort int // ClawBench's own port, excluded from detection
	cancel   context.CancelFunc
}

// allocateLocalPort finds an available local port for forwarding.
// Prefers the requested port; if taken, scans upward until a free one is found.
// Privileged ports (< 1024) are never returned directly — they are mapped to
// high ports (1024+) since Android/non-root cannot bind privileged ports.
func (r *ProxyRegistry) allocateLocalPort(preferred int) int {
	// Privileged ports must be remapped to non-privileged range
	if preferred < 1024 {
		// Start scanning from 1024 for a free port
		for p := 1024; p <= 65535; p++ {
			if _, taken := r.ports[p]; !taken {
				return p
			}
		}
		return 1024 // fallback
	}
	if _, taken := r.ports[preferred]; !taken {
		return preferred
	}
	// Scan upward from preferred+1 to find a free local port
	for p := preferred + 1; p <= 65535; p++ {
		if _, taken := r.ports[p]; !taken {
			return p
		}
	}
	// Should never happen in practice
	return preferred
}

// hostDisplayName returns a human-readable host name for error messages.
func hostDisplayName(host string) string {
	if host == "" {
		return "localhost"
	}
	return host
}

// ProxyService is the global singleton, initialized from main.go.
var ProxyService *ProxyRegistry

// NewProxyRegistry creates a new port registry and starts background health checks.
// It also restores any previously persisted ports from the database.
// The caller (main.go) decides whether to create this based on port_forward.enabled.
func NewProxyRegistry(selfPort int) *ProxyRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	allowedPorts := "1024-65535" // default: non-privileged ports only (ISS-186 — was empty=allow-all)
	r := &ProxyRegistry{
		ports:    make(map[int]*model.ForwardedPort),
		proxies:  make(map[int]*proxy.ReverseProxy),
		cfg:      model.ProxyConfig{AllowedPorts: allowedPorts},
		selfPort: selfPort,
		cancel:   cancel,
	}

	// Restore persisted ports from database
	r.loadPortsFromDB()

	slog.Info(
		"proxy service initialized",
		slog.String("allowed_ports", r.cfg.AllowedPorts),
		slog.Int("self_port", selfPort),
		slog.Int("restored_ports", len(r.ports)),
	)

	// Start background health checker
	go r.healthCheckLoop(ctx)

	return r
}

// SetAllowedPorts overrides the allowed port range and removes any already-loaded
// ports that fall outside the new range.
func (r *ProxyRegistry) SetAllowedPorts(allowedPorts string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.AllowedPorts = allowedPorts

	// Remove ports that are no longer allowed
	for lp, p := range r.ports {
		if !isPortInRange(p.Port, r.cfg.AllowedPorts) {
			slog.Info("proxy port removed (outside allowed range)", slog.Int("local_port", lp), slog.Int("port", p.Port))
			r.stopReverseProxy(lp)
			delete(r.ports, lp)
			r.deletePortFromDB(lp)
		}
	}
}

// Stop shuts down the proxy registry and all health check goroutines.
func (r *ProxyRegistry) Stop() {
	// Stop all reverse proxies
	r.mu.Lock()
	for lp, rp := range r.proxies {
		rp.Close()
		delete(r.proxies, lp)
	}
	r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}
	slog.Info("proxy service stopped")
}

// RegisterPort adds a port to the forwarding registry and returns the allocated local port.
// port is the target port on the remote host. If the same port is already
// registered (with a different host), a nearby free local port is auto-assigned.
func (r *ProxyRegistry) RegisterPort(port int, host string, name string, protocol string) (int, error) {
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port number: %d", port)
	}
	if !r.IsPortAllowed(port) {
		return 0, fmt.Errorf("port %d is not in the allowed range", port)
	}
	if protocol != "https" {
		protocol = "http"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if this exact (port, host) is already registered
	for _, p := range r.ports {
		if p.Port == port && p.Host == host {
			return 0, fmt.Errorf("port %d → %s is already registered (local %d)", port, hostDisplayName(host), p.LocalPort)
		}
	}

	// Auto-allocate a local port (prefers the target port if available)
	localPort := r.allocateLocalPort(port)

	r.ports[localPort] = &model.ForwardedPort{
		Port:       port,
		LocalPort:  localPort,
		Host:       host,
		Name:       name,
		Protocol:   protocol,
		AutoDetect: false,
		Active:     checkPortActive(port, host),
	}

	// For non-localhost targets, start an HTTP reverse proxy to rewrite the Host header.
	// SSH tunnel is TCP-level and cannot modify HTTP headers; the reverse proxy
	// ensures the backend receives the correct Host header (targetHost:targetPort).
	if isNonLocalhostTarget(host) {
		if err := r.startReverseProxy(localPort, port, host, protocol); err != nil {
			// Log but don't fail — the port is still registered for SSH tunneling
			slog.Warn(
				"failed to start reverse proxy for non-localhost target",
				slog.Int("local_port", localPort),
				slog.Int("target_port", port),
				slog.String("host", host),
				slog.String("err", err.Error()),
			)
		}
	}

	// Persist to database
	r.savePortToDB(localPort, port, host, name, protocol)

	slog.Info(
		"proxy port registered",
		slog.Int("local_port", localPort),
		slog.Int("target_port", port),
		slog.String("host", host),
		slog.String("name", name),
		slog.String("protocol", protocol),
	)

	return localPort, nil
}

// UpdatePort modifies an existing forwarded port's host, name, and protocol.
// localPort is the immutable key; the target (port, host) must remain unique among other entries.
func (r *ProxyRegistry) UpdatePort(localPort int, port int, host string, name string, protocol string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port number: %d", port)
	}
	if !r.IsPortAllowed(port) {
		return fmt.Errorf("port %d is not in the allowed range", port)
	}
	if protocol != "https" {
		protocol = "http"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.ports[localPort]
	if !ok {
		return fmt.Errorf("local port %d is not registered", localPort)
	}

	// Check (port, host) uniqueness — skip self
	for lp, p := range r.ports {
		if lp == localPort {
			continue
		}
		if p.Port == port && p.Host == host {
			return fmt.Errorf("port %d → %s is already registered (local %d)", port, hostDisplayName(host), lp)
		}
	}

	// If target port changed, may need to re-allocate local port
	newLocalPort := r.reallocateLocalPort(localPort, port, existing.Port)

	// Determine if reverse proxy needs to be restarted:
	// host, port, or protocol changed for a non-localhost target,
	// or the target switched between localhost and non-localhost.
	needProxyRestart := r.needsProxyRestart(existing, host, port, protocol)

	if needProxyRestart {
		r.stopReverseProxy(localPort)
	}

	// If localPort changed, move the entry in the map
	if newLocalPort != localPort {
		delete(r.ports, localPort)
		r.deletePortFromDB(localPort)
	}

	r.ports[newLocalPort] = &model.ForwardedPort{
		Port:       port,
		LocalPort:  newLocalPort,
		Host:       host,
		Name:       name,
		Protocol:   protocol,
		AutoDetect: existing.AutoDetect,
		Active:     checkPortActive(port, host),
	}

	// Start reverse proxy if needed for the new target
	if needProxyRestart && isNonLocalhostTarget(host) {
		if err := r.startReverseProxy(newLocalPort, port, host, protocol); err != nil {
			slog.Warn(
				"failed to restart reverse proxy after UpdatePort",
				slog.Int("local_port", newLocalPort),
				slog.Int("target_port", port),
				slog.String("host", host),
				slog.String("err", err.Error()),
			)
		}
	}

	r.savePortToDB(newLocalPort, port, host, name, protocol)

	slog.Info(
		"proxy port updated",
		slog.Int("local_port", newLocalPort),
		slog.Int("target_port", port),
		slog.String("host", host),
		slog.String("name", name),
		slog.String("protocol", protocol),
	)

	return nil
}

// reallocateLocalPort determines the new local port if the target port changed.
// If the old local port matched the old target port and the new target port is available,
// reassign to the new target port; otherwise keep the current local port.
func (r *ProxyRegistry) reallocateLocalPort(localPort, newTargetPort, oldTargetPort int) int {
	if newTargetPort == oldTargetPort {
		return localPort
	}
	if localPort == oldTargetPort {
		if _, taken := r.ports[newTargetPort]; !taken {
			return newTargetPort
		}
	}
	return localPort
}

// needsProxyRestart returns true if the reverse proxy must be restarted after a port update.
// This happens when host/port/protocol changed for a non-localhost target,
// or when the target switches between localhost and non-localhost.
func (r *ProxyRegistry) needsProxyRestart(existing *model.ForwardedPort, host string, port int, protocol string) bool {
	hostChanged := host != existing.Host
	portChanged := port != existing.Port
	protocolChanged := protocol != existing.Protocol
	wasNonLocalhost := isNonLocalhostTarget(existing.Host)
	isNonLocalhost := isNonLocalhostTarget(host)
	return (hostChanged || portChanged || protocolChanged) && (wasNonLocalhost || isNonLocalhost)
}

// UnregisterPort removes a port from the forwarding registry by local port.
func (r *ProxyRegistry) UnregisterPort(localPort int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.ports[localPort]; !exists {
		return fmt.Errorf("local port %d is not registered", localPort)
	}

	delete(r.ports, localPort)

	// Stop any reverse proxy running for this port
	r.stopReverseProxy(localPort)

	// Remove from database
	r.deletePortFromDB(localPort)

	slog.Info("proxy port unregistered", slog.Int("local_port", localPort))
	return nil
}

// ListPorts returns all registered ports with current health status.
func (r *ProxyRegistry) ListPorts() []model.ForwardedPort {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]model.ForwardedPort, 0, len(r.ports))
	for lp, p := range r.ports {
		fp := *p
		_, fp.HasReverseProxy = r.proxies[lp]
		result = append(result, fp)
	}

	// Sort by local port number for stable output
	sort.Slice(result, func(i, j int) bool {
		return result[i].LocalPort < result[j].LocalPort
	})

	return result
}

// IsPortAllowed checks whether a port falls within the configured allowed range.
// RLock ensures concurrent reads of cfg.AllowedPorts are synchronized with SetAllowedPorts writes (ISS-185).
func (r *ProxyRegistry) IsPortAllowed(port int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return isPortInRange(port, r.cfg.AllowedPorts)
}

// DetectedPort represents an auto-detected listening port with its protocol and process.
type DetectedPort struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`    // "http", "https", or "other" (non-HTTP like SSH)
	ProcessName string `json:"processName"` // Name of the process listening on this port
	ProcessArgs string `json:"processArgs"` // Partial command-line arguments
}

// detectedPortInfo is an internal type for platform-specific scan results.
type detectedPortInfo struct {
	Port        int
	ProcessName string
	ProcessArgs string
}

// DetectListeningPorts returns a list of TCP ports currently in LISTEN state
// on the server, filtered to exclude system ports and ClawBench's own port.
// Each port is probed to determine if it speaks TLS (https).
func (r *ProxyRegistry) DetectListeningPorts() []DetectedPort {
	var portInfos []detectedPortInfo

	switch runtime.GOOS {
	case "linux":
		portInfos = parseProcNetTCP()
	case "darwin":
		portInfos = parseLsof()
	case "windows":
		portInfos = parseNetstat()
	default:
		slog.Warn("port auto-detection not supported on this OS", slog.String("os", runtime.GOOS))
		return nil
	}

	// Filter: exclude our own port (allow all other ports including privileged ones)
	filtered := make([]detectedPortInfo, 0, len(portInfos))
	for _, p := range portInfos {
		if p.Port > 0 && p.Port != r.selfPort {
			filtered = append(filtered, p)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Port < filtered[j].Port
	})

	// Classify each port
	result := make([]DetectedPort, len(filtered))
	for i, p := range filtered {
		protocol := classifyPort(p.Port, p.ProcessName)
		result[i] = DetectedPort{Port: p.Port, Protocol: protocol, ProcessName: p.ProcessName, ProcessArgs: p.ProcessArgs}
	}
	return result
}

// classifyPort determines the protocol of a listening port.
// Known non-HTTP ports (SSH, MySQL, Redis, etc.) are marked as "other"
// since they cannot be forwarded through an HTTP proxy/browser.
func classifyPort(port int, processName string) string { //nolint:gocyclo // multi-condition port classification
	// Well-known non-HTTP ports
	switch port {
	case 22, 2222: // SSH
		return "other"
	case 25, 465, 587: // SMTP
		return "other"
	case 3306: // MySQL
		return "other"
	case 5432: // PostgreSQL
		return "other"
	case 6379: // Redis
		return "other"
	case 27017: // MongoDB
		return "other"
	case 21: // FTP
		return "other"
	}

	// Check process name for known non-HTTP services
	name := strings.ToLower(processName)
	if strings.Contains(name, "ssh") || strings.Contains(name, "sshd") {
		return "other"
	}
	if strings.Contains(name, "mysql") || strings.Contains(name, "mysqld") {
		return "other"
	}
	if strings.Contains(name, "postgres") {
		return "other"
	}
	if strings.Contains(name, "redis") {
		return "other"
	}
	if strings.Contains(name, "mongod") {
		return "other"
	}

	// Probe for TLS
	if detectTLS(port) {
		return "https"
	}
	return "http"
}

// detectTLS attempts a TLS handshake to determine if a port speaks HTTPS.
func detectTLS(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()

	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	err = tlsConn.Handshake()
	return err == nil
}

// healthCheckLoop periodically checks if registered ports are still listening.
func (r *ProxyRegistry) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Run an immediate check
	r.checkAllPorts()

	for {
		select {
		case <-ticker.C:
			r.checkAllPorts()
		case <-ctx.Done():
			return
		}
	}
}

// checkAllPorts dials each registered port to determine if it's active.
func (r *ProxyRegistry) checkAllPorts() {
	r.mu.RLock()
	portList := make([]struct {
		localPort int
		port      int
		host      string
	}, 0, len(r.ports))
	for lp, p := range r.ports {
		portList = append(portList, struct {
			localPort int
			port      int
			host      string
		}{localPort: lp, port: p.Port, host: p.Host})
	}
	r.mu.RUnlock()

	for _, entry := range portList {
		active := checkPortActive(entry.port, entry.host)

		r.mu.Lock()
		if p, ok := r.ports[entry.localPort]; ok {
			p.Active = active
		}
		r.mu.Unlock()
	}
}

// checkPortActive attempts a TCP connection to determine if a port is listening.
// Uses a longer timeout for non-localhost targets (LAN/remote hosts may have higher latency).
func checkPortActive(port int, host string) bool {
	targetHost := host
	if targetHost == "" {
		targetHost = "127.0.0.1"
	}
	timeout := 2 * time.Second
	if targetHost != "127.0.0.1" && targetHost != "localhost" {
		timeout = 5 * time.Second
	}
	addr := net.JoinHostPort(targetHost, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// parseProcNetTCP reads /proc/net/tcp on Linux to find LISTEN sockets
// and resolves the process name for each port.
func parseProcNetTCP() []detectedPortInfo {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		slog.Debug("failed to read /proc/net/tcp", slog.String("err", err.Error()))
		return nil
	}

	// Parse /proc/net/tcp to get port -> inode mapping
	portInodes := parseProcNetTCPData(string(data))

	// Build inode -> process name mapping by scanning /proc/PID/fd/
	inodeProcess := resolveInodeToProcess()

	result := make([]detectedPortInfo, 0, len(portInodes))
	for port, inode := range portInodes {
		info := procInfo{}
		if inode > 0 {
			if p, ok := inodeProcess[inode]; ok {
				info = p
			}
		}
		result = append(result, detectedPortInfo{Port: port, ProcessName: info.Name, ProcessArgs: info.Args})
	}

	return result
}

// parseProcNetTCPData parses the content of /proc/net/tcp.
// Returns a mapping of port number to socket inode for LISTEN sockets.
func parseProcNetTCPData(data string) map[int]uint64 {
	portInodes := make(map[int]uint64)
	scanner := bufio.NewScanner(strings.NewReader(data))

	// Skip header line
	if !scanner.Scan() {
		return portInodes
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// local_address:port is in field[1], e.g. "00000000:1F90"
		localAddr := fields[1]
		colonIdx := strings.LastIndex(localAddr, ":")
		if colonIdx < 0 {
			continue
		}
		portHex := localAddr[colonIdx+1:]

		// state is in field[3], "0A" = LISTEN
		state := fields[3]
		if state != "0A" {
			continue
		}

		port, err := strconv.ParseInt(portHex, 16, 32)
		if err != nil {
			continue
		}

		// inode is in field[9]
		var inode uint64
		if len(fields) > 9 {
			inode, _ = strconv.ParseUint(fields[9], 10, 64)
		}

		portInodes[int(port)] = inode
	}

	return portInodes
}

// procInfo holds resolved process information for a PID.
type procInfo struct {
	Name string
	Args string
}

// resolveInodeToProcess scans /proc/PID/fd/ to build a mapping from
// socket inode numbers to process info.
func resolveInodeToProcess() map[uint64]procInfo { //nolint:gocognit,gocyclo // multi-source inode resolution
	inodeMap := make(map[uint64]procInfo)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return inodeMap
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		// Check if directory name is a number (PID)
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}

		// Read process info from /proc/PID/cmdline
		// cmdline uses null bytes as separators; fields: exe_path arg1 arg2 ...
		cmdlinePath := "/proc/" + pidStr + "/cmdline"
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// Split by null bytes
		fields := bytes.Split(cmdlineData, []byte{0})
		if len(fields) == 0 || len(fields[0]) == 0 {
			continue
		}

		// First field is the executable path — extract basename
		procName := filepath.Base(string(fields[0]))

		// Remaining fields are arguments — join with space, truncate to 120 chars
		var argsStr string
		if len(fields) > 1 {
			argParts := make([]string, 0, len(fields)-1)
			for _, f := range fields[1:] {
				if len(f) > 0 {
					argParts = append(argParts, string(f))
				}
			}
			argsStr = strings.Join(argParts, " ")
			// Truncate long args
			if len(argsStr) > 120 {
				argsStr = argsStr[:120] + "…"
			}
		}

		info := procInfo{Name: procName, Args: argsStr}

		// Scan /proc/PID/fd/ for socket inodes
		fdDir := "/proc/" + pidStr + "/fd"
		fdEntries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		for _, fd := range fdEntries {
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			// Links look like: socket:[12345]
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inodeStr := strings.TrimPrefix(link, "socket:[")
			inodeStr = strings.TrimSuffix(inodeStr, "]")
			inode, err := strconv.ParseUint(inodeStr, 10, 64)
			if err != nil {
				continue
			}
			// Only store first process per inode (avoid overwriting with less relevant one)
			if _, exists := inodeMap[inode]; !exists {
				inodeMap[inode] = info
			}
		}
	}

	return inodeMap
}

// parseLsof uses lsof on macOS to find LISTEN sockets and their process names.
func parseLsof() []detectedPortInfo {
	cmd := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-P", "-n")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("lsof command failed", slog.String("err", err.Error()))
		return nil
	}

	// Map port -> process name (first process wins for dedup)
	portProcess := make(map[int]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Lines look like: node  12345 user  23u  IPv4 ...  TCP *:5173 (LISTEN)
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// COMMAND is field[0]
		procName := fields[0]

		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			rest := line[idx+1:]
			// Extract port number (stop at space or paren)
			var portStr string
			for _, ch := range rest {
				if ch >= '0' && ch <= '9' {
					portStr += string(ch)
				} else {
					break
				}
			}
			if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
				if _, exists := portProcess[port]; !exists {
					portProcess[port] = procName
				}
			}
		}
	}

	result := make([]detectedPortInfo, 0, len(portProcess))
	for port, procName := range portProcess {
		result = append(result, detectedPortInfo{Port: port, ProcessName: procName})
	}

	return result
}

// parseNetstat uses netstat on Windows to find LISTEN sockets and their process names.
func parseNetstat() []detectedPortInfo {
	cmd := exec.Command("netstat", "-ano")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("netstat command failed", slog.String("err", err.Error()))
		return nil
	}

	// Map port -> PID
	portPID := make(map[int]int)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "LISTENING") {
			continue
		}
		// Lines look like:  TCP    0.0.0.0:5173          0.0.0.0:0              LISTENING       12345
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		localAddr := fields[1]
		colonIdx := strings.LastIndex(localAddr, ":")
		if colonIdx < 0 {
			continue
		}
		port, err := strconv.Atoi(localAddr[colonIdx+1:])
		if err != nil || port <= 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		if _, exists := portPID[port]; !exists {
			portPID[port] = pid
		}
	}

	// Resolve PID -> process name via tasklist
	pidProcess := resolveWindowsPIDs(portPID)

	result := make([]detectedPortInfo, 0, len(portPID))
	for port, pid := range portPID {
		procName := pidProcess[pid]
		result = append(result, detectedPortInfo{Port: port, ProcessName: procName})
	}

	return result
}

// resolveWindowsPIDs uses tasklist to map PIDs to process names.
func resolveWindowsPIDs(portPID map[int]int) map[int]string {
	pidProcess := make(map[int]string)
	if len(portPID) == 0 {
		return pidProcess
	}

	// Collect unique PIDs
	pids := make(map[int]bool)
	for _, pid := range portPID {
		pids[pid] = true
	}

	// Run tasklist to get process names
	cmd := exec.Command("tasklist", "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("tasklist command failed", slog.String("err", err.Error()))
		return pidProcess
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// CSV format: "name.exe","12345","Console","1","12,345 K"
		// Simple CSV parse
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.Trim(parts[0], "\"")
		pidStr := strings.Trim(parts[1], "\"")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if pids[pid] {
			pidProcess[pid] = name
		}
	}

	return pidProcess
}

// isPortInRange checks if a port number falls within the allowed range string.
// Supported formats: "1024-65535", "3000,5173,8080", "1024-5000,8080"
func isPortInRange(port int, rangeStr string) bool {
	if rangeStr == "" {
		return true // empty = allow all
	}

	parts := strings.Split(rangeStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				continue
			}
			low, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			high, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			if port >= low && port <= high {
				return true
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil {
				continue
			}
			if port == p {
				return true
			}
		}
	}

	return false
}

// loadPortsFromDB restores previously persisted forwarded ports from the database.
// Called once during ProxyRegistry initialization.
func (r *ProxyRegistry) loadPortsFromDB() {
	if db == nil {
		return
	}

	rows, err := dbRead.Query("SELECT local_port, port, host, name, protocol FROM forwarded_ports")
	if err != nil {
		slog.Warn("failed to load persisted ports from DB", slog.String("err", err.Error()))
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var localPort, port int
		var host, name, protocol string
		if err := rows.Scan(&localPort, &port, &host, &name, &protocol); err != nil {
			continue
		}
		if !r.IsPortAllowed(port) {
			slog.Warn("skipping persisted port outside allowed range", slog.Int("port", port))
			continue
		}
		if protocol != "https" {
			protocol = "http"
		}
		r.ports[localPort] = &model.ForwardedPort{
			Port:       port,
			LocalPort:  localPort,
			Host:       host,
			Name:       name,
			Protocol:   protocol,
			AutoDetect: false,
			Active:     false, // will be updated by health check
		}
		// For non-localhost targets, start an HTTP reverse proxy to rewrite Host header.
		if isNonLocalhostTarget(host) {
			if err := r.startReverseProxy(localPort, port, host, protocol); err != nil {
				slog.Warn(
					"failed to start reverse proxy on DB load",
					slog.Int("local_port", localPort),
					slog.Int("target_port", port),
					slog.String("host", host),
					slog.String("err", err.Error()),
				)
			}
		}
	}

	if len(r.ports) > 0 {
		slog.Info("restored forwarded ports from database", slog.Int("count", len(r.ports)))
	}
}

// savePortToDB persists a single forwarded port to the database.
func (r *ProxyRegistry) savePortToDB(localPort int, port int, host string, name, protocol string) {
	if db == nil {
		return
	}
	_, err := WriteExec(
		"INSERT OR REPLACE INTO forwarded_ports (local_port, port, host, name, protocol) VALUES (?, ?, ?, ?, ?)",
		localPort, port, host, name, protocol,
	)
	if err != nil {
		slog.Error("failed to persist port to DB", slog.Int("local_port", localPort), slog.String("err", err.Error()))
	}
}

// deletePortFromDB removes a forwarded port from the database.
func (r *ProxyRegistry) deletePortFromDB(localPort int) {
	if db == nil {
		return
	}
	_, err := WriteExec("DELETE FROM forwarded_ports WHERE local_port = ?", localPort)
	if err != nil {
		slog.Error("failed to delete port from DB", slog.Int("local_port", localPort), slog.String("err", err.Error()))
	}
}

// isNonLocalhostTarget returns true if the target host is not localhost/127.0.0.1.
// Non-localhost targets need an HTTP reverse proxy to rewrite the Host header.
func isNonLocalhostTarget(host string) bool {
	return host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1"
}

// startReverseProxy creates and starts an HTTP reverse proxy for a non-localhost target.
// The proxy listens on 127.0.0.1:{localPort} and forwards to {host}:{port},
// rewriting the Host header to match the target.
func (r *ProxyRegistry) startReverseProxy(localPort int, targetPort int, host string, protocol string) error {
	targetAddr := net.JoinHostPort(host, strconv.Itoa(targetPort))
	rp, err := proxy.NewReverseProxy("127.0.0.1", localPort, targetAddr, protocol)
	if err != nil {
		return fmt.Errorf("failed to create reverse proxy: %w", err)
	}

	go rp.Serve()
	r.proxies[localPort] = rp

	slog.Info(
		"reverse proxy started for non-localhost target",
		slog.Int("local_port", localPort),
		slog.String("target", targetAddr),
		slog.String("protocol", protocol),
	)

	return nil
}

// stopReverseProxy stops and removes the reverse proxy for the given local port.
func (r *ProxyRegistry) stopReverseProxy(localPort int) {
	if rp, ok := r.proxies[localPort]; ok {
		rp.Close()
		delete(r.proxies, localPort)
		slog.Info("reverse proxy stopped", slog.Int("local_port", localPort))
	}
}
