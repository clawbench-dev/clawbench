package platform

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestManagedNodeHome_PrefersClawbenchHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	if got := ManagedNodeHome(); got != home {
		t.Fatalf("ManagedNodeHome() = %q, want %q", got, home)
	}
	if got, want := ManagedNodeDir(), filepath.Join(home, "node"); got != want {
		t.Fatalf("ManagedNodeDir() = %q, want %q", got, want)
	}
}

func TestManagedNodeHome_UsesDataDirEnvironment(t *testing.T) {
	if IsWindows() {
		t.Skip("Windows uses LOCALAPPDATA, not XDG_DATA_HOME")
	}
	data := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", "")
	t.Setenv("XDG_DATA_HOME", data)
	if got, want := ManagedNodeHome(), filepath.Join(data, "clawbench"); got != want {
		t.Fatalf("ManagedNodeHome() = %q, want %q", got, want)
	}
}

func TestPickLatestLTS(t *testing.T) {
	releases := []nodeRelease{
		{Version: "v24.1.0", LTS: false},   // not LTS yet
		{Version: "v22.9.0", LTS: "Jod"},   // older minor of the newest LTS line
		{Version: "v22.11.0", LTS: "Jod"},  // newest LTS
		{Version: "v20.19.0", LTS: "Iron"}, // older LTS line
		{Version: "v18.20.0", LTS: "Hydrogen"},
	}
	got, err := pickLatestLTS(releases)
	if err != nil {
		t.Fatalf("pickLatestLTS: %v", err)
	}
	if got != "v22.11.0" {
		t.Fatalf("pickLatestLTS() = %q, want v22.11.0", got)
	}
}

func TestPickLatestLTS_IgnoresReleasesBelowTheMinimum(t *testing.T) {
	releases := []nodeRelease{
		{Version: "v18.20.0", LTS: "Hydrogen"},
		{Version: "v16.20.0", LTS: "Gallium"},
	}
	if _, err := pickLatestLTS(releases); err == nil {
		t.Fatal("expected an error when every LTS release is older than the minimum")
	}
}

