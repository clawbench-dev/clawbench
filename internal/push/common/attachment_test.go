package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/model"
)

func TestClassifyIncoming(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		sticky     string
		wantKind   RouteKind
		wantShort  string
		wantMsg    string
		wantReason string
	}{
		{
			name: "explicit target wins over sticky",
			text: "@a1b2c3d4 hello", sticky: "sticky-session",
			wantKind: RouteToSession, wantShort: "a1b2c3d4", wantMsg: "hello",
		},
		{
			name: "explicit target with no sticky still routes",
			text: "@a1b2c3d4 hello", sticky: "",
			wantKind: RouteToSession, wantShort: "a1b2c3d4", wantMsg: "hello",
		},
		{
			name: "explicit target with empty message",
			text: "@a1b2c3d4", sticky: "",
			wantKind: RouteToSession, wantShort: "a1b2c3d4", wantMsg: "",
		},
		{
			name: "ls command lists sessions",
			text: "/ls", sticky: "sticky-session",
			wantKind: RouteListSessions,
		},
		{
			name: "ls command with surrounding whitespace",
			text: "  /ls  ", sticky: "",
			wantKind: RouteListSessions,
		},
		{
			name: "plain text goes to sticky session",
			text: "run the tests", sticky: "sticky-session",
			wantKind: RouteToSession, wantMsg: "run the tests",
		},
		{
			name: "plain text is trimmed",
			text: "  run the tests  ", sticky: "sticky-session",
			wantKind: RouteToSession, wantMsg: "run the tests",
		},
		{
			name: "plain text with no sticky asks for a target",
			text: "run the tests", sticky: "",
			wantKind: RouteNoTarget,
		},
		{
			name: "empty text with no sticky asks for a target",
			text: "", sticky: "",
			wantKind: RouteNoTarget,
		},
		{
			name: "whitespace-only text with sticky still routes to it",
			text: "   ", sticky: "sticky-session",
			wantKind: RouteToSession, wantMsg: "",
		},
		{
			// "@short" is under the 8-hex minimum, so it is not a command and
			// must fall through to the sticky path rather than being dropped.
			name: "near-miss command falls through to sticky",
			text: "@abc hi", sticky: "sticky-session",
			wantKind: RouteToSession, wantMsg: "@abc hi",
		},
		{
			name: "near-miss command with no sticky asks for a target",
			text: "@abc hi", sticky: "",
			wantKind: RouteNoTarget,
		},
		{
			// "/ls" is only a command when it is the whole message; embedded in
			// a sentence it is ordinary text for the AI.
			name: "ls embedded in text is not a command",
			text: "please run /ls for me", sticky: "sticky-session",
			wantKind: RouteToSession, wantMsg: "please run /ls for me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyIncoming(tt.text, tt.sticky)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.ShortID != tt.wantShort {
				t.Errorf("ShortID = %q, want %q", got.ShortID, tt.wantShort)
			}
			if got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

func TestClassifyIncomingAttachment(t *testing.T) {
	tests := []struct {
		name   string
		sticky string
		want   RouteKind
	}{
		{"with sticky target routes to it", "sess-1", RouteToSession},
		{"without sticky target asks for one", "", RouteNoTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyIncomingAttachment(tt.sticky)
			if got.Kind != tt.want {
				t.Fatalf("Kind = %v, want %v", got.Kind, tt.want)
			}
			// An attachment carries no text, and never names a session.
			if got.Message != "" {
				t.Errorf("Message = %q, want empty", got.Message)
			}
			if got.ShortID != "" {
				t.Errorf("ShortID = %q, want empty", got.ShortID)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name", "report.pdf", "report.pdf"},
		{"cjk name preserved", "钉钉让进步发生.pdf", "钉钉让进步发生.pdf"},
		{"spaces preserved", "my report.pdf", "my report.pdf"},
		// Path traversal must not survive: only the final component is kept.
		{"posix traversal stripped", "../../etc/passwd", "passwd"},
		{"windows traversal stripped", `..\..\windows\system32\evil.dll`, "evil.dll"},
		{"absolute posix path stripped", "/etc/shadow", "shadow"},
		{"nested path stripped", "a/b/c/file.txt", "file.txt"},
		// Characters that are illegal on Windows or awkward in shells.
		{"illegal chars replaced", `a:b*c?d"e<f>g|h.txt`, "a_b_c_d_e_f_g_h.txt"},
		{"newline replaced", "line\nbreak.txt", "line_break.txt"},
		{"nul replaced", "nul\x00byte.txt", "nul_byte.txt"},
		// Degenerate inputs fall back to a usable name.
		{"empty becomes attachment", "", "attachment"},
		{"dot becomes attachment", ".", "attachment"},
		{"dotdot becomes attachment", "..", "attachment"},
		{"only separators becomes attachment", "///", "attachment"},
		{"leading dots trimmed", "...hidden.txt", "hidden.txt"},
		{"trailing space and dot trimmed", "name. ", "name"},
		// Length guard keeps the extension.
		{"very long name truncated with extension", strings.Repeat("x", 300) + ".txt", strings.Repeat("x", 116) + ".txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFilename(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// The result must never introduce a separator, or the caller's
			// filepath.Join would silently escape the uploads directory.
			if strings.ContainsAny(got, `/\`) {
				t.Errorf("SanitizeFilename(%q) = %q contains a path separator", tt.in, got)
			}
			if len(got) > 120 {
				t.Errorf("SanitizeFilename(%q) = %q exceeds 120 bytes", tt.in, got)
			}
		})
	}
}

// TestSaveAttachment_CollisionDoesNotOverwrite verifies repeated sends of the
// same filename accumulate with a numeric suffix rather than clobbering.
func TestSaveAttachment_CollisionDoesNotOverwrite(t *testing.T) {
	project := t.TempDir()

	first, err := SaveAttachment(project, "report.pdf", strings.NewReader("first"), 1024)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second, err := SaveAttachment(project, "report.pdf", strings.NewReader("second"), 1024)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	if first.Path != ".clawbench/uploads/report.pdf" {
		t.Errorf("first path = %q", first.Path)
	}
	if second.Path != ".clawbench/uploads/report_1.pdf" {
		t.Errorf("second path = %q, want .clawbench/uploads/report_1.pdf", second.Path)
	}

	// The first file's bytes must be intact — this is what the O_EXCL create
	// protects, and a stat-then-create implementation could lose here.
	got, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(first.Path)))
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if string(got) != "first" {
		t.Errorf("first file content = %q, want 'first'", string(got))
	}
}

// TestSaveAttachment_ConcurrentSameName verifies concurrent saves of the same
// filename all land in distinct files. Media downloads run concurrently, so a
// check-then-create race would silently drop one of them.
func TestSaveAttachment_ConcurrentSameName(t *testing.T) {
	project := t.TempDir()

	const n = 8
	paths := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entry, err := SaveAttachment(project, "same.txt", strings.NewReader("x"), 1024)
			paths[i], errs[i] = entry.Path, err
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("save %d: %v", i, errs[i])
		}
		if seen[paths[i]] {
			t.Fatalf("path %q was handed out twice — a concurrent save overwrote another", paths[i])
		}
		seen[paths[i]] = true
	}

	entries, err := os.ReadDir(filepath.Join(project, ".clawbench", "uploads"))
	if err != nil {
		t.Fatalf("read uploads: %v", err)
	}
	if len(entries) != n {
		t.Errorf("files on disk = %d, want %d (one per concurrent save)", len(entries), n)
	}
}

// TestSaveAttachment_RejectsOversizedAndRemovesPartial verifies the size cap is
// enforced during the write and no truncated file is left behind.
func TestSaveAttachment_RejectsOversizedAndRemovesPartial(t *testing.T) {
	project := t.TempDir()

	_, err := SaveAttachment(project, "big.bin", strings.NewReader(strings.Repeat("a", 2048)), 1024)
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}

	entries, rerr := os.ReadDir(filepath.Join(project, ".clawbench", "uploads"))
	if rerr != nil {
		t.Fatalf("read uploads: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected oversized save must leave no file, found %d", len(entries))
	}
}

// TestSaveAttachment_ExactLimitSucceeds pins the boundary the "read one byte
// past the limit" trick exists for: a file of exactly maxBytes must be
// accepted, while maxBytes+1 must not. A naive io.Copy with a bare
// io.LimitReader would silently truncate instead of rejecting.
func TestSaveAttachment_ExactLimitSucceeds(t *testing.T) {
	project := t.TempDir()

	entry, err := SaveAttachment(project, "exact.bin", strings.NewReader(strings.Repeat("a", 1024)), 1024)
	if err != nil {
		t.Fatalf("a file exactly at the limit must be accepted, got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(entry.Path)))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if len(got) != 1024 {
		t.Errorf("saved %d bytes, want 1024 (must not be truncated)", len(got))
	}

	_, err = SaveAttachment(project, "over.bin", strings.NewReader(strings.Repeat("a", 1025)), 1024)
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("one byte over the limit must be rejected, got %v", err)
	}
}

// TestSaveAttachment_NoProjectPathFails verifies the guard that keeps an
// attachment from being written relative to the process CWD when the session
// carries no project root.
func TestSaveAttachment_NoProjectPathFails(t *testing.T) {
	_, err := SaveAttachment("", "a.txt", strings.NewReader("x"), 1024)
	if err == nil {
		t.Fatal("expected an error when the project path is empty")
	}
}

// TestSaveAttachment_ReaderErrorRemovesPartial verifies a mid-stream read
// failure does not leave a truncated attachment behind — the prompt would
// otherwise reference a file whose contents are silently cut short.
func TestSaveAttachment_ReaderErrorRemovesPartial(t *testing.T) {
	project := t.TempDir()

	_, err := SaveAttachment(project, "broken.bin", &failingReader{}, 1024)
	if err == nil {
		t.Fatal("expected an error when the reader fails mid-stream")
	}

	uploads := filepath.Join(project, ".clawbench", "uploads")
	entries, rerr := os.ReadDir(uploads)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return
		}
		t.Fatalf("read uploads: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("a failed save must leave no file, found %d", len(entries))
	}
}

// failingReader yields some bytes and then fails, simulating a dropped
// connection partway through a media download.
type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n >= 16 {
		return 0, errors.New("connection reset")
	}
	n := copy(p, strings.Repeat("x", 8))
	r.n += n
	return n, nil
}

// TestAttachmentMaxBytes_DefaultWhenUnset pins the fallback: an unloaded
// config leaves UploadMaxSizeMB at zero, which must not be interpreted as
// "reject everything".
func TestAttachmentMaxBytes_DefaultWhenUnset(t *testing.T) {
	orig := model.UploadMaxSizeMB
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	for _, mb := range []int{0, -1} {
		model.UploadMaxSizeMB = mb
		if got := AttachmentMaxBytes(); got != 100*1024*1024 {
			t.Errorf("UploadMaxSizeMB=%d: AttachmentMaxBytes() = %d, want the 100MB default", mb, got)
		}
	}

	model.UploadMaxSizeMB = 7
	if got := AttachmentMaxBytes(); got != 7*1024*1024 {
		t.Errorf("AttachmentMaxBytes() = %d, want 7MB", got)
	}
}

// TestDownloadAttachment_Success verifies the happy path: the bytes land in the
// project's uploads directory under a sanitized name.
func TestDownloadAttachment_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote-bytes"))
	}))
	defer srv.Close()

	project := t.TempDir()
	entry, err := DownloadAttachment(srv.Client(), srv.URL+"/report.txt", project, "report.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Path != ".clawbench/uploads/report.txt" {
		t.Errorf("path = %q, want .clawbench/uploads/report.txt", entry.Path)
	}
	got, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(entry.Path)))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != "remote-bytes" {
		t.Errorf("content = %q, want remote-bytes", string(got))
	}
}

// TestDownloadAttachment_NonOKStatus verifies a non-200 response is reported
// rather than saved — an error page must never become an attachment.
func TestDownloadAttachment_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>denied</html>"))
	}))
	defer srv.Close()

	project := t.TempDir()
	_, err := DownloadAttachment(srv.Client(), srv.URL, project, "x.txt")
	if err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention the status, got %v", err)
	}
	assertNoUploads(t, project)
}

// TestDownloadAttachment_DeclaredLengthTooLarge verifies the Content-Length
// pre-check: a server that advertises an oversized body is rejected before a
// single byte is read.
func TestDownloadAttachment_DeclaredLengthTooLarge(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(2*1024*1024))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	project := t.TempDir()
	_, err := DownloadAttachment(srv.Client(), srv.URL, project, "big.bin")
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}
	assertNoUploads(t, project)
}

// TestDownloadAttachment_UndeclaredLengthStillCapped verifies the size cap also
// holds when the server sends no Content-Length (chunked transfer), which is
// the case the pre-check cannot cover.
func TestDownloadAttachment_UndeclaredLengthStillCapped(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Flushing forces chunked encoding, so ContentLength stays -1.
		flusher, _ := w.(http.Flusher)
		for range 4 {
			_, _ = w.Write([]byte(strings.Repeat("a", 512*1024)))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	project := t.TempDir()
	_, err := DownloadAttachment(srv.Client(), srv.URL, project, "big.bin")
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}
	assertNoUploads(t, project)
}

// TestDownloadAttachment_UnreachableURL verifies a transport failure surfaces
// as an error instead of a zero-value entry.
func TestDownloadAttachment_UnreachableURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // closed before use, so the dial fails

	if _, err := DownloadAttachment(srv.Client(), url, t.TempDir(), "x.txt"); err == nil {
		t.Fatal("expected an error for an unreachable URL")
	}
}

// TestDownloadAttachment_NilClientUsesDefault verifies the nil-client fallback
// is exercised rather than panicking, and that a malformed URL is reported.
func TestDownloadAttachment_NilClientUsesDefault(t *testing.T) {
	project := t.TempDir()

	if _, err := DownloadAttachment(nil, "http://[::1]:namedport/", project, "x.txt"); err == nil {
		t.Fatal("expected an error for a malformed URL")
	}
	assertNoUploads(t, project)
}

// assertNoUploads fails when the project's uploads directory holds any file.
func assertNoUploads(t *testing.T, project string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(project, ".clawbench", "uploads"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read uploads: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no saved attachments, found %d", len(entries))
	}
}
