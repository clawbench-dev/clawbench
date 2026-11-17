package platform

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Private Node.js runtime.
//
// The one-line installer (scripts/install.sh, scripts/install.ps1) has always
// shipped its own Node.js under $CLAWBENCH_HOME/node. This file gives the
// SERVER the same ability, so the one-click agent install works on a machine
// that has never had Node at all: instead of failing with "npm was not found",
// it downloads the current LTS into that same directory and puts it on PATH.
//
// A private copy rather than the system package manager: no sudo, no
// distro-specific branch, no interactive prompt. The trade-off is that this
// Node.js is visible only to ClawBench and the processes it spawns (the agent
// CLIs) — which is exactly the audience that needs it.
//
// Sharing the installer's directory matters: a machine installed with
// `curl … | install.sh` already has a working Node there, and creating a second
// copy elsewhere would leave two versions to keep in sync.

const (
	// nodeMirrorBaseURL is npmmirror's Node.js binary mirror. Same source the
	// one-line installer uses for machines in mainland China.
	nodeMirrorBaseURL = "https://registry.npmmirror.com/-/binary/node"
	// nodeDistBaseURL is the official distribution server.
	nodeDistBaseURL = "https://nodejs.org/dist"

	// nodeMinMajor mirrors the one-line installer: an agent CLI that needs
	// Node 20 must not silently run on an older system Node.
	nodeMinMajor = 20

	// nodeInstallTimeout bounds the whole download-and-unpack. A 30MB archive
	// over a slow link is minutes, not seconds; the caller's install budget is
	// separate and larger.
	nodeInstallTimeout = 5 * time.Minute

	// nodeMaxArchiveBytes caps the download. The real archive is ~30MB (Linux
	// x64) to ~50MB (Windows x64); the ceiling only exists so a misbehaving
	// mirror cannot fill the disk.
	nodeMaxArchiveBytes = 200 << 20

	// nodeMaxIndexBytes bounds index.json / SHASUMS256.txt reads.
	nodeMaxIndexBytes = 8 << 20
)

// nodeHTTPClient has no Timeout: the context passed to each request carries the
// deadline, and a client timeout would cut a slow-but-working download short.
var nodeHTTPClient = &http.Client{}

// nodeRelease is one entry of <base>/index.json. LTS is `false` for releases
// that are not LTS and a name ("Jod") for those that are, so it is decoded as
// any and inspected rather than guessed from the major being even.
type nodeRelease struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"`
}

// ManagedNodeHome returns the directory the one-line installer uses for its
// private runtime (and, on Windows, the desktop app's per-user data root).
func ManagedNodeHome() string {
	if home := strings.TrimSpace(os.Getenv("CLAWBENCH_HOME")); home != "" {
		return home
	}
	if IsWindows() {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "clawbench")
		}
		return filepath.Join(UserHomeDir(), "clawbench")
	}
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(data, "clawbench")
	}
	return filepath.Join(UserHomeDir(), ".local", "share", "clawbench")
}

// ManagedNodeDir is where the private Node.js lives.
func ManagedNodeDir() string {
	return filepath.Join(ManagedNodeHome(), "node")
}

// managedNodeBinDir is the directory that must be on PATH for `node` / `npm` to
// resolve. Windows keeps node.exe at the root of the install directory.
func managedNodeBinDir() string {
	if IsWindows() {
		return ManagedNodeDir()
	}
	return filepath.Join(ManagedNodeDir(), "bin")
}

// PrependManagedNodePath puts the private runtime first on this process's PATH.
// Reports whether PATH changed.
//
// First rather than last on purpose: the agent CLIs installed by this flow were
// resolved against this Node, and a system Node too old for them would fail at
// spawn time with a syntax error rather than something actionable.
func PrependManagedNodePath() bool {
	dir := managedNodeBinDir()
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	pathMu.Lock()
	defer pathMu.Unlock()

	current := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(current) {
		if entry == dir {
			return false
		}
	}
	next := dir
	if current != "" {
		next = dir + string(os.PathListSeparator) + current
	}
	if err := os.Setenv("PATH", next); err != nil {
		slog.Warn("could not add the private Node.js to PATH", "dir", dir, "error", err)
		return false
	}
	slog.Info("added the private Node.js to PATH", "dir", dir)
	return true
}

