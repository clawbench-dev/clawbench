package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
)

// mediaTestServer wires both DingTalk endpoints (messageFiles/download and the
// temporary download URL) onto one httptest server and points the package
// variables at it.
func mediaTestServer(t *testing.T, downloadBody string, downloadStatus int) (*httptest.Server, *string) {
	t.Helper()
	var gotDownloadCode string

	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-acs-dingtalk-access-token") == "" {
			t.Errorf("download API call must carry the access token header")
		}
		var req struct {
			DownloadCode string `json:"downloadCode"`
			RobotCode    string `json:"robotCode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotDownloadCode = req.DownloadCode
		if req.RobotCode == "" {
			t.Errorf("download API call must carry robotCode")
		}
		w.WriteHeader(http.StatusOK)
		// The temporary URL points back at this same server.
		_, _ = w.Write([]byte(`{"downloadUrl":"` + "http://" + r.Host + `/blob"}`))
	})
	mux.HandleFunc("/blob", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(downloadStatus)
		_, _ = w.Write([]byte(downloadBody))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	origFileURL := dingtalkMessageFileURL
	dingtalkMessageFileURL = srv.URL + "/download"
	t.Cleanup(func() { dingtalkMessageFileURL = origFileURL })

	return srv, &gotDownloadCode
}

func newMediaManager(t *testing.T) *Manager {
	t.Helper()
	mgr := NewManager(&model.DingTalkConfig{AppKey: "test-app-key"})
	mgr.cachedToken = "test-access-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)
	t.Cleanup(func() { resetTokenCache(mgr) })
	return mgr
}

func TestExtractMedia(t *testing.T) {
	tests := []struct {
		name         string
		msgType      string
		content      any
		wantOK       bool
		wantCode     string
		wantFilename string
	}{
		{
			name:    "file message",
			msgType: "file",
			content: map[string]any{"downloadCode": "code-1", "fileName": "报告.pdf"},
			wantOK:  true, wantCode: "code-1", wantFilename: "报告.pdf",
		},
		{
			name:    "picture message has no filename",
			msgType: "picture",
			content: map[string]any{"downloadCode": "code-2"},
			wantOK:  true, wantCode: "code-2", wantFilename: "image.png",
		},
		{
			name:    "file message without filename gets a fallback",
			msgType: "file",
			content: map[string]any{"downloadCode": "code-3"},
			wantOK:  true, wantCode: "code-3", wantFilename: "attachment",
		},
		{
			name:    "text is not media",
			msgType: "text",
			content: map[string]any{"content": "hello"},
			wantOK:  false,
		},
		{
			name:    "richText is not media",
			msgType: "richText",
			content: map[string]any{"richText": []any{}},
			wantOK:  false,
		},
		{
			name:    "file message missing downloadCode",
			msgType: "file",
			content: map[string]any{"fileName": "x.pdf"},
			wantOK:  false,
		},
		{
			name:    "nil content",
			msgType: "file",
			content: nil,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, filename, ok := extractMedia(tt.msgType, tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if filename != tt.wantFilename {
				t.Errorf("filename = %q, want %q", filename, tt.wantFilename)
			}
		})
	}
}

func TestDownloadMedia_Success(t *testing.T) {
	_, gotCode := mediaTestServer(t, "file-bytes", http.StatusOK)
	mgr := newMediaManager(t)
	project := t.TempDir()

	entry, err := mgr.downloadMedia(context.Background(), "file",
		map[string]any{"downloadCode": "code-abc", "fileName": "report.txt"}, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *gotCode != "code-abc" {
		t.Errorf("downloadCode sent = %q, want code-abc", *gotCode)
	}
	if entry.Path != ".clawbench/uploads/report.txt" {
		t.Errorf("path = %q, want .clawbench/uploads/report.txt", entry.Path)
	}
	got, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(entry.Path)))
	if err != nil {
		t.Fatalf("saved file unreadable: %v", err)
	}
	if string(got) != "file-bytes" {
		t.Errorf("saved content = %q, want file-bytes", string(got))
	}
}

