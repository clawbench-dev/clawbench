package platform

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// ── release-naming and version helpers ──────────────────────────────────────

// TestNodeReleasePlatform covers the GOOS/GOARCH → release-name mapping. A wrong
// name is a 404 at download time, i.e. a one-click install that fails for every
// user on that architecture; the Windows/ARM special case is a real one (Node
// ships win-arm64 but most npm packages have no prebuilt binaries for it, so the
// x64 build is installed under emulation, matching the shell installer).
func TestNodeReleasePlatform(t *testing.T) {
	osName, arch, err := nodeReleasePlatform()
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		if err != nil {
			t.Fatalf("nodeReleasePlatform on %s/%s: %v", runtime.GOOS, runtime.GOARCH, err)
		}
	default:
		if err == nil {
			t.Fatalf("%s has no release naming; expected an error", runtime.GOOS)
		}
		return
	}

	wantArch := map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86"}[runtime.GOARCH]
	if IsWindows() && wantArch == "arm64" {
		wantArch = "x64"
	}
	if wantArch != "" && arch != wantArch {
		t.Fatalf("arch = %q, want %q", arch, wantArch)
	}
	if IsWindows() {
		if osName != "win" {
			t.Fatalf("Windows must map to %q, got %q", "win", osName)
		}
		if nodeArchiveExt() != ".zip" {
			t.Fatalf("Windows release is a zip, got %q", nodeArchiveExt())
		}
		if nodeExecutableName() != "node.exe" {
			t.Fatalf("nodeExecutableName on Windows = %q", nodeExecutableName())
		}
	} else {
		if osName != runtime.GOOS {
			t.Fatalf("osName = %q, want %q", osName, runtime.GOOS)
		}
		if nodeArchiveExt() != ".tar.gz" {
			t.Fatalf("POSIX release is a tarball, got %q", nodeArchiveExt())
		}
		if nodeExecutableName() != "node" {
			t.Fatalf("nodeExecutableName = %q", nodeExecutableName())
		}
	}
}

// TestPickLatestLTS_ShapeEdges: index.json marks LTS with a codename string and
// non-LTS with `false`. Guessing from an even major number was wrong (v24 was
// once flagged false), so each decode shape is pinned, including the unknown one.
func TestPickLatestLTS_ShapeEdges(t *testing.T) {
	releases := []nodeRelease{
		{Version: "v24.0.0", LTS: false},   // not LTS, even though the major is even
		{Version: "v23.0.0", LTS: true},    // decoded as a JSON boolean, not a codename
		{Version: "v25.0.0", LTS: nil},     // malformed entry
		{Version: "v24.1.0", LTS: 1},       // a number is not a valid LTS marker
		{Version: "v20.19.0", LTS: "Iron"}, // the winner
		{Version: "garbage", LTS: "Jod"},   // unparsable version, skipped
		{Version: "v19.0.0", LTS: "Old"},   // below the minimum major
		{Version: "v20.11.0", LTS: "Iron"}, // older minor of the same line
	}
	got, err := pickLatestLTS(releases)
	if err != nil {
		t.Fatalf("pickLatestLTS: %v", err)
	}
	// v23 is the highest LTS release at or above the minimum, and `true` counts
	// as LTS just like a codename. v24.1.0 is skipped because a number is not a
	// valid marker, so the even major alone does not make a release LTS.
	if got != "v23.0.0" {
		t.Fatalf("pickLatestLTS() = %q, want v23.0.0", got)
	}
}

func TestParseNodeVersion_ShortAndNegative(t *testing.T) {
	// "v22.11" (no patch) is a valid release string in some mirrors.
	major, minor, patch, ok := parseNodeVersion("v22.11")
	if !ok || major != 22 || minor != 11 || patch != 0 {
		t.Fatalf("parseNodeVersion(v22.11) = %d.%d.%d,%v", major, minor, patch, ok)
	}
	if _, _, _, ok := parseNodeVersion("v22"); ok {
		t.Fatal("a bare major is not a release version")
	}
	if _, _, _, ok := parseNodeVersion("v22.-1.0"); ok {
		t.Fatal("negative components must not parse")
	}
	if _, _, _, ok := parseNodeVersion("v22.x.0"); ok {
		t.Fatal("non-numeric components must not parse")
	}
}

