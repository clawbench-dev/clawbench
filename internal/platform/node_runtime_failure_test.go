package platform

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Failure paths of the private-Node installer that the happy-path tests never
// reach: a download that dies mid-body, an install directory that cannot be
// created, the second (fallback) source succeeding after the first one failed.
// Each of these is a real user-facing outcome — an error message that names the
// right cause, or "it worked on the retry" — so they are asserted on the
// message and the resulting directory state, not just on "err != nil".

// ── HTTP layer ─────────────────────────────────────────────────────────────

// A body that dies mid-transfer must be reported as a copy error naming the URL.
// The truncated file is what a flaky link produces; silently returning a short
// body would unpack a corrupt archive.
func TestDownloadToFile_TruncatedBodyIsACopyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A Content-Length longer than what is written makes the client see an
		// unexpected EOF while copying, i.e. copyErr, not a short-but-clean body.
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("only the first bytes"))
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "archive")
	_, err := downloadToFile(t.Context(), srv.URL+"/node.tar.gz", target)
	if err == nil {
		t.Fatal("a truncated download must be an error, not a short archive")
	}
	if !strings.Contains(err.Error(), srv.URL+"/node.tar.gz") {
		t.Fatalf("the error must name the URL that failed, got %v", err)
	}
}

// A 4xx/5xx must be reported with its status, not as an empty body: index-less
// or not-found archives are ordinary mirror misconfigurations, and "HTTP 503"
// tells the operator more than "unexpected EOF".
func TestDownloadToFile_Non200ReportsTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := downloadToFile(t.Context(), srv.URL+"/x.tar.gz", filepath.Join(t.TempDir(), "x"))
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("expected the status code in the error, got %v", err)
	}
}

// downloadToFile distinguishes the three ways a download can go wrong: the file
// it writes cannot be created, the transfer dies mid-body (copyErr), and the
// file cannot be closed (closeErr). The first two are asserted above; the third
// (a full disk at fsync time) is not reachable without fault injection and is
// covered only by the closeErr branch itself.
//
// A download that exceeds nodeMaxArchiveBytes is refused: the ceiling exists so a
// misbehaving mirror cannot fill the disk, and the check has to happen AFTER the
// bytes were streamed (that is when the size is known), so the error names the
// URL and the limit rather than surfacing as a truncated archive later.
func TestDownloadToFile_OversizedBodyIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<20)
		for i := 0; i <= nodeMaxArchiveBytes>>20; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	url := srv.URL + "/huge.tar.gz"
	_, err := downloadToFile(t.Context(), url, filepath.Join(t.TempDir(), "huge"))
	if err == nil {
		t.Fatal("a body over the ceiling must be refused")
	}
	if !strings.Contains(err.Error(), "larger than") || !strings.Contains(err.Error(), url) {
		t.Fatalf("the error must name the URL and the limit, got %v", err)
	}
}

// A tar member that claims a huge body but ends early is a read error mid-entry,
// and the message says so: unpacking "as much as arrived" would install a
// truncated runtime that fails at spawn time instead of install time.
func TestExtractTarEntry_TruncatedBodyFailsUnpack(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "node/bin/node", Typeflag: tar.TypeReg, Mode: 0o755, Size: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("only a few bytes")); err != nil {
		t.Fatal(err)
	}
	_ = gz.Close()

	path := filepath.Join(t.TempDir(), "cut.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	err := unpackTarGzArchive(path, t.TempDir())
	if err == nil {
		t.Fatal("a truncated member must fail the unpack")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("expected the truncated body to be reported, got %v", err)
	}
}

// The read side of index.json / SHASUMS256.txt: a non-200 is an error naming the
// URL instead of the bytes "404 page not found", which json.Unmarshal would then
// report as a confusing syntax error.
func TestHTTPGetLimited_Non200NamesTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	url := srv.URL + "/index.json"
	_, err := httpGetLimited(t.Context(), url, 1024)
	if err == nil || !strings.Contains(err.Error(), url) || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected %q with HTTP 404, got %v", url, err)
	}
}

