package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"clawbench/internal/platform"
	"clawbench/internal/version"
	"clawbench/internal/ws"
)

// upgradeShutdownFunc is called to gracefully shut down the server during upgrade.
var upgradeShutdownFunc func()

// SetUpgradeShutdownFunc sets the function called to gracefully shut down during upgrade.
func SetUpgradeShutdownFunc(f func()) {
	upgradeShutdownFunc = f
}

// upgradeIsSupervised reports whether the process is running under a supervisor.
var upgradeIsSupervised func() bool

// SetUpgradeIsSupervised sets the function that reports supervisor status.
func SetUpgradeIsSupervised(f func() bool) {
	upgradeIsSupervised = f
}

// upgradeCancel is the cancellation function for the current upgrade goroutine.
var upgradeCancel context.CancelFunc

// npmPlatformPkg maps runtime.GOOS/runtime.GOARCH to the npm platform package name.
var npmPlatformPkg = map[string]string{
	"linux/amd64":   "@xulongzhe/clawbench-linux-x64",
	"linux/arm64":   "@xulongzhe/clawbench-linux-arm64",
	"darwin/amd64":  "@xulongzhe/clawbench-darwin-x64",
	"darwin/arm64":  "@xulongzhe/clawbench-darwin-arm64",
	"windows/amd64": "@xulongzhe/clawbench-win32-x64",
}