func TestNewerNodeVersion(t *testing.T) {
	cases := []struct {
		a, b [3]int
		want bool
	}{
		{[3]int{22, 11, 0}, [3]int{20, 19, 0}, true},
		{[3]int{20, 19, 0}, [3]int{22, 11, 0}, false},
		{[3]int{22, 12, 0}, [3]int{22, 11, 9}, true},
		{[3]int{22, 11, 9}, [3]int{22, 11, 10}, false},
		{[3]int{22, 11, 10}, [3]int{22, 11, 9}, true},
	}
	for _, c := range cases {
		got := newerNodeVersion(c.a[0], c.a[1], c.a[2], c.b[0], c.b[1], c.b[2])
		if got != c.want {
			t.Errorf("newerNodeVersion(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// ── home / PATH ─────────────────────────────────────────────────────────────

// TestManagedNodeHome_FallsBackToThePlatformDefault: with neither CLAWBENCH_HOME
// nor XDG_DATA_HOME set, the runtime lands under the user's home. It must be an
// absolute path (the server may run with any working directory).
func TestManagedNodeHome_FallsBackToThePlatformDefault(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if IsWindows() {
		// Windows prefers LOCALAPPDATA; either way the answer must be absolute.
		if !filepath.IsAbs(ManagedNodeHome()) {
			t.Fatalf("ManagedNodeHome() = %q, want an absolute path", ManagedNodeHome())
		}
		return
	}
	want := filepath.Join(UserHomeDir(), ".local", "share", "clawbench")
	if got := ManagedNodeHome(); got != want {
		t.Fatalf("ManagedNodeHome() = %q, want %q", got, want)
	}
}

// TestPrependManagedNodePath_PrependsToEmptyPath: an empty PATH must not produce
// a leading separator (":" is the current directory on POSIX, which would make
// the shell resolve commands from wherever it happens to be).
func TestPrependManagedNodePath_PrependsToEmptyPath(t *testing.T) {
	if IsWindows() {
		t.Skip("PATH semantics differ on Windows")
	}
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	bin := managedNodeBinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	if !PrependManagedNodePath() {
		t.Fatal("expected PATH to change")
	}
	if got := os.Getenv("PATH"); got != bin {
		t.Fatalf("PATH = %q, want exactly %q", got, bin)
	}
}

// TestManagedNodeInstalled_IgnoresDirectories: a *directory* named "node" is not
// an installed runtime; treating it as one would make EnsureNodeRuntime skip the
// download and then fail with "does not run".
func TestManagedNodeInstalled_IgnoresDirectories(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	if managedNodeInstalled() {
		t.Fatal("nothing is installed yet")
	}
	if err := os.MkdirAll(filepath.Join(managedNodeBinDir(), nodeExecutableName()), 0o755); err != nil {
		t.Fatal(err)
	}
	if managedNodeInstalled() {
		t.Fatal("a directory named like the binary is not an installed runtime")
	}
}

// ── download / verify / unpack ──────────────────────────────────────────────

// buildReleaseArchives builds a minimal Node.js release for the current platform
// in the format that platform actually ships (tar.gz on POSIX, zip on Windows):
// unpackNodeArchive must accept whatever the download produced. `nodeDir` is the
// version/os/arch directory the archive is unpacked from; the caller derives the
// expected top directory from the release file name so the two can differ (an
// archive that unpacks to some other directory must be refused).
func buildReleaseArchives(t *testing.T, nodeDir, nodeBody string) []byte {
	t.Helper()
	prefix := nodeDir + "/"
	entries := map[string]tarEntry{
		prefix + "bin/" + nodeExecutableName(): {body: nodeBody, mode: 0o755},
		prefix + "LICENSE":                     {body: "license", mode: 0o644},
	}
	if !IsWindows() {
		return buildTarGz(t, entries)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(os.FileMode(entries[name].mode))
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entries[name].body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseArchiveName is the file downloadNodeRelease asks for on this platform,
// derived from the same helpers the production code uses (the extension differs
// between the POSIX tarball and the Windows zip).
func releaseArchiveName(osName, arch string) string {
	return fmt.Sprintf("node-%s-%s-%s%s", "v22.11.0", osName, arch, nodeArchiveExt())
}

// TestDownloadNodeRelease_ChecksumMismatchLeavesNothingBehind: a corrupted
// download must fail before anything replaces (or creates) the install
// directory, and the staging directory must be gone afterwards — otherwise every
// failed attempt leaves a few hundred megabytes in the user's data dir.
func TestDownloadNodeRelease_ChecksumMismatchLeavesNothingBehind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	file := releaseArchiveName(osName, arch)
	archive := buildReleaseArchives(t, strings.TrimSuffix(file, nodeArchiveExt()), "node")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v22.11.0/SHASUMS256.txt":
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + file + "\n"))
		case "/v22.11.0/" + file:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err = downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", osName, arch)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum error, got %v", err)
	}
	if _, statErr := os.Stat(ManagedNodeDir()); !os.IsNotExist(statErr) {
		t.Fatalf("a failed install must not create %q", ManagedNodeDir())
	}
	entries, readErr := os.ReadDir(home)
	if readErr == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".node-staging-") {
				t.Fatalf("staging directory %q was left behind", e.Name())
			}
		}
	}
}