// A cancelled context surfaces as a transport error from Do; it must not hang or
// pretend to have fetched something.
func TestHTTPGetLimited_CancelledContextIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if body, err := httpGetLimited(ctx, srv.URL, 16); err == nil {
		t.Fatalf("a cancelled request must fail, got body %q", body)
	}
}

// ── installManagedNode fallback ────────────────────────────────────────────

// The first source failing must not abort the install: the second one is tried
// and, when it works, the version it served is what gets reported. This is the
// mainland-China case where npmmirror is down but nodejs.org is reachable (or
// the reverse) — the one-click install still has to work.
func TestInstallManagedNode_FallsBackToTheSecondSource(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer dead.Close()
	working := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer working.Close()

	restore := stubNodeDownloadBasesValue(t, []string{dead.URL, working.URL})
	defer restore()

	version, err := installManagedNode()
	if err != nil {
		t.Fatalf("installManagedNode must fall back to the second source: %v", err)
	}
	if version != "v22.11.0" {
		t.Fatalf("version = %q, want the one the working source served", version)
	}
	if _, err := os.Stat(filepath.Join(ManagedNodeDir(), "bin", "node")); err != nil {
		t.Fatalf("the fallback source's runtime is missing: %v", err)
	}
}

// An empty source list is a programming error, but it must still produce an
// actionable error rather than a nil-dereference or a silent success.
func TestInstallManagedNode_NoSourcesIsAnError(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	restore := stubNodeDownloadBasesValue(t, nil)
	defer restore()

	_, err := installManagedNode()
	if err == nil || !strings.Contains(err.Error(), "no download source") {
		t.Fatalf("expected 'no download source', got %v", err)
	}
}

// ── directory-level failures ───────────────────────────────────────────────

// If the data root cannot be created, the install must say so and name the
// path: the alternative is a download that succeeds and then cannot be stored,
// which reads as "the mirror is broken".
func TestDownloadNodeRelease_UnusableHomeIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture blocks a directory by making it a plain file, which is only enforceable on POSIX")
	}
	parent := t.TempDir()
	// A regular FILE where the data root should be: MkdirAll cannot create it.
	blocked := filepath.Join(parent, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWBENCH_HOME", blocked)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	err := downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", "linux", "x64")
	if err == nil {
		t.Fatal("expected an error when the data root is a regular file")
	}
	if !strings.Contains(err.Error(), blocked) {
		t.Fatalf("the error must name the directory it could not create, got %v", err)
	}
}

// A non-empty Node.js directory that is in the way of the install must not make
// the install fail: staging is a sibling, and the final rename replaces the old
// tree. Anything else would make every reinstall-of-a-running-install awkward.
func TestDownloadNodeRelease_ReplacesANonEmptyTarget(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())

	// A leftover from a failed manual copy: a file where the unpacked top
	// directory needs to be, plus an extra sibling inside node/.
	if err := os.MkdirAll(filepath.Join(ManagedNodeDir(), "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ManagedNodeDir(), "stale"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := serveNodeRelease(t, file, archive, "")
	defer srv.Close()

	if err := downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", osReleaseName(t), archReleaseName(t)); err != nil {
		t.Fatalf("downloadNodeRelease: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ManagedNodeDir(), "stale")); !os.IsNotExist(err) {
		t.Fatal("the previous tree must be replaced, not merged")
	}
	nodes, err := os.ReadDir(ManagedNodeDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := false
	for _, e := range nodes {
		if e.Name() == "bin" && e.IsDir() {
			bin = true
		}
	}
	if !bin {
		t.Fatal("the newly unpacked bin/ directory is missing")
	}
}

// A corrupted (non-gzip) download must be rejected as "not readable" rather than
// unpacking garbage; the message names the file so the operator knows which
// mirror served it.
func TestDownloadNodeRelease_UnreadableArchiveIsReported(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	ext := nodeArchiveExt()
	file := "node-v22.11.0-" + osName + "-" + arch + ext

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ext) {
			_, _ = w.Write([]byte("this is not an archive"))
			return
		}
		http.NotFound(w, r) // no checksums: allowed, the unpack is what must complain
	}))
	defer srv.Close()

	err = downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", osName, arch)
	if err == nil {
		t.Fatal("expected an unreadable archive to fail")
	}
	if !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("expected %q: not readable, got %v", file, err)
	}
}