// EnsureNodeRuntime makes `node` and `npm` usable, downloading a private Node.js
// when the machine has none (or one older than nodeMinMajor).
//
// Returns a short human-readable note when something was installed, and "" when
// the machine already had a usable Node. The note is meant for the install
// output the user is watching, so it is a single line of plain text.
func EnsureNodeRuntime() (string, error) {
	if nodeRuntimeReady() {
		return "", nil
	}

	// A private copy may already be on disk — installed by a previous run or by
	// the one-line installer, whose PATH line only applies to interactive
	// shells. Try it before downloading another one.
	if managedNodeInstalled() {
		PrependManagedNodePath()
		if nodeRuntimeReady() {
			return "", nil
		}
	}

	version, err := installManagedNode()
	if err != nil {
		return "", err
	}
	PrependManagedNodePath()
	if !nodeRuntimeReady() {
		return "", fmt.Errorf("unpacked Node.js %s to %s, but it does not run: "+
			"the system is probably missing shared libraries (libatomic1 on Debian/Ubuntu)",
			version, ManagedNodeDir())
	}
	return fmt.Sprintf("ClawBench installed Node.js %s (private copy: %s)", version, ManagedNodeDir()), nil
}

// nodeRuntimeReady reports whether a Node new enough for the agent CLIs is
// resolvable on the current PATH.
func nodeRuntimeReady() bool {
	if _, err := exec.LookPath("node"); err != nil {
		return false
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return false
	}
	return nodeMajorVersion() >= nodeMinMajor
}

// managedNodeInstalled reports whether the private runtime is on disk. It says
// nothing about whether it runs.
func managedNodeInstalled() bool {
	info, err := os.Stat(filepath.Join(managedNodeBinDir(), nodeExecutableName()))
	return err == nil && !info.IsDir()
}

func nodeExecutableName() string {
	if IsWindows() {
		return "node.exe"
	}
	return "node"
}

// nodeMajorVersion parses the major from `node -v` ("v22.11.0" → 22) for the
// `node` on PATH. Returns 0 when it cannot be run or prints something else.
func nodeMajorVersion() int {
	// A bound of its own: a broken `node` that hangs must not hold the install
	// slot until the outer timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", "-v").Output()
	if err != nil {
		return 0
	}
	major, _, _, ok := parseNodeVersion(strings.TrimSpace(string(out)))
	if !ok {
		return 0
	}
	return major
}

// installManagedNode downloads and unpacks the current LTS, trying the mirror
// chosen for this machine first and the other source second.
func installManagedNode() (string, error) {
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), nodeInstallTimeout)
	defer cancel()

	var lastErr error
	for _, base := range nodeDownloadBases() {
		version, err := fetchLatestNodeLTS(ctx, base)
		if err != nil {
			slog.Warn("could not resolve the Node.js LTS version", "source", base, "error", err)
			lastErr = err
			continue
		}
		if err := downloadNodeRelease(ctx, base, version, osName, arch); err != nil {
			slog.Warn("could not install Node.js", "source", base, "version", version, "error", err)
			lastErr = err
			continue
		}
		slog.Info("downloaded Node.js", "version", version, "source", base)
		return version, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no download source available")
	}
	return "", fmt.Errorf("could not install Node.js: %w", lastErr)
}

// nodeDownloadBases returns the sources in the order to try them. The choice is
// the same mainland-China probe the npm registry rewrite uses, so a machine that
// gets the npmmirror registry also gets the npmmirror binaries.
//
// A variable so a test can point it at a local server instead of the internet.
var nodeDownloadBases = func() []string {
	if IsChinaMainland() {
		return []string{nodeMirrorBaseURL, nodeDistBaseURL}
	}
	return []string{nodeDistBaseURL, nodeMirrorBaseURL}
}