// TestDownloadNodeRelease_RejectsArchiveWithoutTheTopDirectory: a release
// archive always contains node-<version>-<os>-<arch>/. A tar that unpacks to
// anything else is not a Node.js release, and renaming it into place would leave
// a "node" directory with no bin/ in it.
func TestDownloadNodeRelease_RejectsArchiveWithoutTheTopDirectory(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	file := releaseArchiveName(osName, arch)
	// Same bytes as a release, but nested under a different top dir.
	archive := buildReleaseArchives(t, "something-else", "node")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v22.11.0/"+file {
			_, _ = w.Write(archive)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err = downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", osName, arch)
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("expected a 'does not contain' error, got %v", err)
	}
}

// TestDownloadNodeRelease_ReplacesAPreviousCopy: an upgrade replaces the old
// runtime in place, and nothing of the old tree may survive.
func TestDownloadNodeRelease_ReplacesAPreviousCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	osName, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	if err := os.MkdirAll(ManagedNodeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(ManagedNodeDir(), "old-version-marker")
	if err := os.WriteFile(stale, []byte("v20"), 0o644); err != nil {
		t.Fatal(err)
	}

	file := releaseArchiveName(osName, arch)
	archive := buildReleaseArchives(t, strings.TrimSuffix(file, nodeArchiveExt()), "node")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v22.11.0/"+file {
			_, _ = w.Write(archive)
			return
		}
		http.NotFound(w, r) // no checksums: allowed
	}))
	defer srv.Close()

	if err := downloadNodeRelease(t.Context(), srv.URL, "v22.11.0", osName, arch); err != nil {
		t.Fatalf("downloadNodeRelease: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the previous Node.js must be replaced, not merged")
	}
	if _, err := os.Stat(filepath.Join(ManagedNodeDir(), "bin", nodeExecutableName())); err != nil {
		t.Fatalf("the new runtime is missing: %v", err)
	}
}

