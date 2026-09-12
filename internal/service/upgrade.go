package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"clawbench/internal/platform"
	"clawbench/internal/version"
	"clawbench/internal/ws"
)

// osWindows is used for runtime.GOOS comparison to avoid goconst duplication.
const osWindows = "windows"

// URL schemes recognized by isValidRegistryURL.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// upgradeHTTPClient is the HTTP client used for upgrade requests.
// Overridden in tests to point at httptest.NewServer.
var upgradeHTTPClient = http.DefaultClient

// upgradeShutdownFunc is called to gracefully shut down the server during upgrade.
var upgradeShutdownFunc func()

// SetUpgradeShutdownFunc sets the function called to gracefully shut down during upgrade.
func SetUpgradeShutdownFunc(f func()) {
	upgradeShutdownFunc = f
}

// upgradeRestartFunc triggers a restart without replacing the binary. Used by
// the version short-circuit, where the wanted binary is already on disk and
// only needs to be re-executed. Wired to the server's restart function.
var upgradeRestartFunc func()

// SetUpgradeRestartFunc sets the function called to restart without replacing.
func SetUpgradeRestartFunc(f func()) {
	upgradeRestartFunc = f
}

// selfBinaryVersion reports the version of the binary at the given path. It is
// a variable so tests can avoid executing a real binary.
var selfBinaryVersion = probeBinaryVersion

// upgradeExecutable resolves the running binary path. Overridden in tests.
var upgradeExecutable = os.Executable

// upgradeIsSupervised reports whether the process is running under a supervisor.
var upgradeIsSupervised func() bool

// SetUpgradeIsSupervised sets the function that reports supervisor status.
func SetUpgradeIsSupervised(f func() bool) {
	upgradeIsSupervised = f
}

// upgradeIsContainer reports whether the process runs inside a container.
// Overridden in tests.
var upgradeIsContainer = platform.IsContainer

// resolveReplaceInPlace decides whether the upgrade should replace the binary
// in place and rely on an external restart, rather than spawning the
// self-restart `upgrade-replace` subprocess.
//
// A container always returns true. The subprocess path cannot work there: the
// runtime destroys the namespace when PID 1 exits, killing the helper before it
// can replace anything, so the service would silently come back on the old
// binary. This holds regardless of the supervisor probe — k8s, runit and
// supervisord are all unrecognized by it, which must not change the container's
// behavior.
//
// Outside a container the probe is authoritative: a truly unsupervised
// deployment (e.g. a plain nohup/setsid process) needs the subprocess path,
// since nothing else would restart it.
func resolveReplaceInPlace(isContainer, probeSupervised bool) bool {
	return isContainer || probeSupervised
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

// getUserRegistryBase returns the user's configured npm mirror base URL, or ""
// if none is configured. It checks, in order: the NPM_CONFIG_REGISTRY env var,
// the npm_config_registry env var, then the registry= line in the user-level
// ~/.npmrc. The result is trimmed and validated as an http(s) URL.
func getUserRegistryBase() string {
	candidates := []string{
		os.Getenv("NPM_CONFIG_REGISTRY"),
		os.Getenv("npm_config_registry"),
		parseNpmRcRegistry(),
	}
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		// Reject non-http(s) values (e.g. "default" or a registry scope key),
		// and values with no host (e.g. "http://" or "http:"), which would
		// otherwise produce a malformed request URL later.
		if !isValidRegistryURL(c) {
			continue
		}
		return strings.TrimRight(c, "/")
	}
	return ""
}

// isValidRegistryURL reports whether v is an http(s) URL with a usable host.
func isValidRegistryURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case schemeHTTP, schemeHTTPS:
		return true
	default:
		return false
	}
}

