package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDrawioDataDir points model.DataDir at a temp dir and clears the
// package-level install state, so one test's "failed" cannot leak into the next.
func withDrawioDataDir(t *testing.T) string {
	t.Helper()
	orig := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		model.DataDir = orig
		resetDrawioState()
		drawioInstaller = installDrawio
		drawioExpectedChecksum = drawioSHA256
	})
	resetDrawioState()
	return model.DataDir
}

// sha256Hex is the digest the installer compares against.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// drawioZip builds an archive the installer can unpack.
func drawioZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestDrawioDownloadURLs_CustomFirstThenMirrorsThenDirect(t *testing.T) {
	orig := model.ConfigInstance.Drawio.DownloadURL
	t.Cleanup(func() { model.ConfigInstance.Drawio.DownloadURL = orig })

	model.ConfigInstance.Drawio.DownloadURL = "https://mirror.internal/draw.war"
	urls := drawioDownloadURLs()

	require.NotEmpty(t, urls)
	assert.Equal(t, "https://mirror.internal/draw.war", urls[0],
		"a configured URL must be tried first — on an air-gapped network it is the only one that works")
	assert.Contains(t, urls[len(urls)-1], "https://github.com/jgraph/drawio/releases/download/",
		"the canonical github URL must stay as the final fallback")
	for _, m := range drawioMirrorPrefixes {
		assert.Contains(t, urls, m+"https://github.com/jgraph/drawio/releases/download/"+drawioVersion+"/"+drawioAsset)
	}

	// Empty setting means no custom entry at all, not an empty-string candidate.
	model.ConfigInstance.Drawio.DownloadURL = "  "
	urls = drawioDownloadURLs()
	assert.NotContains(t, urls, "", "a blank setting must not become a download candidate")
	assert.Contains(t, urls[0], drawioMirrorPrefixes[0])
}

func TestInstallDrawioFrom_FallsBackToTheNextSource(t *testing.T) {
	withDrawioDataDir(t)
	archive := drawioZip(t, map[string]string{
		"index.html":    "<html>drawio</html>",
		"js/app.min.js": "// app",
	})
	drawioExpectedChecksum = sha256Hex(archive)

	var hits []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/bad.war" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	err := installDrawioFrom([]string{srv.URL + "/bad.war", srv.URL + "/good.war"})
	require.NoError(t, err)
	assert.Equal(t, []string{"/bad.war", "/good.war"}, hits, "a failing source must hand over to the next one")
	assert.True(t, DrawioReady())
	assert.FileExists(t, filepath.Join(DrawioInstallDir(), "index.html"))
}