// TestHTTPGetLimited and TestDownloadToFile pin the transport contract: a
// non-200 is an error naming the URL (not an empty body), and a body over the cap
// is refused instead of being half-written.
func TestHTTPGetLimitedAndDownloadToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/body":
			_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
		}
	}))
	defer srv.Close()

	if _, err := httpGetLimited(t.Context(), srv.URL+"/missing", 1024); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected an HTTP 404 error, got %v", err)
	}
	body, err := httpGetLimited(t.Context(), srv.URL+"/body", 10)
	if err != nil {
		t.Fatalf("httpGetLimited: %v", err)
	}
	if len(body) != 10 {
		t.Fatalf("limit not applied: got %d bytes", len(body))
	}
	if _, err := httpGetLimited(t.Context(), "http://bad host/", 10); err == nil {
		t.Fatal("expected an error for an unusable URL")
	}

	path := filepath.Join(t.TempDir(), "archive")
	digest, err := downloadToFile(t.Context(), srv.URL+"/body", path)
	if err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}
	if digest == "" {
		t.Fatal("downloadToFile must return the SHA-256 of what it wrote")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	if info.Size() != 100 {
		t.Fatalf("downloaded %d bytes, want 100", info.Size())
	}
	if _, err := downloadToFile(t.Context(), srv.URL+"/missing", path); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected an HTTP 404 error, got %v", err)
	}
	if _, err := downloadToFile(t.Context(), "http://bad host/", path); err == nil {
		t.Fatal("expected an error for an unusable URL")
	}
	// The destination cannot be created: the error is reported, not ignored.
	if _, err := downloadToFile(t.Context(), srv.URL+"/body", filepath.Join(path, "nested")); err == nil {
		t.Fatal("expected an error when the destination is not writable")
	}
}

// TestFetchNodeChecksum_MatchesExactFile: the SHASUMS file lists many archives;
// matching the wrong line would reject every good download on a machine whose
// platform differs by one character.
func TestFetchNodeChecksum_MatchesExactFile(t *testing.T) {
	file := "node-v22.11.0-linux-x64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  node-v22.11.0-linux-arm64.tar.gz\n", strings.Repeat("a", 64))
		fmt.Fprintf(w, "%s  %s\n", strings.Repeat("b", 64), file)
		fmt.Fprintf(w, "%s  node-v22.11.0-win-x64.zip\n", strings.Repeat("c", 64))
	}))
	defer srv.Close()

	got, err := fetchNodeChecksum(t.Context(), srv.URL, file)
	if err != nil {
		t.Fatalf("fetchNodeChecksum: %v", err)
	}
	if got != strings.Repeat("b", 64) {
		t.Fatalf("checksum = %q, want the line for %s", got, file)
	}
	// Any request to a URL the server answers with a body is fine; the caller
	// treats a failed fetch as "no checksum published", so the only thing
	// pinned here is that the listed file is matched exactly.
	if got, err := fetchNodeChecksum(t.Context(), srv.URL, "node-v22.11.0-win-arm64.zip"); err != nil || got != "" {
		t.Fatalf("unlisted file: got %q, %v; want empty and no error", got, err)
	}
}

// TestEnsureNodeRuntime_ReportsUnsupportedPlatform: when the platform has no
// release naming the install must fail with a message, not spin through the
// download sources.
func TestEnsureNodeRuntime_ReportsUnsupportedPlatform(t *testing.T) {
	if _, _, err := nodeReleasePlatform(); err == nil {
		t.Skip("this platform has a release naming; the unsupported branch cannot be reached")
	}
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")
	if _, err := EnsureNodeRuntime(); err == nil {
		t.Fatal("expected an error on a platform without release binaries")
	}
}

