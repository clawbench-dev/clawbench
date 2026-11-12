package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

// The draw.io editor is NOT embedded in the binary: the official webapp is
// ~118MB unpacked, which would triple the install size for a feature most users
// never open. Instead the server downloads it once on first use, unpacks it
// under <DataDir>/vendor/drawio/<version>/ and serves it from there. One
// download serves every device (phone, tablet, desktop, browser) because the
// files live on the server, not in the client.
//
// It must NOT go under the frontend build dir (.clawbench-web/): build.sh copies
// that whole directory into internal/frontend/dist and go:embed bakes it into
// the binary — a downloaded editor sitting there would be embedded on the next
// build, defeating the entire point.
const (
	// drawioVersion is the release the download URLs point at. The version is
	// part of the served path (/vendor/drawio/<version>/...) on purpose: drawio
	// ships fixed filenames (app.min.js), so a version-less path would let
	// browsers keep running the previous editor from cache after an upgrade.
	drawioVersion = "v32.6.10"
	// drawioAsset is the official release archive. It is a plain zip (a WAR is
	// a zip) holding the static webapp plus a Java backend we never run.
	drawioAsset       = "draw.war"
	drawioReleaseRepo = "jgraph/drawio"
	// drawioEntry is the file whose presence means "installed".
	drawioEntry = "index.html"
	// drawioSHA256 is the SHA-256 of the exact archive above.
	//
	// drawio publishes no checksum file, so this value was computed locally from
	// the release asset and cross-checked against two independent mirrors
	// (gh-proxy.com and ghfast.top), which agreed byte for byte. It therefore
	// LOCKS the install to those bytes rather than PROVING they are upstream's:
	// a first download that was already substituted cannot be detected. What it
	// does catch is everything that happens afterwards — a mirror caching a
	// different release, a truncated transfer, a swapped archive.
	//
	// It changes whenever drawioVersion changes; update both together.
	drawioSHA256 = "a57b06b3b6a876a351dacfe65d027d9509319cd030e9bacf6ae04a034d66a38f"

	// drawioStateFailed is the terminal state of a failed install. Named (and
	// deliberately separate from the live-notes status constants) because the
	// same literal appears in more than one service file.
	drawioStateFailed = "failed"
)

// drawioMaxEntryBytes caps one extracted archive member. The archive is
// checksum-verified, but the unpack step must still not be able to write an
// unbounded file from a reader (a repacked archive, or a checksum variable
// pointed at a test fixture).
//
// A variable so the size-limit test can lower it instead of allocating 512MB.
var drawioMaxEntryBytes int64 = 512 << 20

// drawioMirrorPrefixes are prefix proxies prepended to the canonical GitHub
// URL. Same list and same reasoning as githubReleaseMirrors in
// desktop_upgrade.go: community-run, no uptime guarantee, so they are only
// candidates and the direct URL stays as the final fallback.
var drawioMirrorPrefixes = []string{
	"https://gh-proxy.com/",
	"https://ghfast.top/",
}

// drawioDownloadTimeout bounds ONE candidate attempt. 48MB in 10 minutes is a
// floor of ~80 KB/s, so a mirror that is alive but useless fails here instead of
// holding the install forever — the walk only advances on failure.
const drawioDownloadTimeout = 10 * time.Minute

// DrawioStatus is the polling payload of GET /api/drawio/status.
type DrawioStatus struct {
	Installed bool `json:"installed"`
	// State is "idle", "downloading", "installing" or "failed". "failed" keeps
	// the error until the next attempt, so a client that polls after the fact
	// still learns why nothing happened.
	State      string `json:"state"`
	Version    string `json:"version"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error"`
	// BasePath is where the editor is served from once installed, e.g.
	// "/vendor/drawio/v32.6.10/". Empty until installed.
	BasePath string `json:"basePath"`
}