// nodeReleasePlatform maps GOOS/GOARCH onto Node.js's release naming.
func nodeReleasePlatform() (string, string, error) {
	var osName string
	switch {
	case IsWindows():
		osName = "win"
	case runtime.GOOS == "linux":
		osName = "linux"
	case runtime.GOOS == "darwin":
		osName = "darwin"
	default:
		return "", "", fmt.Errorf("cannot install Node.js automatically on %s", runtime.GOOS)
	}

	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	case "386":
		arch = "x86"
	default:
		return "", "", fmt.Errorf("cannot install Node.js automatically on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Windows on ARM: Node.js ships win-arm64, but npm packages routinely lack
	// win32-arm64 prebuilt binaries, so the one-line installer deliberately uses
	// the x64 build under emulation. Match it, or the install that follows this
	// download would fail on a missing prebuilt.
	if osName == "win" && arch == "arm64" {
		arch = "x64"
	}
	return osName, arch, nil
}

// fetchLatestNodeLTS reads <base>/index.json and picks the newest LTS release
// that is at least nodeMinMajor.
func fetchLatestNodeLTS(ctx context.Context, base string) (string, error) {
	body, err := httpGetLimited(ctx, base+"/index.json", nodeMaxIndexBytes)
	if err != nil {
		return "", err
	}
	var releases []nodeRelease
	if unmarshalErr := json.Unmarshal(body, &releases); unmarshalErr != nil {
		return "", fmt.Errorf("%s/index.json: %w", base, unmarshalErr)
	}
	version, err := pickLatestLTS(releases)
	if err != nil {
		return "", fmt.Errorf("%s/index.json: %w", base, err)
	}
	return version, nil
}

// pickLatestLTS returns the highest LTS version at or above nodeMinMajor.
func pickLatestLTS(releases []nodeRelease) (string, error) {
	best := ""
	bestMajor, bestMinor, bestPatch := -1, -1, -1
	for _, release := range releases {
		if !isLTSRelease(release.LTS) {
			continue
		}
		major, minor, patch, ok := parseNodeVersion(release.Version)
		if !ok || major < nodeMinMajor {
			continue
		}
		if newerNodeVersion(major, minor, patch, bestMajor, bestMinor, bestPatch) {
			best, bestMajor, bestMinor, bestPatch = release.Version, major, minor, patch
		}
	}
	if best == "" {
		return "", fmt.Errorf("no LTS release of v%d or newer", nodeMinMajor)
	}
	return best, nil
}

func newerNodeVersion(major, minor, patch, thanMajor, thanMinor, thanPatch int) bool {
	if major != thanMajor {
		return major > thanMajor
	}
	if minor != thanMinor {
		return minor > thanMinor
	}
	return patch > thanPatch
}

// isLTSRelease reports whether index.json's "lts" field marks a release as LTS.
// It is `false` (bool) for non-LTS releases and the codename (string) for LTS
// ones — guessing from an even major is wrong (v24 was once flagged false).
func isLTSRelease(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed != ""
	case bool:
		return typed
	}
	return false
}

// parseNodeVersion splits "v22.11.0" into its numeric parts.
func parseNodeVersion(version string) (int, int, int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	numbers := make([]int, 0, 3)
	for _, part := range parts[:min(len(parts), 3)] {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return 0, 0, 0, false
		}
		numbers = append(numbers, number)
	}
	for len(numbers) < 3 {
		numbers = append(numbers, 0)
	}
	return numbers[0], numbers[1], numbers[2], true
}

// downloadNodeRelease fetches, verifies and unpacks one release into
// ManagedNodeDir, replacing any previous copy only once the new one is complete.
func downloadNodeRelease(ctx context.Context, base, version, osName, arch string) error {
	ext := nodeArchiveExt()
	file := fmt.Sprintf("node-%s-%s-%s%s", version, osName, arch, ext)
	url := base + "/" + version + "/" + file

	home := ManagedNodeHome()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", home, err)
	}
	// Staging inside the destination's parent keeps the final step a rename on
	// the same filesystem, and leaves nothing behind when this fails.
	staging, err := os.MkdirTemp(home, ".node-staging-")
	if err != nil {
		return fmt.Errorf("could not create a staging directory in %s: %w", home, err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	archivePath := filepath.Join(staging, file)
	digest, err := downloadToFile(ctx, url, archivePath)
	if err != nil {
		return err
	}
	// A checksum that cannot be fetched does not block the install (the same
	// rule the one-line installer follows); a checksum that does not match
	// always does.
	if expected, err := fetchNodeChecksum(ctx, base+"/"+version+"/SHASUMS256.txt", file); err == nil && expected != "" {
		if !strings.EqualFold(expected, digest) {
			return fmt.Errorf("%s: checksum mismatch, the download is incomplete", file)
		}
	}

	unpacked := filepath.Join(staging, "unpacked")
	if err := unpackNodeArchive(archivePath, unpacked); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	inner := filepath.Join(unpacked, strings.TrimSuffix(file, ext))
	if info, err := os.Stat(inner); err != nil || !info.IsDir() {
		return fmt.Errorf("%s: the archive does not contain %s", file, filepath.Base(inner))
	}

	target := ManagedNodeDir()
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("could not replace the previous Node.js at %s: %w", target, err)
	}
	if err := os.Rename(inner, target); err != nil {
		return fmt.Errorf("could not move Node.js into %s: %w", target, err)
	}
	return nil
}