func TestParseNodeVersion(t *testing.T) {
	major, minor, patch, ok := parseNodeVersion("v22.11.1")
	if !ok || major != 22 || minor != 11 || patch != 1 {
		t.Fatalf("parseNodeVersion(v22.11.1) = %d.%d.%d,%v", major, minor, patch, ok)
	}
	if _, _, _, ok := parseNodeVersion("not-a-version"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestUnpackTarGzArchive_KeepsExecutableBitAndSymlinks(t *testing.T) {
	top := "node-v22.11.0-linux-x64"
	archive := buildTarGz(t, map[string]tarEntry{
		top + "/bin/node":     {body: "#!/bin/sh\necho v22.11.0\n", mode: 0o755},
		top + "/bin/npm":      {body: "#!/bin/sh\nexit 0\n", mode: 0o755},
		top + "/LICENSE":      {body: "license", mode: 0o644},
		top + "/bin/npm-link": {symlink: "../lib/npm-cli.js"},
	})

	dest := t.TempDir()
	if err := unpackNodeArchive(writeTemp(t, "node.tar.gz", archive), dest); err != nil {
		t.Fatalf("unpackNodeArchive: %v", err)
	}

	node := filepath.Join(dest, top, "bin", "node")
	info, err := os.Stat(node)
	if err != nil {
		t.Fatalf("node binary missing: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("node binary lost its executable bit: %v", info.Mode())
	}
	link := filepath.Join(dest, top, "bin", "npm-link")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("symlink missing: %v", err)
	}
	if target != "../lib/npm-cli.js" {
		t.Fatalf("symlink target = %q", target)
	}
}

func TestUnpackTarGzArchive_RejectsTraversal(t *testing.T) {
	archive := buildTarGz(t, map[string]tarEntry{
		"../../etc/evil": {body: "pwned", mode: 0o644},
	})
	err := unpackNodeArchive(writeTemp(t, "node.tar.gz", archive), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "escapes the install directory") {
		t.Fatalf("expected a traversal error, got %v", err)
	}
}

func TestUnpackTarGzArchive_RejectsAbsoluteSymlink(t *testing.T) {
	archive := buildTarGz(t, map[string]tarEntry{
		"node/bin/node": {symlink: "/etc/passwd"},
	})
	err := unpackNodeArchive(writeTemp(t, "node.tar.gz", archive), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "absolute target") {
		t.Fatalf("expected an absolute-symlink error, got %v", err)
	}
}

func TestUnpackTarGzArchive_HandlesDirsHardlinksAndModeZero(t *testing.T) {
	top := "node-v22.11.0-linux-x64"
	archive := buildTarGz(t, map[string]tarEntry{
		top + "/":          {dir: true},
		top + "/a-license": {body: "license", mode: 0o644},
		top + "/z-copying": {hardlink: top + "/a-license"},
		top + "/README":    {body: "readme", mode: 0},
		top + "/lib/node":  {body: "x", mode: 0o755},
	})

	dest := t.TempDir()
	if err := unpackNodeArchive(writeTemp(t, "node.tar.gz", archive), dest); err != nil {
		t.Fatalf("unpackNodeArchive: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dest, top, "z-copying"))
	if err != nil {
		t.Fatalf("hardlink missing: %v", err)
	}
	if string(body) != "license" {
		t.Fatalf("hardlink content = %q", body)
	}
	if _, err := os.Stat(filepath.Join(dest, top, "README")); err != nil {
		t.Fatalf("mode-less file missing: %v", err)
	}
}

func TestUnpackNodeArchive_RejectsUnreadableArchives(t *testing.T) {
	dest := t.TempDir()
	if err := unpackNodeArchive(writeTemp(t, "bad.tar.gz", []byte("not gzip")), dest); err == nil {
		t.Fatal("expected an error for a corrupt tar.gz")
	}
	if err := unpackNodeArchive(writeTemp(t, "bad.zip", []byte("not a zip")), dest); err == nil {
		t.Fatal("expected an error for a corrupt zip")
	}
}

func TestUnpackZipArchive_HandlesDirectoryEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("node-v22.11.0-win-x64/lib/"); err != nil {
		t.Fatal(err)
	}
	writer, err := zw.Create("node-v22.11.0-win-x64/lib/tool.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := unpackNodeArchive(writeTemp(t, "node.zip", buf.Bytes()), dest); err != nil {
		t.Fatalf("unpackNodeArchive: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dest, "node-v22.11.0-win-x64", "lib")); err != nil || !info.IsDir() {
		t.Fatalf("directory entry missing: %v", err)
	}
}

// Entries a Node.js release never contains (devices, fifos) are skipped rather
// than failing the unpack.
func TestUnpackTarGzArchive_SkipsUnsupportedEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "node/pipe", Typeflag: tar.TypeFifo}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if err := unpackNodeArchive(writeTemp(t, "node.tar.gz", buf.Bytes()), t.TempDir()); err != nil {
		t.Fatalf("an unsupported entry must be skipped, got %v", err)
	}
}

func TestFetchLatestNodeLTS_ReportsUnusableIndexes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	defer server.Close()
	if _, err := fetchLatestNodeLTS(context.Background(), server.URL); err == nil {
		t.Fatal("expected an error for an unparseable index.json")
	}
	if _, err := fetchLatestNodeLTS(context.Background(), "http://bad host/"); err == nil {
		t.Fatal("expected an error for an unusable URL")
	}
}

func TestFetchNodeChecksum_UnlistedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 64) + "  node-v20.0.0-linux-x64.tar.gz\n"))
	}))
	defer server.Close()

	got, err := fetchNodeChecksum(context.Background(), server.URL, "node-v22.11.0-linux-x64.tar.gz")
	if err != nil {
		t.Fatalf("fetchNodeChecksum: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty for a file the list does not mention", got)
	}
}

func TestUnpackZipArchive(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writer, err := zw.Create("node-v22.11.0-win-x64/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("MZ fake")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := unpackNodeArchive(writeTemp(t, "node.zip", buf.Bytes()), dest); err != nil {
		t.Fatalf("unpackNodeArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "node-v22.11.0-win-x64", "node.exe")); err != nil {
		t.Fatalf("node.exe missing: %v", err)
	}
}

func TestPrependManagedNodePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	bin := managedNodeBinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, nodeExecutableName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	original := "/usr/bin:/bin"
	t.Setenv("PATH", original)
	if !PrependManagedNodePath() {
		t.Fatal("expected PATH to change")
	}
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, bin+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want it to start with %q", got, bin)
	}
	if PrependManagedNodePath() {
		t.Fatal("a second call must be a no-op")
	}
}