// parseNpmRcRegistry reads the user-level .npmrc and returns the value of the
// registry= line, or "" if absent/unreadable. Path resolution uses
// platform.UserHomeDir so it works on Windows, macOS and Linux.
func parseNpmRcRegistry() string {
	path := filepath.Join(platform.UserHomeDir(), ".npmrc")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "registry" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// registryCandidates returns the ordered list of registry bases to try: the
// default base first, followed by the user's configured mirror (if any).
func registryCandidates() []string {
	bases := []string{getRegistryBase()}
	if m := getUserRegistryBase(); m != "" {
		bases = append(bases, m)
	}
	return bases
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

// fetchUpgradeInfo queries the npm registry for upgrade info, trying the
// default registry base first and falling back to the user's configured mirror
// if the default cannot be reached.
func fetchUpgradeInfo() (*UpgradeInfo, error) {
	currentVer := version.Get()
	pkg, err := getPlatformPkg()
	if err != nil {
		return nil, err
	}

	var errs []error
	for _, registryBase := range registryCandidates() {
		info, err := fetchUpgradeInfoFromBase(registryBase, pkg, currentVer)
		if err == nil {
			return info, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", registryBase, err))
		slog.Warn("upgrade: registry query failed, trying next candidate", "base", registryBase, "error", err)
	}

	return nil, fmt.Errorf("all registry sources failed: %w", errors.Join(errs...))
}

// fetchUpgradeInfoFromBase queries a single registry base and returns upgrade info.
func fetchUpgradeInfoFromBase(registryBase, pkg, currentVer string) (*UpgradeInfo, error) {
	url := fmt.Sprintf("%s/%s/latest", registryBase, pkg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := upgradeHTTPClient.Do(req)
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

	// Some mirrors (e.g. Nexus) return a malformed dist.tarball that keeps the
	// dist-tag in the package-name segment ("@scope/pkg@latest/-/..."); strip it
	// before pointing the tarball at the base used for the query.
	tarballURL = normalizeTarballURL(tarballURL)
	tarballURL = rewriteTarballURL(tarballURL, registryBase)

	hasUpgrade := version.CompareVersions(currentVer, npmResp.Version) < 0 || version.IsDevBuild(currentVer)

	return &UpgradeInfo{
		CurrentVersion: currentVer,
		LatestVersion:  npmResp.Version,
		TarballURL:     tarballURL,
		Integrity:      npmResp.Dist.Integrity,
		HasUpgrade:     hasUpgrade,
	}, nil
}

// normalizeTarballURL removes a dist-tag suffix (e.g. "@latest") that some
// registry mirrors incorrectly leave in the package-name segment of a tarball
// URL. A standard npm URL looks like ".../@scope/pkg/-/pkg-1.2.3.tgz", but
// Nexus-backed mirrors have been observed returning
// ".../@scope/pkg@latest/-/pkg-1.2.3.tgz", which 404s. The dist-tag is the
// trailing "@tag" on the path segment immediately preceding "/-/". URLs
// without "/-/" or without such a suffix are returned unchanged.
func normalizeTarballURL(tarball string) string {
	// Everything from "/-/" onward is the filename part ("-/pkg-1.2.3.tgz") and
	// is never touched; only the package-name segment before it can be malformed.
	const sep = "/-/"
	idx := strings.Index(tarball, sep)
	if idx < 0 {
		// Not a standard npm tarball path — leave it alone rather than guess.
		return tarball
	}
	prefix, suffix := tarball[:idx], tarball[idx:]

	// The package name is the final path segment of prefix, e.g. "pkg@latest"
	// or "@scope/pkg@latest".
	segStart := strings.LastIndex(prefix, "/") + 1
	seg := prefix[segStart:]

	// Strip a trailing "@tag". at > 0 (not at >= 0) preserves a leading "@",
	// which marks an npm scope rather than a dist-tag, so "@scope/pkg" stays
	// intact while "@scope/pkg@latest" loses only the "@latest".
	if at := strings.LastIndex(seg, "@"); at > 0 {
		return prefix[:segStart] + seg[:at] + suffix
	}
	return tarball
}

// rewriteTarballURL points the tarball at the same registry base used for the query.
func rewriteTarballURL(tarball, base string) string {
	const npmjs = "https://registry.npmjs.org"
	if base != npmjs && len(tarball) > len(npmjs) && tarball[:len(npmjs)] == npmjs {
		return base + tarball[len(npmjs):]
	}
	return tarball
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

func performUpgrade(ctx context.Context) { //nolint:gocyclo // upgrade flow is inherently multi-step
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

	// 1b. Resolve the running binary and refuse environments where self-replace
	// does not apply. Done before any network or disk work so a non-applicable
	// deployment gets the clearest error without downloading the tarball.
	//
	// resolveSelfBinary prefers the path recorded at startup over
	// os.Executable(): the latter reports where the binary is *now*, so an
	// external package manager replacing the package mid-run (npm retires and
	// deletes the old package directory) leaves it pointing at a deleted inode.
	currentBin, err := ResolveSelfBinary()
	if err != nil {
		slog.Warn("upgrade: cannot resolve running binary", "error", err)
		SetUpgradeErrorCode(UpgradeErrSelfPathUnresolved,
			fmt.Sprintf("Cannot locate the running ClawBench binary: %v. "+
				"If it was replaced or removed by a package manager while the service "+
				"was running, restart ClawBench and try again.", err))
		broadcastUpgradeUpdate()
		return
	}
	slog.Info("upgrade: current binary", "path", currentBin)

	isContainerEnv := upgradeIsContainer()
	probeSupervised := upgradeIsSupervised != nil && upgradeIsSupervised()
	isSupervised := resolveReplaceInPlace(isContainerEnv, probeSupervised)
	slog.Info("upgrade: supervisor check",
		"isSupervised", isSupervised, "isContainer", isContainerEnv,
		"probeSupervised", probeSupervised)

	if isContainerEnv {
		slog.Info("upgrade: container detected — using in-place replace + restart-policy restart",
			"dockerLike", platform.IsDockerLike())
	}

	// 1c. Version short-circuit: when the binary already on disk is at (or
	// ahead of) the target — the normal outcome of updating through the
	// package manager instead of the UI — downloading the same version again
	// would waste a full tarball transfer. Restarting is enough to load it.
	//
	// Placed before the install-directory preflight: this path writes nothing,
	// so an unwritable install directory must not block it.
	//
	// The probe is best-effort: if it fails, fall through to the normal path.
	// Likewise when no restart function is wired: short-circuiting without a
	// way to restart would leave the upgrade reported as restarting forever.
	if diskVer, verErr := selfBinaryVersion(currentBin); verErr == nil {
		if shouldShortCircuit(diskVer, info.LatestVersion) && upgradeRestartFunc != nil {
			slog.Info("upgrade: disk binary already at target version — restarting without download",
				"disk", diskVer, "target", info.LatestVersion)
			setStateAndBroadcast(UpgradePhaseRestarting, 95, "Restarting...")
			upgradeRestartFunc()
			return
		}
	} else {
		slog.Warn("upgrade: version probe failed, proceeding with download",
			"path", currentBin, "error", verErr)
	}

	// 1d. Preflight: the install directory (and the backup path) must be
	// writable, otherwise the backup step would fail after downloading the whole
	// tarball. Fail fast with an actionable code so the UI can tell the user
	// what to do.
	if dir, permErr := CheckInstallDirWritable(); permErr != nil {
		slog.Warn("upgrade: install directory not writable", "dir", dir, "error", permErr)
		SetUpgradeErrorCode(UpgradeErrInstallDirNotWritable,
			fmt.Sprintf("Install directory %s is not writable by the current user: %v. "+
				"Re-run with sudo or install ClawBench to a user-writable directory.", dir, permErr))
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
	if runtime.GOOS == osWindows {
		newBinPath += ".exe"
	}

	if downloadErr := downloadAndExtract(ctx, info.TarballURL, info.Integrity, newBinPath); downloadErr != nil {
		_ = os.RemoveAll(tmpDir)
		SetUpgradeError(fmt.Sprintf("Download/extract failed: %v", downloadErr))
		broadcastUpgradeUpdate()
		return
	}

	// 3. Backup current binary (used for rollback and as the replacement launcher).
	setStateAndBroadcast(UpgradePhaseBackingUp, 80, "Backing up current binary...")

	backupPath := currentBin + ".bak"
	if err := copyFile(currentBin, backupPath); err != nil {
		SetUpgradeError(fmt.Sprintf("Failed to backup binary: %v", err))
		_ = os.RemoveAll(tmpDir)
		broadcastUpgradeUpdate()
		return
	}
	_ = os.Chmod(backupPath, 0o755) //nolint:gosec // G302: backup binary must be executable
	SetUpgradeBackupPath(backupPath)
	slog.Info("upgrade: backup created", "path", backupPath)

	// Supervised (non-Docker, e.g. systemd): replace the binary in place, then
	// shut down and let the supervisor restart the service. The upgrade-replace
	// self-restart subprocess is incompatible with supervised deployment — the
	// supervisor kills the unit's cgroup (and any orphan it spawned) once the
	// main process exits, so a self-spawned replacement would never survive.
	if isSupervised {
		slog.Info("upgrade: supervised non-Docker deploy — replacing in place, letting supervisor restart")
		if err := performSupervisedUpgrade(newBinPath, currentBin); err != nil {
			SetUpgradeError(err.Error())
			_ = os.RemoveAll(tmpDir)
			broadcastUpgradeUpdate()
			return
		}
		_ = os.RemoveAll(tmpDir)
		return
	}

	// 4. Unsupervised: launch upgrade-replace subprocess.
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

	cmd := exec.CommandContext(context.Background(), backupPath, args...) //nolint:gosec // G702: backupPath is a copy of the current binary created by the upgrade process; noctx: subprocess must outlive the upgrade context
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	setCmdProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		slog.Error("upgrade: failed to launch upgrade subprocess", "error", err)
		SetUpgradeError(fmt.Sprintf("Failed to launch upgrade subprocess: %v", err))
		_ = os.RemoveAll(tmpDir)
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
func downloadAndExtract(ctx context.Context, tarballURL, integrity, destPath string) error { //nolint:gocyclo // download+extract+verify is inherently multi-branch
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarballURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := upgradeHTTPClient.Do(req)
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
	defer func() { _ = gzr.Close() }()

	tarReader := tar.NewReader(gzr)
	binName := "clawbench"
	if runtime.GOOS == osWindows {
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
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			//nolint:gosec // G110: tarball size verified by integrity hash
			if _, copyErr := io.Copy(outFile, tarReader); copyErr != nil {
				_ = outFile.Close()
				return fmt.Errorf("failed to write binary: %w", copyErr)
			}
			_ = outFile.Close()
			_ = os.Chmod(destPath, 0o755) //nolint:gosec // G302: binary must be executable

			// Verify integrity if available
			if hasher != nil && integrity != "" {
				// Read remaining data to ensure hasher has full tarball content
				_, _ = io.Copy(io.Discard, gzr) //nolint:gosec // G110: integrity hash validates tarball

				if verifyErr := verifyIntegrity(hasher, integrity); verifyErr != nil {
					_ = os.Remove(destPath)
					return fmt.Errorf("integrity verification failed: %w", verifyErr)
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
		slog.Warn("upgrade: unsupported integrity algorithm, skipping verification", "integrity", integrity[:min(len(integrity), 20)])
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
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, copyErr := io.Copy(out, in); copyErr != nil {
		return copyErr
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// upgradeRename overrides os.Rename so tests can force a cross-device error and
// exercise the copy fallback. Real deployments use os.Rename.
var upgradeRename = os.Rename

// replaceBinaryInPlace replaces the target binary with the new binary,
// preferring an atomic rename and falling back to a staged copy when the new
// binary lives on a different filesystem (e.g. /tmp vs the install dir). The
// target is made executable. On Unix this is safe even while the current
// process is running — the running process keeps its old inode, and future
// starts use the new file.
//
// The fallback copies into a temp file in the target's directory and renames it
// over the target, rather than writing the target in place. That keeps the
// requirement to directory write permission, matching what the preflight
// checks: overwriting the target directly would additionally require the target
// file itself to be writable, which fails for e.g. a root-owned 0755 binary in
// a user-writable directory.
func replaceBinaryInPlace(newPath, target string) error {
	if err := upgradeRename(newPath, target); err == nil {
		return os.Chmod(target, 0o755) //nolint:gosec // G302: binary must be executable
	}

	// Staged copy: temp file in the target dir, then atomic rename over target.
	src, err := os.Open(newPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(target), ".clawbench-replace-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	// Use os.Rename directly (not the test hook): the source now lives in the
	// target directory, so this is the real same-filesystem rename.
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Chmod(target, 0o755) //nolint:gosec // G302: binary must be executable
}

// performSupervisedUpgrade replaces the running binary in place and triggers a
// graceful shutdown so an external supervisor (e.g. systemd) restarts the
// service with the new binary. It returns an error if the replacement fails;
// shutdown is only triggered after a successful replacement.
func performSupervisedUpgrade(newBinPath, currentBin string) error {
	setStateAndBroadcast(UpgradePhaseReplacing, 90, "Replacing binary...")
	if err := replaceBinaryInPlace(newBinPath, currentBin); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}
	setStateAndBroadcast(UpgradePhaseRestarting, 95, "Restarting...")
	slog.Info("upgrade: triggering supervised shutdown for restart")
	if upgradeShutdownFunc != nil {
		upgradeShutdownFunc()
	}
	return nil
}

// CheckInstallDirWritable reports whether the current user can perform the
// filesystem operations a self-upgrade needs next to the running binary:
// creating the ".bak" backup and staging a replacement in the same directory.
// Both are directory operations — the binary's own mode bits are not enough (a
// rename over a read-only file succeeds as long as the directory is writable).
//
// The check actually creates and removes files rather than inspecting mode
// bits, so it correctly reflects ACLs, read-only mounts and mandatory access
// control. The backup path is probed explicitly because an existing
// non-writable ".bak" (e.g. left behind by a previous root-run upgrade) would
// block the backup even though the directory itself is writable. A pre-existing
// ".bak" is left in place — only a file this probe created is removed.
//
// It returns the install directory (for user-facing messages) alongside the
// error.
func CheckInstallDirWritable() (string, error) {
	exe, err := ResolveSelfBinary()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)

	// 1. The directory must accept new files (backup + staged replace).
	f, err := os.CreateTemp(dir, ".clawbench-permcheck-*")
	if err != nil {
		return dir, err
	}
	name := f.Name()
	_ = f.Close()
	if rmErr := os.Remove(name); rmErr != nil {
		// Surface a leftover probe file rather than silently ignoring it.
		slog.Warn("upgrade: failed to remove permission probe file", "path", name, "error", rmErr)
	}

	// 2. The specific backup path must be writable (overwriting/creating it).
	backupPath := exe + ".bak"
	_, statErr := os.Stat(backupPath)
	existedBefore := statErr == nil

	bf, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return dir, err
	}
	if closeErr := bf.Close(); closeErr != nil {
		slog.Warn("upgrade: failed to close backup probe file", "path", backupPath, "error", closeErr)
	}
	// Only remove what we created; never delete a pre-existing backup.
	if !existedBefore {
		if rmErr := os.Remove(backupPath); rmErr != nil {
			slog.Warn("upgrade: failed to remove backup probe file", "path", backupPath, "error", rmErr)
		}
	}
	return dir, nil
}

// IsDocker reports whether the process runs under Docker or Podman — i.e.
// where the `docker pull` / `docker compose` advice is actionable.
//
// Kept as a thin wrapper over platform.IsDockerLike for the upgrade API's
// `is_docker` field. Use platform.IsContainer for supervision decisions: a k8s
// pod is a container (so self-restart is impossible) but not Docker-like.
func IsDocker() bool {
	return platform.IsDockerLike()
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