// npmRegistryResponse represents the relevant fields from the npm registry API.
type npmRegistryResponse struct {
	Version string `json:"version"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
		Shasum    string `json:"shasum"`
	} `json:"dist"`
}

// UpgradeInfo holds the result of a single registry query.
type UpgradeInfo struct {
	CurrentVersion string
	LatestVersion  string
	TarballURL     string
	Integrity      string // e.g. "sha512-abcdef..."
	HasUpgrade     bool
}

// getPlatformPkg returns the npm platform package name for the current OS/arch.
func getPlatformPkg() (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	pkg, ok := npmPlatformPkg[key]
	if !ok {
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return pkg, nil
}

// getRegistryBase returns the npm registry base URL based on China detection.
func getRegistryBase() string {
	if platform.IsChinaMainland() {
		return "https://registry.npmmirror.com"
	}
	return "https://registry.npmjs.org"
}

// CheckForUpgrade queries the npm registry for the latest version.
// Returns (currentVersion, latestVersion, error).
func CheckForUpgrade() (string, string, error) {
	info, err := fetchUpgradeInfo()
	if err != nil {
		return version.Get(), "", err
	}
	return info.CurrentVersion, info.LatestVersion, nil
}

// fetchUpgradeInfo queries the npm registry once and returns all upgrade info.
func fetchUpgradeInfo() (*UpgradeInfo, error) {
	currentVer := version.Get()
	pkg, err := getPlatformPkg()
	if err != nil {
		return nil, err
	}

	registryBase := getRegistryBase()
	url := fmt.Sprintf("%s/%s/latest", registryBase, pkg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var npmResp npmRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	tarballURL := npmResp.Dist.Tarball
	if tarballURL == "" {
		return nil, fmt.Errorf("no tarball URL in registry response")
	}

	// If using npmmirror, rewrite tarball URL to npmmirror CDN
	if strings.HasPrefix(registryBase, "https://registry.npmmirror.com") {
		tarballURL = strings.Replace(tarballURL,
			"https://registry.npmjs.org", "https://registry.npmmirror.com", 1)
	}

	hasUpgrade := version.CompareVersions(currentVer, npmResp.Version) < 0 || version.IsDevBuild(currentVer)

	return &UpgradeInfo{
		CurrentVersion: currentVer,
		LatestVersion:  npmResp.Version,
		TarballURL:     tarballURL,
		Integrity:      npmResp.Dist.Integrity,
		HasUpgrade:     hasUpgrade,
	}, nil
}

// PerformUpgrade executes the full upgrade flow in a background goroutine.
func PerformUpgrade() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	upgradeCancel = cancel
	go performUpgrade(ctx)
}

// CancelUpgrade cancels the current upgrade process.
func CancelUpgrade() {
	if upgradeCancel != nil {
		upgradeCancel()
	}
}

func performUpgrade(ctx context.Context) {
	ResetUpgradeState()

	// 1. Check for upgrade (single registry query)
	setStateAndBroadcast(UpgradePhaseChecking, 0, "Checking for updates...")

	info, err := fetchUpgradeInfo()
	if err != nil {
		slog.Error("upgrade: version check failed", "error", err)
		SetUpgradeError(fmt.Sprintf("Failed to check version: %v", err))
		broadcastUpgradeUpdate()
		return
	}
	SetUpgradeVersions(info.CurrentVersion, info.LatestVersion)
	slog.Info("upgrade: version check", "current", info.CurrentVersion, "latest", info.LatestVersion,
		"compare", version.CompareVersions(info.CurrentVersion, info.LatestVersion), "isDev", version.IsDevBuild(info.CurrentVersion))

	if !info.HasUpgrade {
		SetUpgradeError("Already on the latest version")
		broadcastUpgradeUpdate()
		return
	}

	// 2. Download and extract (with timeout from ctx)
	setStateAndBroadcast(UpgradePhaseDownloading, 0, "Downloading...")

	tmpDir, err := os.MkdirTemp("", "clawbench-upgrade-*")
	if err != nil {
		SetUpgradeError(fmt.Sprintf("Failed to create temp dir: %v", err))
		broadcastUpgradeUpdate()
		return
	}

	newBinPath := filepath.Join(tmpDir, "clawbench-new")
	if runtime.GOOS == "windows" {
		newBinPath += ".exe"
	}

	if err := downloadAndExtract(ctx, info.TarballURL, info.Integrity, newBinPath); err != nil {
		os.RemoveAll(tmpDir)
		SetUpgradeError(fmt.Sprintf("Download/extract failed: %v", err))
		broadcastUpgradeUpdate()
		return
	}

	// 3. Supervisor check — Docker refuses self-replace
	isSupervised := upgradeIsSupervised != nil && upgradeIsSupervised()
	slog.Info("upgrade: supervisor check", "isSupervised", isSupervised, "isDocker", isDocker())
	if isSupervised && isDocker() {
		SetUpgradeError("Running in Docker — please pull new image: docker pull ghcr.io/xulongzhe/clawbench:latest")
		os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}

	// 4. Backup and launch upgrade-replace subprocess
	setStateAndBroadcast(UpgradePhaseBackingUp, 80, "Backing up current binary...")

	currentBin, err := os.Executable()
	if err != nil {
		SetUpgradeError(fmt.Sprintf("Failed to get current binary path: %v", err))
		os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}
	slog.Info("upgrade: current binary", "path", currentBin)

	backupPath := currentBin + ".bak"
	if err := copyFile(currentBin, backupPath); err != nil {
		SetUpgradeError(fmt.Sprintf("Failed to backup binary: %v", err))
		os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}
	os.Chmod(backupPath, 0755)
	SetUpgradeBackupPath(backupPath)
	slog.Info("upgrade: backup created", "path", backupPath)

	setStateAndBroadcast(UpgradePhaseReplacing, 90, "Replacing binary...")

	// Launch .bak with upgrade-replace subcommand
	args := []string{
		"upgrade-replace",
		"--new-bin", newBinPath,
		"--target", currentBin,
		"--tmp-dir", tmpDir,
	}
	// Pass through all original server flags (skip subcommands)
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--data-dir" || arg == "--port" || arg == "--host" {
			args = append(args, arg)
			if i+1 < len(os.Args) {
				i++
				args = append(args, os.Args[i])
			}
		} else if strings.HasPrefix(arg, "--data-dir=") || strings.HasPrefix(arg, "--port=") || strings.HasPrefix(arg, "--host=") {
			args = append(args, arg)
		}
	}

	slog.Info("upgrade: launching upgrade-replace subprocess", "backup", backupPath, "args", args)

	cmd := exec.Command(backupPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if err := cmd.Start(); err != nil {
		slog.Error("upgrade: failed to launch upgrade subprocess", "error", err)
		SetUpgradeError(fmt.Sprintf("Failed to launch upgrade subprocess: %v", err))
		os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}

	slog.Info("upgrade: upgrade-replace subprocess started", "pid", cmd.Process.Pid)

	setStateAndBroadcast(UpgradePhaseRestarting, 95, "Restarting...")

	// Gracefully shut down current process (no sentinel — upgrade-replace handles restart)
	slog.Info("upgrade: triggering shutdown (no sentinel)")
	if upgradeShutdownFunc != nil {
		upgradeShutdownFunc()
	}
}

// downloadAndExtract downloads the npm tarball and extracts the binary.
// ctx provides timeout and cancellation. integrity is the expected SHA-512 hash.
func downloadAndExtract(ctx context.Context, tarballURL, integrity, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarballURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// If integrity is provided, wrap with hashing reader for verification
	var bodyReader io.Reader = resp.Body
	var hasher hash.Hash
	if integrity != "" {
		hasher = sha512.New()
		bodyReader = io.TeeReader(resp.Body, hasher)
	}

	// Wrap body with progress reader (throttled)
	totalSize := resp.ContentLength
	progressReader := &progressReader{
		reader: bodyReader,
		total:  totalSize,
		onProgress: throttledProgress(func(progress int) {
			setStateAndBroadcast(UpgradePhaseDownloading, progress*70/100, "Downloading...")
		}),
	}

	// Extract binary from .tgz
	gzr, err := gzip.NewReader(progressReader)
	if err != nil {
		return fmt.Errorf("gzip decompress failed: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)
	binName := "clawbench"
	if runtime.GOOS == "windows" {
		binName = "clawbench.exe"
	}

	setStateAndBroadcast(UpgradePhaseExtracting, 70, "Extracting...")

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read failed: %w", err)
		}

		// Look for the binary in package/bin/
		if filepath.Base(header.Name) == binName && strings.Contains(header.Name, "bin/") {
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write binary: %w", err)
			}
			outFile.Close()
			os.Chmod(destPath, 0755)

			// Verify integrity if available
			if hasher != nil && integrity != "" {
				// Read remaining data to ensure hasher has full tarball content
				_, _ = io.Copy(io.Discard, gzr)

				if err := verifyIntegrity(hasher, integrity); err != nil {
					os.Remove(destPath)
					return fmt.Errorf("integrity verification failed: %w", err)
				}
				slog.Info("upgrade: integrity verified", "algorithm", "sha512")
			}

			return nil
		}
	}

	return fmt.Errorf("binary '%s' not found in tarball", binName)
}

// verifyIntegrity checks the downloaded tarball against the npm integrity string.
// The integrity string format is "sha512-<base64-hash>".
func verifyIntegrity(hasher hash.Hash, integrity string) error {
	if !strings.HasPrefix(integrity, "sha512-") {
		slog.Warn("upgrade: unsupported integrity algorithm, skipping verification", "integrity", integrity[:min(20, len(integrity))])
		return nil
	}
	expectedB64 := strings.TrimPrefix(integrity, "sha512-")
	expectedHash, err := base64.StdEncoding.DecodeString(expectedB64)
	if err != nil {
		return fmt.Errorf("failed to decode integrity hash: %w", err)
	}
	actualHash := hasher.Sum(nil)
	if !equalHashes(actualHash, expectedHash) {
		return fmt.Errorf("hash mismatch: expected %x, got %x", expectedHash[:8], actualHash[:8])
	}
	return nil
}

func equalHashes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// throttledProgress wraps an onProgress callback to only call it when the
// percentage actually changes, preventing excessive WS broadcasts.
func throttledProgress(fn func(int)) func(int) {
	var lastPercent int
	return func(p int) {
		if p != lastPercent {
			lastPercent = p
			fn(p)
		}
	}
}

// progressReader wraps an io.Reader to report download progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	read       int64
	onProgress func(percent int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)
	if pr.total > 0 && pr.onProgress != nil {
		percent := int(pr.read * 100 / pr.total)
		pr.onProgress(percent)
	}
	return n, err
}

// copyFile copies a file preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// isDocker checks if running inside a Docker container.
func isDocker() bool {
	if os.Getenv("container") != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

// setStateAndBroadcast sets upgrade state and broadcasts it via WS.
func setStateAndBroadcast(phase UpgradePhase, progress int, message string) {
	SetUpgradeState(phase, progress, message)
	broadcastUpgradeUpdate()
}

// broadcastUpgradeUpdate sends the current upgrade state via WS.
func broadcastUpgradeUpdate() {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	state := GetUpgradeState()
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  "event",
		Event: "upgrade_update",
		Data:  state,
	})
}

// CleanStaleUpgradeTempDirs removes leftover temp directories from previous
// upgrade attempts. Should be called on server startup.
func CleanStaleUpgradeTempDirs() {
	pattern := filepath.Join(os.TempDir(), "clawbench-upgrade-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, dir := range matches {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		// Only clean up directories older than 1 hour (avoid removing active upgrade)
		if time.Since(info.ModTime()) > time.Hour {
			if err := os.RemoveAll(dir); err == nil {
				slog.Info("upgrade: cleaned stale temp dir", "path", dir)
			}
		}
	}
}