var (
	drawioMu         sync.Mutex
	drawioInstalling bool
	drawioState      = "idle"
	drawioDownloaded int64
	drawioTotal      int64
	drawioError      string
	// drawioHTTPClient is a variable so tests can substitute a client backed by
	// an in-memory server without touching the network.
	//
	// Certificate verification is OFF on purpose. Every byte is checked against
	// drawioSHA256 before anything is unpacked, so a machine in the middle can
	// only deliver the official archive (harmless) or something else (rejected
	// before it is written). That is what makes an internal mirror with a
	// self-signed certificate usable — the common case on a LAN — and it is
	// equally why a plain http:// source is safe to accept.
	//
	// This trade is only sound while the pinned hash exists: if it is ever
	// removed, verification has to come back with it.
	drawioHTTPClient = &http.Client{
		Timeout: drawioDownloadTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			//nolint:gosec // integrity comes from drawioSHA256, see above
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	// drawioInstaller is the work StartDrawioInstall runs in the background.
	// A variable so the concurrency test can hold it open instead of racing a
	// real download.
	drawioInstaller = installDrawio
	// drawioExpectedChecksum is the value every download is verified against.
	// A variable only so tests can drive both outcomes (match / mismatch)
	// without shipping a fixture whose hash equals the real release.
	drawioExpectedChecksum = drawioSHA256
)

// ErrDrawioInstallBusy is returned when an install (or an upload) is already
// running. The handler maps it to 409.
var ErrDrawioInstallBusy = errors.New("drawio install already in progress")

// DrawioVersion returns the pinned release, for display and for tests.
func DrawioVersion() string { return drawioVersion }

// DrawioInstallDir is where the unpacked editor lives.
func DrawioInstallDir() string {
	return filepath.Join(model.DataDir, "vendor", "drawio", drawioVersion)
}

// DrawioBasePath is the URL prefix the editor is served from.
func DrawioBasePath() string {
	return "/vendor/drawio/" + drawioVersion + "/"
}

// DrawioReady reports whether the editor is already unpacked and usable.
func DrawioReady() bool {
	_, err := os.Stat(filepath.Join(DrawioInstallDir(), drawioEntry))
	return err == nil
}

// GetDrawioStatus reports the current state. Installed wins over the in-memory
// state: a server restarted mid-download reports "idle" again, but a finished
// install is a fact on disk.
func GetDrawioStatus() DrawioStatus {
	st := DrawioStatus{Version: drawioVersion}
	if DrawioReady() {
		st.Installed = true
		st.State = "ready"
		st.BasePath = DrawioBasePath()
		return st
	}

	drawioMu.Lock()
	defer drawioMu.Unlock()
	st.State = drawioState
	st.Downloaded = drawioDownloaded
	st.Total = drawioTotal
	st.Error = drawioError
	return st
}

// StartDrawioInstall kicks off a background download+unpack. It returns as soon
// as the work is scheduled; the client follows GetDrawioStatus for progress.
//
// A second call while one is running is refused rather than queued: the two
// would write the same directory.
func StartDrawioInstall() error {
	drawioMu.Lock()
	if drawioInstalling {
		drawioMu.Unlock()
		return ErrDrawioInstallBusy
	}
	drawioInstalling = true
	drawioState = "downloading"
	drawioDownloaded = 0
	drawioTotal = 0
	drawioError = ""
	drawioMu.Unlock()

	go func() {
		err := drawioInstaller()
		drawioMu.Lock()
		drawioInstalling = false
		if err != nil {
			drawioState = drawioStateFailed
			drawioError = err.Error()
		} else {
			drawioState = "ready"
		}
		drawioMu.Unlock()
	}()
	return nil
}

func setDrawioProgress(downloaded, total int64) {
	drawioMu.Lock()
	drawioDownloaded = downloaded
	drawioTotal = total
	drawioMu.Unlock()
}

func setDrawioState(s string) {
	drawioMu.Lock()
	drawioState = s
	drawioMu.Unlock()
}

// drawioDownloadURLs returns the candidates in the order they are tried: a
// user-supplied URL first (an internal mirror is the only thing that works on an
// air-gapped network), then the prefix proxies, then github.com itself.
func drawioDownloadURLs() []string {
	direct := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
		drawioReleaseRepo, drawioVersion, drawioAsset)

	var urls []string
	if custom := strings.TrimSpace(model.ConfigInstance.Drawio.DownloadURL); custom != "" {
		urls = append(urls, custom)
	}
	for _, m := range drawioMirrorPrefixes {
		urls = append(urls, m+direct)
	}
	return append(urls, direct)
}

// installDrawio downloads the first working candidate and unpacks it into place.
func installDrawio() error {
	return installDrawioFrom(drawioDownloadURLs())
}

// installDrawioFrom tries each candidate in order.
//
// The archive lands in a temp file, is unpacked into a staging directory and is
// only renamed into the final location once complete — a failed or interrupted
// attempt never leaves a half-populated editor that looks installed.
func installDrawioFrom(urls []string) error {
	var lastErr error
	for _, u := range urls {
		tmp, err := downloadDrawioArchive(u)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", u, err)
			continue
		}
		setDrawioState("installing")
		err = unpackDrawio(tmp)
		_ = os.Remove(tmp)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", u, err)
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("no download source configured")
	}
	return fmt.Errorf("draw.io editor could not be installed: %w", lastErr)
}