func TestInstallDrawioFrom_RejectsEntriesThatEscapeTheInstallDir(t *testing.T) {
	dataDir := withDrawioDataDir(t)
	archive := drawioZip(t, map[string]string{
		"../escaped.txt": "should never be written",
	})
	drawioExpectedChecksum = sha256Hex(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	err := installDrawioFrom([]string{srv.URL + "/evil.war"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes the install directory")

	// The archive comes from a third-party mirror, so the guard is what stops it
	// writing outside the install dir with the server's privileges.
	assert.NoFileExists(t, filepath.Join(dataDir, "escaped.txt"))
	assert.False(t, DrawioReady())
}

func TestStartDrawioInstall_RefusesASecondCall(t *testing.T) {
	withDrawioDataDir(t)

	release := make(chan struct{})
	drawioInstaller = func() error {
		<-release
		return nil
	}

	require.NoError(t, StartDrawioInstall())
	// The first call owns the install; a second would unpack into the same
	// directory, so it must be refused rather than queued.
	require.Error(t, StartDrawioInstall())

	close(release)
	// Let the goroutine settle so the next test does not inherit its state.
	require.Eventually(t, func() bool {
		drawioMu.Lock()
		defer drawioMu.Unlock()
		return !drawioInstalling
	}, 2*time.Second, 10*time.Millisecond)
}

func TestGetDrawioStatus_InstalledOnDiskWinsOverMemoryState(t *testing.T) {
	withDrawioDataDir(t)

	// A server restarted after a failure still has the editor on disk; the
	// in-memory "failed" must not make the UI offer a download again.
	require.NoError(t, os.MkdirAll(DrawioInstallDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(DrawioInstallDir(), "index.html"), []byte("x"), 0o644))
	drawioMu.Lock()
	drawioState = "failed"
	drawioMu.Unlock()

	st := GetDrawioStatus()
	assert.True(t, st.Installed)
	assert.Equal(t, "ready", st.State)
	assert.Equal(t, DrawioBasePath(), st.BasePath)
}

func TestInstallDrawioFrom_RejectsAnArchiveThatFailsVerification(t *testing.T) {
	withDrawioDataDir(t)
	// A well-formed but wrong digest: exactly the shape of a mirror serving a
	// different release, or a truncated transfer.
	drawioExpectedChecksum = strings.Repeat("0", 64)

	archive := drawioZip(t, map[string]string{"index.html": "<html>drawio</html>"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	err := installDrawioFrom([]string{srv.URL + "/swapped.war"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	// The editor must not be usable after a rejected download.
	assert.False(t, DrawioReady())
	assert.NoDirExists(t, DrawioInstallDir())
}

func TestInstallDrawioFrom_TriesTheNextSourceAfterAChecksumMismatch(t *testing.T) {
	withDrawioDataDir(t)
	good := drawioZip(t, map[string]string{"index.html": "<html>ok</html>"})
	swapped := drawioZip(t, map[string]string{"index.html": "<html>swapped</html>"})
	drawioExpectedChecksum = sha256Hex(good)

	var hits []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/swapped.war" {
			_, _ = w.Write(swapped)
			return
		}
		_, _ = w.Write(good)
	}))
	defer srv.Close()

	require.NoError(t, installDrawioFrom([]string{srv.URL + "/swapped.war", srv.URL + "/good.war"}))
	assert.Equal(t, []string{"/swapped.war", "/good.war"}, hits,
		"a mirror that serves the wrong bytes must hand over to the next candidate")
	body, err := os.ReadFile(filepath.Join(DrawioInstallDir(), "index.html"))
	require.NoError(t, err)
	assert.Equal(t, "<html>ok</html>", string(body))
}

func TestDrawioSHA256_IsAWellFormedDigest(t *testing.T) {
	// An empty or placeholder value would silently switch verification off, so
	// the pinned constant itself is guarded here.
	assert.Len(t, drawioSHA256, 64)
	assert.Regexp(t, "^[0-9a-f]{64}$", drawioSHA256)
}

func TestInstallDrawioFromReader_InstallsAVerifiedArchive(t *testing.T) {
	withDrawioDataDir(t)
	archive := drawioZip(t, map[string]string{"index.html": "<html>uploaded</html>"})
	drawioExpectedChecksum = sha256Hex(archive)

	require.NoError(t, InstallDrawioFromReader(bytes.NewReader(archive), int64(len(archive))))

	assert.True(t, DrawioReady())
	body, err := os.ReadFile(filepath.Join(DrawioInstallDir(), "index.html"))
	require.NoError(t, err)
	assert.Equal(t, "<html>uploaded</html>", string(body))
	assert.Equal(t, "ready", GetDrawioStatus().State)
}

func TestInstallDrawioFromReader_RejectsWrongContent(t *testing.T) {
	withDrawioDataDir(t)
	// The offline path is not a way around verification: a hand-supplied file
	// must match the same digest as a downloaded one.
	drawioExpectedChecksum = strings.Repeat("0", 64)
	archive := drawioZip(t, map[string]string{"index.html": "<html>nope</html>"})

	err := InstallDrawioFromReader(bytes.NewReader(archive), int64(len(archive)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.False(t, DrawioReady())

	// The failure is visible to a client that polls afterwards, not just to the
	// caller that made the request.
	st := GetDrawioStatus()
	assert.Equal(t, "failed", st.State)
	assert.Contains(t, st.Error, "checksum mismatch")
}

func TestInstallDrawioFromReader_RefusesWhileAnInstallIsRunning(t *testing.T) {
	withDrawioDataDir(t)
	drawioMu.Lock()
	drawioInstalling = true
	drawioMu.Unlock()

	err := InstallDrawioFromReader(bytes.NewReader(nil), 0)
	assert.ErrorIs(t, err, ErrDrawioInstallBusy)
}

func TestSettingsDescriptionPointsAtThePinnedRelease(t *testing.T) {
	// The settings panel tells the user where to fetch the archive by hand, and
	// that URL embeds the version. Nothing else would notice it going stale after
	// a version bump — the user would download the old file and then hit a
	// checksum error with no idea why.
	want := "releases/download/" + drawioVersion + "/" + drawioAsset
	for _, name := range []string{"zh.ts", "en.ts"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "i18n", "locales", name))
		require.NoError(t, err)
		assert.Containsf(t, string(body), want, "%s must point at the pinned release", name)
	}
}

func TestUnpackDrawio_RemovesCopiesFromPreviousVersions(t *testing.T) {
	withDrawioDataDir(t)

	// A previous release's editor and a directory that is not ours at all.
	root := filepath.Join(model.DataDir, "vendor", "drawio")
	oldVersion := filepath.Join(root, "v0.0.1")
	notOurs := filepath.Join(root, "notes")
	require.NoError(t, os.MkdirAll(oldVersion, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldVersion, "index.html"), []byte("old"), 0o644))
	require.NoError(t, os.MkdirAll(notOurs, 0o755))

	archive := drawioZip(t, map[string]string{"index.html": "<html>new</html>"})
	drawioExpectedChecksum = sha256Hex(archive)
	require.NoError(t, InstallDrawioFromReader(bytes.NewReader(archive), int64(len(archive))))

	// The old 123MB copy is gone; the current one is there.
	assert.NoDirExists(t, oldVersion)
	assert.True(t, DrawioReady())
	// Anything that does not look like a release directory is left alone.
	assert.DirExists(t, notOurs)
}

func TestPruneOldDrawioVersions_KeepsTheCurrentOne(t *testing.T) {
	withDrawioDataDir(t)
	require.NoError(t, os.MkdirAll(DrawioInstallDir(), 0o755))

	pruneOldDrawioVersions()

	assert.DirExists(t, DrawioInstallDir())
}

func TestDrawioVersion_IsExposedForTheClient(t *testing.T) {
	// The version is part of the served path (browsers cache app.min.js by
	// name), so what the client is told and what the installer pins must agree.
	assert.Equal(t, drawioVersion, DrawioVersion())
	assert.Contains(t, DrawioBasePath(), drawioVersion)
}

func TestInstallDrawioFrom_NoCandidatesIsAnError(t *testing.T) {
	withDrawioDataDir(t)

	err := installDrawioFrom(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no download source configured")
}

func TestDownloadDrawioArchive_UnparseableURLIsReported(t *testing.T) {
	// A hand-written drawio.download_url can be malformed; it must fail that
	// candidate instead of panicking inside the HTTP client.
	_, err := downloadDrawioArchive("://not a url")
	require.Error(t, err)
}

func TestDownloadDrawioArchive_TransportErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/draw.war"
	srv.Close() // nothing is listening any more

	_, err := downloadDrawioArchive(url)
	require.Error(t, err)
}

func TestStartDrawioInstall_RecordsTheFailure(t *testing.T) {
	withDrawioDataDir(t)
	drawioInstaller = func() error { return errors.New("mirror down") }

	require.NoError(t, StartDrawioInstall())
	require.Eventually(t, func() bool {
		drawioMu.Lock()
		defer drawioMu.Unlock()
		return !drawioInstalling
	}, 2*time.Second, 10*time.Millisecond)

	// The error is kept, not cleared: a client that polls after the fact still
	// has to learn why nothing was installed.
	st := GetDrawioStatus()
	assert.Equal(t, drawioStateFailed, st.State)
	assert.Contains(t, st.Error, "mirror down")
}

// failingWriter is an io.Writer that always fails, for the hashing copy path.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStreamAndVerifyArchive_WriteErrorIsReported(t *testing.T) {
	boom := errors.New("disk full")

	err := streamAndVerifyArchive(failingWriter{err: boom}, strings.NewReader("archive"), 7)
	require.ErrorIs(t, err, boom)
}

func TestUnpackDrawio_NotAnArchive(t *testing.T) {
	withDrawioDataDir(t)
	path := filepath.Join(t.TempDir(), "junk.war")
	require.NoError(t, os.WriteFile(path, []byte("not a zip"), 0o600))

	err := unpackDrawio(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not readable")
	assert.False(t, DrawioReady())
}

func TestUnpackDrawio_RejectsAnOversizedEntry(t *testing.T) {
	withDrawioDataDir(t)
	orig := drawioMaxEntryBytes
	drawioMaxEntryBytes = 8
	t.Cleanup(func() { drawioMaxEntryBytes = orig })

	archive := drawioZip(t, map[string]string{"index.html": strings.Repeat("x", 32)})
	path := filepath.Join(t.TempDir(), "big.war")
	require.NoError(t, os.WriteFile(path, archive, 0o600))

	err := unpackDrawio(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
	// A rejected unpack must not leave a half-populated editor behind.
	assert.False(t, DrawioReady())
	assert.NoDirExists(t, DrawioInstallDir())
}

func TestUnpackDrawio_CreatesExplicitDirectoryEntries(t *testing.T) {
	withDrawioDataDir(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create("assets/")
	require.NoError(t, err)
	w, err := zw.Create("assets/icon.svg")
	require.NoError(t, err)
	_, err = w.Write([]byte("<svg/>"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	path := filepath.Join(t.TempDir(), "draw.war")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	require.NoError(t, unpackDrawio(path))
	assert.FileExists(t, filepath.Join(DrawioInstallDir(), "assets", "icon.svg"))
}

func TestDownloadDrawioArchive_AcceptsASelfSignedMirror(t *testing.T) {
	withDrawioDataDir(t)
	archive := drawioZip(t, map[string]string{"index.html": "<html>mirror</html>"})
	drawioExpectedChecksum = sha256Hex(archive)

	// httptest.NewTLSServer serves a certificate no client can verify, which is
	// exactly what an internal mirror looks like. The download must still work:
	// integrity comes from the pinned hash, not from the transport.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	path, err := downloadDrawioArchive(srv.URL + "/draw.war")
	require.NoError(t, err)
	defer func() { _ = os.Remove(path) }()

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, archive, body)
}

func TestDownloadDrawioArchive_AcceptsAPlainHTTPSource(t *testing.T) {
	withDrawioDataDir(t)
	archive := drawioZip(t, map[string]string{"index.html": "<html>lan</html>"})
	drawioExpectedChecksum = sha256Hex(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	path, err := downloadDrawioArchive(srv.URL + "/draw.war")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
}