func nodeArchiveExt() string {
	if IsWindows() {
		return ".zip"
	}
	return ".tar.gz"
}

// fetchNodeChecksum returns the expected SHA-256 for file from a SHASUMS256.txt
// body, or "" when that file does not list it.
func fetchNodeChecksum(ctx context.Context, url, file string) (string, error) {
	body, err := httpGetLimited(ctx, url, nodeMaxIndexBytes)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == file {
			return fields[0], nil
		}
	}
	return "", nil
}

// httpGetLimited fetches url and returns at most limit bytes.
func httpGetLimited(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := nodeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return body, nil
}

// downloadToFile streams url into path and returns the SHA-256 of what was
// written.
func downloadToFile(ctx context.Context, url, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := nodeHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}

	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), io.LimitReader(resp.Body, nodeMaxArchiveBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", fmt.Errorf("%s: %w", url, copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > nodeMaxArchiveBytes {
		return "", fmt.Errorf("%s: archive is larger than %d bytes", url, nodeMaxArchiveBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// unpackNodeArchive extracts a .tar.gz or .zip into dest.
func unpackNodeArchive(archivePath, dest string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return unpackZipArchive(archivePath, dest)
	}
	return unpackTarGzArchive(archivePath, dest)
}

func unpackTarGzArchive(archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("archive is not readable: %w", err)
	}
	defer func() { _ = gz.Close() }()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("archive is not readable: %w", err)
		}
		if err := extractTarEntry(reader, header, dest); err != nil {
			return err
		}
	}
}

// extractTarEntry writes one tar member under dest, refusing anything that would
// escape it. The archive comes from a mirror, so entry names are untrusted.
func extractTarEntry(reader io.Reader, header *tar.Header, dest string) error {
	name, err := safeArchivePath(header.Name)
	if err != nil || name == "" {
		return err
	}
	target := filepath.Join(dest, name)
	if !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		return fmt.Errorf("archive entry escapes the install directory: %q", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg:
		return writeArchiveFile(reader, target, os.FileMode(header.Mode&0o777))
	case tar.TypeSymlink:
		return writeArchiveSymlink(target, name, header.Linkname)
	case tar.TypeLink:
		return writeArchiveHardlink(dest, target, header.Linkname)
	default:
		// Devices, fifos and the like have no business in a Node.js release.
		return nil
	}
}

// writeArchiveFile creates one regular file with the archive's own mode. Node's
// bin/ entries must stay executable, so the mode is applied after the copy
// rather than created with it.
func writeArchiveFile(reader io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	return os.Chmod(target, mode)
}

// writeArchiveSymlink recreates a relative symlink (bin/npm →
// ../lib/node_modules/npm/bin/npm-cli.js) after checking that its target stays
// inside the archive tree.
func writeArchiveSymlink(target, name, linkname string) error {
	if linkname == "" || filepath.IsAbs(linkname) {
		return fmt.Errorf("archive symlink has an absolute target: %q -> %q", name, linkname)
	}
	if _, err := safeArchivePath(filepath.Join(filepath.Dir(name), linkname)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Symlink(linkname, target)
}

func writeArchiveHardlink(dest, target, linkname string) error {
	source, err := safeArchivePath(linkname)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Link(filepath.Join(dest, source), target)
}

// unpackZipArchive extracts a Windows release zip into dest.
func unpackZipArchive(archivePath, dest string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("archive is not readable: %w", err)
	}
	defer func() { _ = reader.Close() }()

	for _, entry := range reader.File {
		if err := extractZipEntry(entry, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(entry *zip.File, dest string) error {
	name, err := safeArchivePath(entry.Name)
	if err != nil || name == "" {
		return err
	}
	target := filepath.Join(dest, name)
	if !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		return fmt.Errorf("archive entry escapes the install directory: %q", entry.Name)
	}
	if entry.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}

	opened, err := entry.Open()
	if err != nil {
		return err
	}
	defer func() { _ = opened.Close() }()
	return writeArchiveFile(opened, target, entry.Mode().Perm())
}

// safeArchivePath cleans an archive entry name and rejects absolute paths and
// parent traversal. Returns "" for the archive root itself.
func safeArchivePath(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return "", nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes the install directory: %q", name)
	}
	return clean, nil
}