// TestExtractTarEntry_RejectsEscapingPaths pins the entries the tar extractor must
// refuse: an absolute name, a ".." name and an absolute symlink target. The
// archive comes from a mirror, so its entry names are untrusted input.
//
// The escape fixtures are chosen to be hostile on every platform. ".." and
// "../.." traverse everywhere, and the absolute name is built with
// filepath.Join from an existing absolute directory so it carries a drive letter
// on Windows (a lone "/etc/passwd" is not absolute there — no volume name — and
// the "escape" would silently become a path inside the destination).
func TestExtractTarEntry_RejectsEscapingPaths(t *testing.T) {
	dest := t.TempDir()
	outside := filepath.Join(filepath.Dir(dest), "outside.txt")
	if !filepath.IsAbs(outside) {
		t.Fatalf("fixture %q must be absolute on %s", outside, runtime.GOOS)
	}
	for _, header := range []*tar.Header{
		{Name: outside, Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "bin/link", Typeflag: tar.TypeSymlink, Linkname: outside},
		{Name: "bin/up", Typeflag: tar.TypeSymlink, Linkname: "../../outside"},
		{Name: "bin/hard", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd"},
	} {
		reader := bytes.NewReader(nil)
		err := extractTarEntry(reader, header, dest)
		if err == nil {
			t.Errorf("entry %q (%c) must be refused", header.Name, header.Typeflag)
		}
	}

	// The archive root itself is ignored rather than treated as an escape.
	if err := extractTarEntry(bytes.NewReader(nil), &tar.Header{Name: ".", Typeflag: tar.TypeDir}, dest); err != nil {
		t.Fatalf("the archive root must be skipped silently: %v", err)
	}
}

// TestExtractTarEntry_RejectsRootRelativeNames is the one fixture that only
// exists on POSIX: "/etc/passwd" is the archiver-style name for "a file at the
// filesystem root", and only on POSIX does it resolve to a path outside the
// destination. On Windows the leading slash does not make a path absolute (no
// volume), so there is nothing for the extractor to refuse — the same attack is
// covered above with a genuinely absolute Windows path.
func TestExtractTarEntry_RejectsRootRelativeNames(t *testing.T) {
	if IsWindows() {
		t.Skip(`"/etc/passwd" is not absolute on Windows (no volume name)`)
	}
	if err := extractTarEntry(bytes.NewReader(nil),
		&tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644}, t.TempDir()); err == nil {
		t.Fatal(`the entry "/etc/passwd" must be refused`)
	}
}

// TestExtractZipEntry_RejectsEscapingPaths is the zip half of the same rule. As
// above, the absolute fixture comes from filepath.Join so it is absolute (drive
// letter included) on Windows too.
func TestExtractZipEntry_RejectsEscapingPaths(t *testing.T) {
	dest := t.TempDir()
	abs := filepath.Join(filepath.Dir(dest), "abs", "escape.txt")
	if !filepath.IsAbs(abs) {
		t.Fatalf("fixture %q must be absolute on %s", abs, runtime.GOOS)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("pwned")); err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Create(filepath.ToSlash(abs)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if err := extractZipEntry(entry, dest); err == nil {
			t.Errorf("entry %q must be refused", entry.Name)
		}
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("nothing may be written outside the destination: %v", entries)
	}
}

// TestWriteArchiveFile_AppliesModeAndDefaults: bin/node must stay executable
// after unpacking (a release tarball's whole purpose), and an entry that carries
// no mode becomes 0644 instead of 0000 (which would make it unreadable).
func TestWriteArchiveFile_AppliesModeAndDefaults(t *testing.T) {
	if IsWindows() {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	exec := filepath.Join(dir, "bin", "node")
	if err := writeArchiveFile(strings.NewReader("#!/bin/sh\n"), exec, 0o755); err != nil {
		t.Fatalf("writeArchiveFile: %v", err)
	}
	info, err := os.Stat(exec)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("mode = %v, want the executable bit", info.Mode().Perm())
	}

	plain := filepath.Join(dir, "LICENSE")
	if err := writeArchiveFile(strings.NewReader("license"), plain, 0); err != nil {
		t.Fatalf("writeArchiveFile: %v", err)
	}
	info, err = os.Stat(plain)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644 for a mode-less entry", info.Mode().Perm())
	}

	// A target whose parent is a file cannot be created: the error is returned.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveFile(strings.NewReader("x"), filepath.Join(blocker, "child"), 0o644); err == nil {
		t.Fatal("expected an error when the parent is not a directory")
	}
}