// ── archive extraction (reached only through the zip/tar loops) ────────────

// unpackNodeArchive dispatches on the extension so the Windows release (zip) and
// the POSIX one (tar.gz) share one entry point. Both branches must be reachable
// on every host: the fixture is built here, not downloaded.
func TestUnpackNodeArchive_DispatchesOnExtension(t *testing.T) {
	dest := t.TempDir()

	tarball := filepath.Join(t.TempDir(), "node.tar.gz")
	if err := os.WriteFile(tarball, buildTarGz(t, map[string]tarEntry{
		"node/bin/node": {body: "#!/bin/sh\n", mode: 0o755},
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unpackNodeArchive(tarball, filepath.Join(dest, "from-tar")); err != nil {
		t.Fatalf("tar.gz branch: %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "node.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("node/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("MZ")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unpackNodeArchive(zipPath, filepath.Join(dest, "from-zip")); err != nil {
		t.Fatalf("zip branch: %v", err)
	}

	for _, want := range []string{
		filepath.Join("from-tar", "node", "bin", "node"),
		filepath.Join("from-zip", "node", "node.exe"),
	} {
		if _, statErr := os.Stat(filepath.Join(dest, want)); statErr != nil {
			t.Fatalf("%s was not extracted: %v", want, statErr)
		}
	}
}

// A zip member whose name escapes the destination is refused, and the refusal
// happens before anything is written: the archive comes from a mirror, so its
// entry names are untrusted input.
func TestUnpackZipArchive_RejectsAbsoluteAndTraversalEntries(t *testing.T) {
	for _, name := range []string{"../escaped.txt", "/etc/escaped.txt"} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("pwned")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "evil.zip")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		dest := filepath.Join(t.TempDir(), "dest")
		err = unpackZipArchive(path, dest)
		if err == nil || !strings.Contains(err.Error(), "escapes the install directory") {
			t.Fatalf("entry %q: expected an escape error, got %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "escaped.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("entry %q wrote outside the destination", name)
		}
	}
}

// A tar member that cleans to ".." (the classic "../x" payload) must be refused
// by the same rule on the tar side.
func TestUnpackTarGzArchive_RejectsParentTraversal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.tar.gz")
	if err := os.WriteFile(path, buildTarGz(t, map[string]tarEntry{
		"../escaped.txt": {body: "pwned", mode: 0o644},
	}), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "dest")
	err := unpackTarGzArchive(path, dest)
	if err == nil || !strings.Contains(err.Error(), "escapes the install directory") {
		t.Fatalf("expected an escape error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatal("the traversal entry was written outside the destination")
	}
}

// writeArchiveSymlink refuses an absolute link target before creating anything:
// bin/npm → /usr/bin/evil would make the "private" runtime resolve outside the
// install directory.
func TestWriteArchiveSymlink_RejectsAbsoluteTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows; the absolute-target check is platform independent but the fixture is not")
	}
	dest := t.TempDir()
	target := filepath.Join(dest, "bin", "npm")
	err := writeArchiveSymlink(target, "bin/npm", "/usr/bin/evil")
	if err == nil || !strings.Contains(err.Error(), "absolute target") {
		t.Fatalf("expected an absolute-target error, got %v", err)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Fatal("a refused symlink must not be created")
	}
}

// An empty link name is equally unusable: os.Symlink("") would fail with a
// confusing ENOENT instead of saying the archive is malformed.
func TestWriteArchiveSymlink_RejectsEmptyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestWriteArchiveSymlink_RejectsAbsoluteTarget")
	}
	err := writeArchiveSymlink(filepath.Join(t.TempDir(), "npm"), "bin/npm", "")
	if err == nil || !strings.Contains(err.Error(), "absolute target") {
		t.Fatalf("expected the empty target to be refused with the same message, got %v", err)
	}
}

// A hardlink to a path outside the archive is refused: the source is resolved
// through safeArchivePath, so "link to /etc/passwd" cannot become a file in the
// install tree.
func TestWriteArchiveHardlink_RejectsEscapingSource(t *testing.T) {
	dest := t.TempDir()
	err := writeArchiveHardlink(dest, filepath.Join(dest, "bin", "node"), "../outside")
	if err == nil || !strings.Contains(err.Error(), "escapes the install directory") {
		t.Fatalf("expected an escape error, got %v", err)
	}
}