func TestPrependManagedNodePath_NoRuntimeInstalled(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	if PrependManagedNodePath() {
		t.Fatal("PATH must not change when there is no private Node.js")
	}
}

// The whole point of the feature: a machine with no Node.js gets one, and the
// caller's PATH is updated so the agent install that follows can use it.
func TestEnsureNodeRuntime_InstallsAndActivates(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	server := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	note, err := EnsureNodeRuntime()
	if err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
	if !strings.Contains(note, "v22.11.0") {
		t.Fatalf("note = %q, want it to name the version", note)
	}
	if _, err := os.Stat(filepath.Join(ManagedNodeDir(), "bin", "node")); err != nil {
		t.Fatalf("node was not installed: %v", err)
	}
	if major := nodeMajorVersion(); major != 22 {
		t.Fatalf("node on PATH reports major %d, want 22", major)
	}
}

// The mirror is chosen by region, so the other one is only a fallback — and it
// has to actually be tried when the first source is unreachable.
func TestEnsureNodeRuntime_FallsBackToTheSecondSource(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer dead.Close()
	alive := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer alive.Close()
	restore := stubNodeDownloadBases(t, []string{dead.URL, alive.URL})
	defer restore()

	if _, err := EnsureNodeRuntime(); err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
	if major := nodeMajorVersion(); major != 22 {
		t.Fatalf("node on PATH reports major %d, want 22", major)
	}
}