// TestUnpackZipArchive_RejectsUnreadableArchive: a truncated download must be
// reported as "not readable" rather than decrypting to zero entries and letting
// the caller rename an empty directory into place.
func TestUnpackNodeArchive_RejectsTruncatedGzip(t *testing.T) {
	full := buildNodeTarGz(t, "node-v22.11.0")
	truncated := full[:len(full)/2]
	if err := unpackNodeArchive(writeTemp(t, "node.tar.gz", truncated), t.TempDir()); err == nil {
		t.Fatal("expected an error for a truncated archive")
	}
}

// safeArchivePath cleans an entry name and hands back the path the extractor
// will join onto the destination — i.e. an OS path, not a POSIX one ("/" becomes
// "\" on Windows). The expectations are therefore built with filepath.FromSlash,
// and the "absolute" cases use a name that is absolute on THIS host: a bare
// "/abs" carries no volume name on Windows and is a rooted-but-not-absolute path
// there, which is covered separately below.
func TestSafeArchivePath(t *testing.T) {
	ok := map[string]string{
		"node-v22/bin/node": filepath.FromSlash("node-v22/bin/node"),
		"./node/bin":        filepath.FromSlash("node/bin"),
		".":                 "",
		"a/../b/c":          filepath.FromSlash("b/c"),
	}
	for name, want := range ok {
		got, err := safeArchivePath(name)
		if err != nil {
			t.Errorf("safeArchivePath(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("safeArchivePath(%q) = %q, want %q", name, got, want)
		}
	}

	escaping := []string{"..", "../x", "a/../../x", filepath.Join(t.TempDir(), "escaped.txt")}
	for _, name := range escaping {
		if _, err := safeArchivePath(name); err == nil {
			t.Errorf("safeArchivePath(%q) must fail", name)
		}
	}
}

// A member rooted at "/" ("/abs", "/etc/x") is an absolute path on POSIX. On
// Windows it has no volume name, so filepath.IsAbs is false and it cleans to
// `\abs` instead — see TestSafeArchivePath_RootRelativeNameIsNotEscapedOnWindows
// for what the code does there. POSIX-only assertion, hence the guard.
func TestSafeArchivePath_RootRelativeNameIsRejected(t *testing.T) {
	if IsWindows() {
		t.Skip("a bare /abs has no volume name on Windows and is not an absolute path")
	}
	for _, name := range []string{"/abs", "/etc/x"} {
		if _, err := safeArchivePath(name); err == nil {
			t.Errorf("safeArchivePath(%q) must fail", name)
		}
	}
}

// ── root-relative archive names (Windows) ──────────────────────────────────

// safeArchivePath rejects absolute names, but on Windows "/abs" has no volume
// name: filepath.IsAbs says false and it cleans to `\abs`. That is still safe —
// the extractors join it onto dest, so `C:\dest` + `\abs` is `C:\dest\abs`
// because a rooted path without a volume keeps the joined volume. This test
// pins that behavior on Windows only (a POSIX host never sees it, where the
// same name is absolute and refused by TestSafeArchivePath_RootRelativeNameIsRejected).
func TestSafeArchivePath_RootRelativeNameIsNotEscapedOnWindows(t *testing.T) {
	if !IsWindows() {
		t.Skip("a bare /abs is an absolute path on POSIX and is rejected there")
	}
	got, err := safeArchivePath("/abs")
	if err != nil {
		t.Fatalf("safeArchivePath(/abs) = %v; the containment check is what must hold", err)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("safeArchivePath(/abs) = %q, which is absolute and would escape the destination", got)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	target := filepath.Join(dest, got)
	if !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		t.Fatalf("Join(%q, %q) = %q, which is outside the destination", dest, got, target)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// buildNodeTarGz builds a minimal tarball with the given top directory. It is
// always a tar: it feeds the tar extractor directly, regardless of the host OS.
func buildNodeTarGz(t *testing.T, top string) []byte {
	t.Helper()
	return buildTarGz(t, map[string]tarEntry{
		top + "/bin/node": {body: "#!/bin/sh\necho v22.11.0\n", mode: 0o755},
		top + "/LICENSE":  {body: "license", mode: 0o644},
	})
}

// ── mirror ordering, staged install, archive replacement ───────────────────

// The mirror choice follows the same region probe the npm registry rewrite uses:
// a machine in mainland China must try npmmirror first (nodejs.org/dist is often
// unreachable or slow there) and vice versa. Getting the order wrong is a
// one-click install that either times out or falls back on every single attempt.
func TestNodeDownloadBases_RegionPicksTheFirstSource(t *testing.T) {
	if _, _, err := nodeReleasePlatform(); err != nil {
		t.Skip("no release naming on this platform; the installer never asks for a source")
	}
	// Pin the probe's cached verdict instead of letting it hit the network, then
	// put the previous value back: the result is a package-level cache every
	// other test in this binary shares.
	prev := ChinaMirrorChecked.Load()
	t.Cleanup(func() { ChinaMirrorChecked.Store(prev) })

	ChinaMirrorChecked.Store(1)
	got := nodeDownloadBases()
	if strings.Join(got, ",") != nodeMirrorBaseURL+","+nodeDistBaseURL {
		t.Fatalf("bases in mainland China = %v, want the npmmirror mirror first", got)
	}

	ChinaMirrorChecked.Store(2)
	got = nodeDownloadBases()
	if strings.Join(got, ",") != nodeDistBaseURL+","+nodeMirrorBaseURL {
		t.Fatalf("bases elsewhere = %v, want nodejs.org first", got)
	}
}

// Every source has to fail before the install gives up, and the error must be
// the last real cause rather than a generic "download failed": the caller prints
// it to the user, who needs to know whether the index or the archive broke.
func TestInstallManagedNode_ReportsTheCauseWhenAllSourcesFail(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	indexless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer indexless.Close()

	// First source: index.json is not JSON at all.
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_, _ = w.Write([]byte("not json"))
			return
		}
		http.NotFound(w, r)
	}))
	defer garbled.Close()

	restore := stubNodeDownloadBasesValue(t, []string{garbled.URL, indexless.URL})
	defer restore()

	_, err := installManagedNode()
	if err == nil {
		t.Fatal("expected an error when no source serves an index")
	}
	// The last source's failure is HTTP 404 from index.json, not the first
	// source's JSON syntax error.
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("error must name the last real cause, got %v", err)
	}
	if !strings.Contains(err.Error(), "/index.json") {
		t.Fatalf("error must say which file was unusable, got %v", err)
	}
}

