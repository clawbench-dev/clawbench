package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
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

	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/version"
	"clawbench/internal/ws"
)

// upgradeShutdownFunc is called to gracefully shut down the server during upgrade.
// Unlike upgradeRestartFunc, this does NOT launch a sentinel process — the
// upgrade-replace subprocess handles restarting after replacing the binary.
var upgradeShutdownFunc func()

// SetUpgradeShutdownFunc sets the function called to gracefully shut down during upgrade.
func SetUpgradeShutdownFunc(f func()) {
	upgradeShutdownFunc = f
}

// upgradeIsSupervised reports whether the process is running under a supervisor.
// Set by main.go via SetUpgradeIsSupervised().
var upgradeIsSupervised func() bool

// SetUpgradeIsSupervised sets the function that reports supervisor status.
func SetUpgradeIsSupervised(f func() bool) {
	upgradeIsSupervised = f
}

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
		Tarball string `json:"tarball"`
	} `json:"dist"`
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
	currentVer := version.Get()
	pkg, err := getPlatformPkg()
	if err != nil {
		return currentVer, "", err
	}

	registryBase := getRegistryBase()
	url := fmt.Sprintf("%s/%s/latest", registryBase, pkg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return currentVer, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return currentVer, "", fmt.Errorf("failed to query registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return currentVer, "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var npmResp npmRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return currentVer, "", fmt.Errorf("failed to decode registry response: %w", err)
	}

	return currentVer, npmResp.Version, nil
}

// PerformUpgrade executes the full upgrade flow in a background goroutine.
func PerformUpgrade() {
	go performUpgrade()
}

func performUpgrade() {
	ResetUpgradeState()

	// 1. Check for upgrade
	SetUpgradeState(UpgradePhaseChecking, 0, "Checking for updates...")
	broadcastUpgradeUpdate()

	currentVer, latestVer, err := CheckForUpgrade()
	if err != nil {
		slog.Error("upgrade: version check failed", "error", err)
		SetUpgradeError(fmt.Sprintf("Failed to check version: %v", err))
		broadcastUpgradeUpdate()
		return
	}
	SetUpgradeVersions(currentVer, latestVer)
	slog.Info("upgrade: version check", "current", currentVer, "latest", latestVer,
		"compare", version.CompareVersions(currentVer, latestVer), "isDev", version.IsDevBuild(currentVer))

	if version.CompareVersions(currentVer, latestVer) >= 0 && !version.IsDevBuild(currentVer) {
		SetUpgradeError("Already on the latest version")
		broadcastUpgradeUpdate()
		return
	}

	// 2. Get tarball URL
	pkg, _ := getPlatformPkg()
	registryBase := getRegistryBase()
	tarballURL, err := getTarballURL(registryBase, pkg, latestVer)
	if err != nil {
		SetUpgradeError(fmt.Sprintf("Failed to get tarball URL: %v", err))
		broadcastUpgradeUpdate()
		return
	}

	// 3. Download and extract
	SetUpgradeState(UpgradePhaseDownloading, 0, "Downloading...")
	broadcastUpgradeUpdate()

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

	if err := downloadAndExtract(tarballURL, newBinPath); err != nil {
		os.RemoveAll(tmpDir)
		SetUpgradeError(fmt.Sprintf("Download/extract failed: %v", err))
		broadcastUpgradeUpdate()
		return
	}

	// 4. Supervisor check — Docker refuses self-replace
	isSupervised := upgradeIsSupervised != nil && upgradeIsSupervised()
	slog.Info("upgrade: supervisor check", "isSupervised", isSupervised, "isDocker", isDocker())
	if isSupervised && isDocker() {
		SetUpgradeError("Running in Docker — please pull new image: docker pull ghcr.io/xulongzhe/clawbench:latest")
		os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}

	// 5. Backup and launch upgrade-replace subprocess (works for both supervised and non-supervised)
	// For systemd: upgrade-replace kills parent, replaces binary, starts new — systemd will
	// also try to restart but upgrade-replace already started the new process.
	// For non-supervised (including false positives where ppid==1): same approach.
	SetUpgradeState(UpgradePhaseBackingUp, 80, "Backing up current binary...")
	broadcastUpgradeUpdate()

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

	SetUpgradeState(UpgradePhaseReplacing, 90, "Replacing binary...")
	broadcastUpgradeUpdate()

	// Launch .bak with upgrade-replace subcommand
	args := []string{
		"upgrade-replace",
		"--new-bin", newBinPath,
		"--target", currentBin,
		"--data-dir", model.DataDir,
	}
	// Pass through --port if set
	for i, arg := range os.Args[1:] {
		if arg == "--port" && i+1 < len(os.Args[1:]) {
			args = append(args, "--port", os.Args[i+2])
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

	SetUpgradeState(UpgradePhaseRestarting, 95, "Restarting...")
	broadcastUpgradeUpdate()

	// Gracefully shut down current process (no sentinel — upgrade-replace handles restart)
	slog.Info("upgrade: triggering shutdown (no sentinel)")
	if upgradeShutdownFunc != nil {
		upgradeShutdownFunc()
	}
}

// getTarballURL queries the registry for the tarball download URL.
func getTarballURL(registryBase, pkg, ver string) (string, error) {
	url := fmt.Sprintf("%s/%s/latest", registryBase, pkg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var npmResp npmRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return "", err
	}

	if npmResp.Dist.Tarball == "" {
		return "", fmt.Errorf("no tarball URL in registry response")
	}

	// If using npmmirror, rewrite tarball URL to npmmirror CDN
	if strings.HasPrefix(registryBase, "https://registry.npmmirror.com") {
		npmResp.Dist.Tarball = strings.Replace(npmResp.Dist.Tarball,
			"https://registry.npmjs.org", "https://registry.npmmirror.com", 1)
	}

	return npmResp.Dist.Tarball, nil
}

// downloadAndExtract downloads the npm tarball and extracts the binary.
func downloadAndExtract(tarballURL, destPath string) error {
	resp, err := http.Get(tarballURL) //nolint:gosec // URL from npm registry
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Wrap body with progress reader
	totalSize := resp.ContentLength
	progressReader := &progressReader{
		reader: resp.Body,
		total:  totalSize,
		onProgress: func(progress int) {
			SetUpgradeState(UpgradePhaseDownloading, progress*70/100, "Downloading...")
			broadcastUpgradeUpdate()
		},
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

	SetUpgradeState(UpgradePhaseExtracting, 70, "Extracting...")
	broadcastUpgradeUpdate()

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
			return nil
		}
	}

	return fmt.Errorf("binary '%s' not found in tarball", binName)
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

// replaceBinary replaces the target binary with the new one.
func replaceBinary(newBin, target string) error {
	// Try rename first (works on same filesystem)
	if err := os.Rename(newBin, target); err != nil {
		slog.Warn("upgrade: rename failed, falling back to copy", "error", err)
		// Fallback: copy content
		if err := copyFile(newBin, target); err != nil {
			return fmt.Errorf("copy fallback failed: %w", err)
		}
	}
	os.Chmod(target, 0755)
	return nil
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