// downloadDrawioArchive streams one candidate to a temp file, reporting progress
// as it goes, and returns the file path.
func downloadDrawioArchive(url string) (string, error) {
	// A request with a context (rather than Get) so a cancelled install does not
	// keep the connection open until the client's own timeout fires.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := drawioHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "clawbench-drawio-*.war")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()

	if err := streamAndVerifyArchive(tmp, resp.Body, resp.ContentLength); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// streamAndVerifyArchive copies the archive into dst while hashing it, then
// checks the digest.
//
// Hashing while copying matters: a 48MB archive is not worth a second full
// read, and a rejected download must never be left behind for the unpack step.
func streamAndVerifyArchive(dst io.Writer, src io.Reader, total int64) error {
	digest := sha256.New()
	sink := io.MultiWriter(dst, digest)

	var written int64
	buf := make([]byte, 256*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, werr := sink.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			setDrawioProgress(written, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if want := drawioExpectedChecksum; want != "" && !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// InstallDrawioFromReader installs from an archive the user supplied — the
// offline path, for a server that cannot reach any download source.
//
// It runs synchronously: the bytes have already been sent by the time it is
// called, so the caller is only waiting for verification and unpacking. The
// same checksum applies, so a hand-supplied file cannot smuggle in different
// content either.
func InstallDrawioFromReader(r io.Reader, total int64) error {
	drawioMu.Lock()
	if drawioInstalling {
		drawioMu.Unlock()
		return ErrDrawioInstallBusy
	}
	drawioInstalling = true
	drawioState = "installing"
	drawioDownloaded = 0
	drawioTotal = total
	drawioError = ""
	drawioMu.Unlock()

	err := installFromReader(r, total)

	drawioMu.Lock()
	drawioInstalling = false
	if err != nil {
		drawioState = drawioStateFailed
		drawioError = err.Error()
	} else {
		drawioState = "ready"
	}
	drawioMu.Unlock()
	return err
}

func installFromReader(r io.Reader, total int64) error {
	tmp, err := os.CreateTemp("", "clawbench-drawio-*.war")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := streamAndVerifyArchive(tmp, r, total); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return unpackDrawio(tmpPath)
}

// unpackDrawio extracts the archive into a staging directory and swaps it into
// the install location.
func unpackDrawio(archivePath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("archive is not readable: %w", err)
	}
	defer func() { _ = zr.Close() }()

	dest := DrawioInstallDir()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	staging := dest + ".staging"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}

	for _, f := range zr.File {
		if err := extractDrawioEntry(f, staging); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}

	// Replace any prior copy of this version. The rename is the only step that
	// makes the install visible, so a crash before it leaves the old (or no)
	// copy intact rather than a mixture.
	_ = os.RemoveAll(dest)
	if err := os.Rename(staging, dest); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}

	pruneOldDrawioVersions()
	return nil
}

// pruneOldDrawioVersions removes editor copies left behind by previous releases.
//
// The version is part of the path, so an app upgrade installs a fresh copy and
// never touches the old one — 123MB per version, with nothing that would ever
// read it again. Safe to do right after a successful unpack: the server serves
// only the pinned version, and an app upgrade has already restarted it, so no
// live page is still loading the previous one.
//
// Best effort on purpose: a permission error here must not turn a successful
// install into a failed one.
func pruneOldDrawioVersions() {
	root := filepath.Join(model.DataDir, "vendor", "drawio")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Only release directories ("v32.6.10"). Anything else — a staging
		// directory left by an interrupted unpack, or a directory someone put
		// there by hand — is not ours to delete.
		if !e.IsDir() || e.Name() == drawioVersion || !strings.HasPrefix(e.Name(), "v") {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// extractDrawioEntry writes one archive member under root, refusing anything
// that would escape it.
//
// The archive comes from a third-party mirror, so entry names are untrusted: a
// name like "../../.ssh/authorized_keys" would otherwise write outside the
// install directory with the server's privileges.
func extractDrawioEntry(f *zip.File, root string) error {
	clean := filepath.Clean(f.Name)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("archive entry escapes the install directory: %q", f.Name)
	}
	target := filepath.Join(root, clean)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return fmt.Errorf("archive entry escapes the install directory: %q", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// CopyN with an explicit ceiling, not Copy: the reader is an archive member
	// whose declared size can lie, and a decompression bomb would fill the disk.
	// limit+1 bytes written means the member exceeded the cap.
	if _, err := io.CopyN(out, rc, drawioMaxEntryBytes+1); err != nil && !errors.Is(err, io.EOF) {
		_ = out.Close()
		return err
	}
	if info, statErr := out.Stat(); statErr == nil && info.Size() > drawioMaxEntryBytes {
		_ = out.Close()
		return fmt.Errorf("archive entry %q is larger than %d bytes", f.Name, drawioMaxEntryBytes)
	}
	return out.Close()
}

// resetDrawioState clears the in-memory install state. Test-only helper: the
// state is package-level so one test's "failed" would otherwise leak into the
// next.
func resetDrawioState() {
	drawioMu.Lock()
	defer drawioMu.Unlock()
	drawioInstalling = false
	drawioState = "idle"
	drawioDownloaded = 0
	drawioTotal = 0
	drawioError = ""
}