// A private runtime that unpacks but is too old is not "usable": the agent CLIs
// require nodeMinMajor, so the download must still happen instead of the old
// copy being silently accepted.
func TestEnsureNodeRuntime_ReplacesTooOldPrivateRuntime(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho v22.11.0\n")
	t.Setenv("CLAWBENCH_HOME", t.TempDir())
	t.Setenv("PATH", "")
	writeFakeNode(t, managedNodeBinDir(), "#!/bin/sh\necho v18.20.4\n")

	server := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	note, err := EnsureNodeRuntime()
	if err != nil {
		t.Fatalf("EnsureNodeRuntime: %v", err)
	}
	if !strings.Contains(note, "v22.11.0") {
		t.Fatalf("note = %q, want it to report the newly installed version", note)
	}
	if major := nodeMajorVersion(); major != 22 {
		t.Fatalf("node on PATH reports major %d, want the downloaded 22", major)
	}
}

// A private runtime that is already new enough is returned as-is, without a
// download: the note is empty because nothing was installed.
func TestEnsureNodeRuntime_AcceptsAnUpToDatePrivateRuntimeWithoutDownloading(t *testing.T) {
	if IsWindows() {
		t.Skip("the fake runtime is a POSIX shell script")
	}
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
		t.Fatalf("note = %q, want empty for a reused runtime", note)
	}
}