func TestDownloadMedia_SanitizesTraversalFilename(t *testing.T) {
	mediaTestServer(t, "x", http.StatusOK)
	mgr := newMediaManager(t)
	project := t.TempDir()

	// A malicious fileName must not escape the uploads directory.
	entry, err := mgr.downloadMedia(context.Background(), "file",
		map[string]any{"downloadCode": "c", "fileName": "../../../etc/passwd"}, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Path != ".clawbench/uploads/passwd" {
		t.Fatalf("path = %q, want .clawbench/uploads/passwd", entry.Path)
	}

	// The saved file must live under the project's uploads directory. Asserting
	// the absolute location (rather than stat-ing a guessed traversal target)
	// is what actually proves containment: the guessed path could be a real
	// system file like /etc/passwd and pass for the wrong reason.
	abs := filepath.Join(project, filepath.FromSlash(entry.Path))
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("attachment was not saved under the project: %v", err)
	}
	uploadsDir := filepath.Join(project, ".clawbench", "uploads")
	rel, err := filepath.Rel(uploadsDir, abs)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if rel != "passwd" {
		t.Errorf("saved file %q is not directly inside the uploads dir", rel)
	}
}

func TestDownloadMedia_NonMediaTypeIsRejected(t *testing.T) {
	mediaTestServer(t, "x", http.StatusOK)
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "text",
		map[string]any{"content": "hello"}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a non-media message type")
	}
}

func TestDownloadMedia_BlobStatusError(t *testing.T) {
	mediaTestServer(t, "nope", http.StatusForbidden)
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "file",
		map[string]any{"downloadCode": "c", "fileName": "a.txt"}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error when the download URL returns a non-200")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention the status, got %v", err)
	}
}

func TestDownloadMedia_OversizedRejected(t *testing.T) {
	// Shrink the cap for the test rather than allocating a huge body.
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 0 // 0 -> default 100MB
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	// Serve more than the (test-lowered) limit. We set the limit via the model
	// var, so compute a body just over it — keep it small by using a 1MB cap.
	model.UploadMaxSizeMB = 1
	big := strings.Repeat("a", 2*1024*1024)
	mediaTestServer(t, big, http.StatusOK)
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "file",
		map[string]any{"downloadCode": "c", "fileName": "big.bin"}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error for an oversized attachment")
	}
	if !strings.Contains(err.Error(), "size limit") {
		t.Errorf("error should mention the size limit, got %v", err)
	}
}

func TestDownloadMedia_OversizedLeavesNoPartialFile(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	big := strings.Repeat("a", 2*1024*1024)
	mediaTestServer(t, big, http.StatusOK)
	mgr := newMediaManager(t)
	project := t.TempDir()

	_, _ = mgr.downloadMedia(context.Background(), "file",
		map[string]any{"downloadCode": "c", "fileName": "big.bin"}, project)

	uploads := filepath.Join(project, ".clawbench", "uploads")
	entries, err := os.ReadDir(uploads)
	if err != nil {
		if os.IsNotExist(err) {
			return // nothing was created at all — also acceptable
		}
		t.Fatalf("read uploads dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected oversized download must leave no file, found %d", len(entries))
	}
}

func TestResolveDownloadURL_TokenExpiredRetriesOnce(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"downloadUrl":"http://example.invalid/blob"}`))
	}))
	defer srv.Close()

	origFileURL := dingtalkMessageFileURL
	dingtalkMessageFileURL = srv.URL
	defer func() { dingtalkMessageFileURL = origFileURL }()

	// A valid cached token is what makes the 401 path reachable: on 401 the
	// manager invalidates the cache and refetches, so the token endpoint must
	// also be stubbed. Without a cached token the first call would refetch
	// before ever hitting the download API.
	origTokenURL := dingtalkTokenURL
	dingtalkTokenURL = srv.URL
	defer func() { dingtalkTokenURL = origTokenURL }()

	mgr := newMediaManager(t)
	url, err := mgr.resolveDownloadURL(context.Background(), "code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "http://example.invalid/blob" {
		t.Errorf("url = %q, want the retried download url", url)
	}
	// One 401 on the download API, one token refetch, one successful retry.
	if calls != 3 {
		t.Errorf("server calls = %d, want 3 (401 + token refetch + retry)", calls)
	}
}

func TestResolveDownloadURL_EmptyURLIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	origFileURL := dingtalkMessageFileURL
	dingtalkMessageFileURL = srv.URL
	defer func() { dingtalkMessageFileURL = origFileURL }()

	mgr := newMediaManager(t)
	if _, err := mgr.resolveDownloadURL(context.Background(), "code"); err == nil {
		t.Fatal("expected an error when downloadUrl is empty")
	}
}