func TestEnsureNodeRuntime_ReportsArchiveDownloadFailure(t *testing.T) {
	file, _ := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_, _ = w.Write([]byte(`[{"version":"v22.11.0","lts":"Jod"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	_, err := EnsureNodeRuntime()
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("expected the archive failure to name %s, got %v", file, err)
	}
}

// A runtime that unpacks but cannot execute is a different failure from a
// download failure, and the message must say so: the fix is a system library,
// not the network.
func TestEnsureNodeRuntime_RejectsRuntimeThatDoesNotRun(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho not-a-version\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	server := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	_, err := EnsureNodeRuntime()
	if err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("expected a runtime error, got %v", err)
	}
}

// A private copy left by the one-line installer is reused instead of downloaded
// again — the download sources must not even be consulted.
func TestEnsureNodeRuntime_ReusesAnExistingPrivateRuntime(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")
	writeFakeNode(t, managedNodeBinDir(), "#!/bin/sh\necho v22.11.0\n")
	restore := stubNodeDownloadBases(t, nil)
	defer restore()

	note, err := EnsureNodeRuntime()
	if err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
	if note != "" {
		t.Fatalf("note = %q, want empty when the existing copy was reused", note)
	}
	if major := nodeMajorVersion(); major != 22 {
		t.Fatalf("node on PATH reports major %d, want 22", major)
	}
}

func TestEnsureNodeRuntime_ChecksumMismatchFails(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	server := serveNodeRelease(t, file, archive, strings.Repeat("0", 64))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	_, err := EnsureNodeRuntime()
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum error, got %v", err)
	}
}

// A mirror that does not publish SHASUMS256.txt must not block the install: the
// one-line installer makes the same trade-off.
func TestEnsureNodeRuntime_AcceptsMissingChecksums(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")

	server := serveNodeRelease(t, file, archive, "")
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	if _, err := EnsureNodeRuntime(); err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
}

// A machine that already has a usable Node must not download anything: the
// download sources are made to fail loudly if they are touched.
func TestEnsureNodeRuntime_NoopWhenNodeIsUsable(t *testing.T) {
	if IsWindows() {
		t.Skip("the fake runtime below is a POSIX shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho v22.11.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", bin)
	restore := stubNodeDownloadBases(t, nil)
	defer restore()

	note, err := EnsureNodeRuntime()
	if err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
	if note != "" {
		t.Fatalf("note = %q, want empty when nothing was installed", note)
	}
}

// prependManagedNodePath must look at the real environment rather than the
// shell-only capture: the agent CLIs are spawned from the server process whose
// PATH was set at start-up, so a directory the login shell knows about is not
// "found" unless it is already in os.Getenv("PATH"). Both spellings of the same
// directory (with and without a trailing separator) count as present.
func TestPrependManagedNodePath_UsesTheProcessEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	bin := managedNodeBinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, nodeExecutableName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Already on PATH with the same spelling: nothing changes, so a caller does
	// not get a false "added" and the PATH does not grow on every restart.
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+bin)
	before := os.Getenv("PATH")
	if PrependManagedNodePath() {
		t.Fatalf("PATH must not change when %q is already on it", bin)
	}
	if got := os.Getenv("PATH"); got != before {
		t.Fatalf("PATH = %q, want it untouched (%q)", got, before)
	}

	// Not on PATH: it goes first so the private runtime wins over a system Node
	// that is too old.
	t.Setenv("PATH", "/usr/bin")
	if !PrependManagedNodePath() {
		t.Fatal("expected PATH to change when the private runtime is missing from it")
	}
	got := os.Getenv("PATH")
	want := bin + string(os.PathListSeparator) + "/usr/bin"
	if got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
}

// An empty CLAWBENCH_HOME must not be read as "the current directory": that
// would put a Node.js download into whatever directory the server started in.
func TestManagedNodeHome_IgnoresAnEmptyOrBlankOverride(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", "   ")
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got := ManagedNodeHome()
	if IsWindows() {
		if !filepath.IsAbs(got) {
			t.Fatalf("ManagedNodeHome() = %q, want an absolute path", got)
		}
		return
	}
	want := filepath.Join(os.Getenv("XDG_DATA_HOME"), "clawbench")
	if got != want {
		t.Fatalf("ManagedNodeHome() = %q, want %q", got, want)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

type tarEntry struct {
	body     string
	mode     int64
	symlink  string
	hardlink string
	dir      bool
}

func buildTarGz(t *testing.T, entries map[string]tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Map iteration order is random, so walk the sorted keys to keep the archive
	// (and any failure) reproducible.
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := entries[name]
		switch {
		case entry.dir:
			if err := tw.WriteHeader(&tar.Header{
				Name: name, Typeflag: tar.TypeDir, Mode: 0o755,
			}); err != nil {
				t.Fatal(err)
			}
		case entry.symlink != "":
			if err := tw.WriteHeader(&tar.Header{
				Name: name, Typeflag: tar.TypeSymlink, Linkname: entry.symlink, Mode: 0o777,
			}); err != nil {
				t.Fatal(err)
			}
		case entry.hardlink != "":
			if err := tw.WriteHeader(&tar.Header{
				Name: name, Typeflag: tar.TypeLink, Linkname: entry.hardlink, Mode: 0o644,
			}); err != nil {
				t.Fatal(err)
			}
		default:
			if err := tw.WriteHeader(&tar.Header{
				Name: name, Typeflag: tar.TypeReg, Mode: entry.mode, Size: int64(len(entry.body)),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checksumOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// fakeNodeRelease builds a release archive for this machine whose node is a
// shell script, so the whole download/verify/unpack/activate path can run
// without the real ~30MB archive.
func fakeNodeRelease(t *testing.T, nodeScript string) (string, []byte) {
	t.Helper()
	if IsWindows() {
		t.Skip("the fake runtime is a POSIX shell script")
	}
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	file := fmt.Sprintf("node-v22.11.0-%s-%s.tar.gz", osName, arch)
	top := strings.TrimSuffix(file, ".tar.gz")
	archive := buildTarGz(t, map[string]tarEntry{
		top + "/bin/node": {body: nodeScript, mode: 0o755},
		top + "/bin/npm":  {body: "#!/bin/sh\nexit 0\n", mode: 0o755},
	})
	return file, archive
}

// serveNodeRelease serves index.json plus one release. An empty checksum means
// the server does not publish SHASUMS256.txt at all.
func serveNodeRelease(t *testing.T, file string, archive []byte, checksum string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = w.Write([]byte(`[{"version":"v22.11.0","lts":"Jod"}]`))
		case "/v22.11.0/" + file:
			_, _ = w.Write(archive)
		case "/v22.11.0/SHASUMS256.txt":
			if checksum == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, file)
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeFakeNode(t *testing.T, dir, script string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeTemp(t *testing.T, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubNodeDownloadBases(t *testing.T, bases []string) func() {
	t.Helper()
	prev := nodeDownloadBases
	nodeDownloadBases = func() []string {
		if bases == nil {
			t.Error("the download sources must not be consulted when Node is already usable")
			return nil
		}
		return bases
	}
	return func() { nodeDownloadBases = prev }
}