// The staging directory is created inside the destination's parent so the final
// move is a rename, and it must not survive a failed run — a leftover per attempt
// would fill the data directory of a machine whose network never works.
//
// The index is served here so the failure happens while fetching the archive,
// which is the only point where the staging directory exists.
func TestDownloadNodeRelease_FailedRunLeavesNoStagingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()
	restore := stubNodeDownloadBasesValue(t, []string{server.URL})
	defer restore()

	_, err := installManagedNode()
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected the archive 404 to be reported, got %v", err)
	}
	entries, readErr := os.ReadDir(home)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".node-staging-") {
			t.Fatalf("staging directory %q survived a failed download", e.Name())
		}
	}
}

// The archive name is what the download URL is built from: a version that
// already carries the `v` must not become `node-vv22…`, and the platform pair
// must match what Node actually publishes — a wrong name is a 404 for every
// user on that platform.
func TestDownloadNodeRelease_RequestsThePlatformArchiveName(t *testing.T) {
	t.Setenv("CLAWBENCH_HOME", t.TempDir())

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_, _ = w.Write([]byte(`[{"version":"v22.11.0","lts":"Jod"}]`))
			return
		}
		asked = append(asked, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()
	restore := stubNodeDownloadBasesValue(t, []string{server.URL})
	defer restore()

	_, _ = installManagedNode()
	if len(asked) == 0 {
		t.Fatal("no archive was requested")
	}
	ext := nodeArchiveExt()
	want := "/v22.11.0/node-v22.11.0-" + osReleaseName(t) + "-" + archReleaseName(t) + ext
	if asked[0] != want {
		t.Fatalf("archive requested at %q, want %q", asked[0], want)
	}
}

// osReleaseName / archReleaseName return the release names of this machine, for
// the URL assertion above.
func osReleaseName(t *testing.T) string {
	t.Helper()
	osName, _, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	return osName
}

func archReleaseName(t *testing.T) string {
	t.Helper()
	_, arch, err := nodeReleasePlatform()
	if err != nil {
		t.Skipf("no release naming for this platform: %v", err)
	}
	return arch
}

// stubNodeDownloadBasesValue replaces the source list unconditionally, for tests
// that run when Node may or may not be installed.
func stubNodeDownloadBasesValue(t *testing.T, bases []string) func() {
	t.Helper()
	prev := nodeDownloadBases
	nodeDownloadBases = func() []string { return bases }
	return func() { nodeDownloadBases = prev }
}

// ── private runtime failing to run ─────────────────────────────────────────

// An older private copy that still cannot run must not be treated as usable: the
// installer downloads a fresh one, and if the fresh one does not run either the
// error has to name the missing-libraries cause (the fix is a system package, not
// clicking install again). The stale copy must be replaced, not left in place.
func TestEnsureNodeRuntime_ReplacesAnUnusablePrivateRuntime(t *testing.T) {
	file, archive := fakeNodeRelease(t, "#!/bin/sh\necho not-a-version\n")
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)
	t.Setenv("PATH", "")
	writeFakeNode(t, managedNodeBinDir(), "#!/bin/sh\necho v18.20.4\n")

	server := serveNodeRelease(t, file, archive, checksumOf(archive))
	defer server.Close()
	restore := stubNodeDownloadBases(t, []string{server.URL})
	defer restore()

	_, err := EnsureNodeRuntime()
	if err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("expected the unpacked runtime to be reported as unusable, got %v", err)
	}
	// The install itself succeeded (the file is the new one), it just does not
	// run — so the message must also name where it went, for the user to inspect.
	if !strings.Contains(err.Error(), ManagedNodeDir()) {
		t.Fatalf("error must name the install directory, got %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(managedNodeBinDir(), nodeExecutableName()))
	if readErr != nil {
		t.Fatalf("runtime missing after the failed activation: %v", readErr)
	}
	if !strings.Contains(string(body), "not-a-version") {
		t.Fatalf("the stale private copy was not replaced: %q", body)
	}
}