// ── small platform helpers ─────────────────────────────────────────────────

// The private runtime's bin directory is what PrependManagedNodePath puts on
// PATH, and the executable test is what decides whether it is "installed". On a
// POSIX host both answer the Unix spelling — the Windows spellings (node.exe at
// the root, .zip archives) are unreachable here by construction.
func TestManagedNodeBinDirAndExecutableNames(t *testing.T) {
	if IsWindows() {
		t.Skip("Windows spellings are pinned by the windows-tagged tests")
	}
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	if got, want := managedNodeBinDir(), filepath.Join(ManagedNodeDir(), "bin"); got != want {
		t.Fatalf("managedNodeBinDir() = %q, want %q", got, want)
	}
	if got := nodeExecutableName(); got != "node" {
		t.Fatalf("nodeExecutableName() = %q, want %q", got, "node")
	}
	if got := nodeArchiveExt(); got != ".tar.gz" {
		t.Fatalf("nodeArchiveExt() = %q, want .tar.gz on POSIX", got)
	}
}

// PrependManagedNodePath must not grow PATH on every call (EnsureNodeRuntime
// calls it on each install attempt), and with an empty PATH it must produce
// exactly the bin directory — a leading separator would read as "the current
// directory".
func TestPrependManagedNodePath_DoesNotDuplicateAnEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture creates a POSIX-looking bin/ layout")
	}
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	bin := filepath.Join(ManagedNodeDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin")

	if !PrependManagedNodePath() {
		t.Fatal("a PATH without the private runtime must report a change")
	}
	if got, want := os.Getenv("PATH"), bin+string(os.PathListSeparator)+"/usr/bin"; got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
	if PrependManagedNodePath() {
		t.Fatal("a second call must be a no-op, or PATH grows a duplicate on every install")
	}
	if got, want := os.Getenv("PATH"), bin+string(os.PathListSeparator)+"/usr/bin"; got != want {
		t.Fatalf("PATH = %q after a no-op call, want %q", got, want)
	}

	t.Setenv("PATH", "")
	if !PrependManagedNodePath() {
		t.Fatal("an empty PATH must be filled with the private runtime")
	}
	if got := os.Getenv("PATH"); got != bin {
		t.Fatalf("PATH = %q, want exactly %q", got, bin)
	}
}

// managedNodeInstalled is "the node binary is a regular file there" — a bare
// node/ directory (a half-finished manual copy) is not an install.
func TestManagedNodeInstalled_RequiresTheBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture writes the POSIX binary name")
	}
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	bin := filepath.Join(ManagedNodeDir(), "bin")
	if err := os.MkdirAll(filepath.Join(bin, nodeExecutableName()), 0o755); err != nil {
		t.Fatal(err)
	}
	if managedNodeInstalled() {
		t.Fatal("a directory named node is not an installed runtime")
	}
	if err := os.Remove(filepath.Join(bin, nodeExecutableName())); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, nodeExecutableName()), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !managedNodeInstalled() {
		t.Fatal("a regular node file must count as installed")
	}
}

// nodeMajorVersion answers 0 for a `node` that cannot be run or prints something
// that is not a version — it is the guard that decides whether the system Node
// is too old, so a parser that accepted garbage would accept a broken runtime.
func TestNodeMajorVersion_UnparsableOutputIsZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a POSIX shell script")
	}
	t.Setenv("PATH", "")
	dir := t.TempDir()
	writeFakeNode(t, dir, "#!/bin/sh\necho not-a-version\n")
	t.Setenv("PATH", dir)
	if got := nodeMajorVersion(); got != 0 {
		t.Fatalf("nodeMajorVersion() = %d, want 0 for an unparsable version", got)
	}

	// A node that exits non-zero is equally unusable.
	writeFakeNode(t, dir, "#!/bin/sh\nexit 1\n")
	if got := nodeMajorVersion(); got != 0 {
		t.Fatalf("nodeMajorVersion() = %d, want 0 for a node that fails", got)
	}
}
