package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
)

// mediaTestServer stands up a fake Feishu OpenAPI host that serves the
// message-resource endpoint, and points the SDK's base URL at it.
func mediaTestServer(t *testing.T, body string) {
	t.Helper()

	mux := http.NewServeMux()
	// GET /open-apis/im/v1/messages/{message_id}/resources/{file_key}?type=...
	mux.HandleFunc("/open-apis/im/v1/messages/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("resource download must carry an Authorization header")
		}
		if !strings.Contains(r.URL.Path, "/resources/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("type") == "" {
			t.Errorf("resource download must specify the resource type")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	origBase := feishuOpenBaseURL
	feishuOpenBaseURL = srv.URL
	t.Cleanup(func() { feishuOpenBaseURL = origBase })
}

func newMediaManager(t *testing.T) *Manager {
	t.Helper()
	// Each test gets its own Manager, so the per-instance token cache needs no
	// cleanup — the cache is a field, not package state.
	mgr := NewManager(&model.FeishuConfig{AppID: "test-app", AppSecret: "test-secret"})
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)
	return mgr
}

func TestExtractMedia(t *testing.T) {
	tests := []struct {
		name         string
		msgType      string
		content      string
		wantOK       bool
		wantKey      string
		wantResType  string
		wantFilename string
	}{
		{
			name:    "file message",
			msgType: "file",
			content: `{"file_key":"file_abc","file_name":"报告.pdf"}`,
			wantOK:  true, wantKey: "file_abc", wantResType: "file", wantFilename: "报告.pdf",
		},
		{
			name:    "file message without file_name gets a fallback",
			msgType: "file",
			content: `{"file_key":"file_abc"}`,
			wantOK:  true, wantKey: "file_abc", wantResType: "file", wantFilename: "attachment",
		},
		{
			name:    "image message uses image_key and the image resource type",
			msgType: "image",
			content: `{"image_key":"img_xyz"}`,
			wantOK:  true, wantKey: "img_xyz", wantResType: "image", wantFilename: "image.png",
		},
		{
			name:    "text is not media",
			msgType: "text",
			content: `{"text":"hello"}`,
			wantOK:  false,
		},
		{
			name:    "post is not media",
			msgType: "post",
			content: `{"zh_cn":{"content":[]}}`,
			wantOK:  false,
		},
		{
			name:    "file message missing file_key",
			msgType: "file",
			content: `{"file_name":"x.pdf"}`,
			wantOK:  false,
		},
		{
			name:    "image message missing image_key",
			msgType: "image",
			content: `{}`,
			wantOK:  false,
		},
		{
			name:    "empty content",
			msgType: "file",
			content: "",
			wantOK:  false,
		},
		{
			name:    "malformed json",
			msgType: "file",
			content: `{not json`,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, resType, filename, ok := extractMedia(tt.msgType, tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if resType != tt.wantResType {
				t.Errorf("resType = %q, want %q", resType, tt.wantResType)
			}
			if filename != tt.wantFilename {
				t.Errorf("filename = %q, want %q", filename, tt.wantFilename)
			}
		})
	}
}

func TestDownloadMedia_Success(t *testing.T) {
	mediaTestServer(t, "file-bytes")
	mgr := newMediaManager(t)
	project := t.TempDir()

	entry, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"report.txt"}`, "om_msg_1", project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func TestDownloadMedia_Image(t *testing.T) {
	mediaTestServer(t, "png-bytes")
	mgr := newMediaManager(t)
	project := t.TempDir()

	entry, err := mgr.downloadMedia(context.Background(), "image",
		`{"image_key":"img_xyz"}`, "om_msg_1", project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Path != ".clawbench/uploads/image.png" {
		t.Errorf("path = %q, want .clawbench/uploads/image.png", entry.Path)
	}
}

func TestDownloadMedia_SanitizesTraversalFilename(t *testing.T) {
	mediaTestServer(t, "x")
	mgr := newMediaManager(t)
	project := t.TempDir()

	entry, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"../../../etc/passwd"}`, "om_msg_1", project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Path != ".clawbench/uploads/passwd" {
		t.Fatalf("path = %q, want .clawbench/uploads/passwd", entry.Path)
	}
	// Assert containment by absolute location: stat-ing a guessed traversal
	// target could hit a real system file and pass for the wrong reason.
	abs := filepath.Join(project, filepath.FromSlash(entry.Path))
	uploadsDir := filepath.Join(project, ".clawbench", "uploads")
	rel, err := filepath.Rel(uploadsDir, abs)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if rel != "passwd" {
		t.Errorf("saved file %q is not directly inside the uploads dir", rel)
	}
}

func TestDownloadMedia_MissingMessageIDIsRejected(t *testing.T) {
	mediaTestServer(t, "x")
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"a.txt"}`, "", t.TempDir())
	if err == nil {
		t.Fatal("expected an error when message_id is empty")
	}
	if !strings.Contains(err.Error(), "message_id") {
		t.Errorf("error should mention message_id, got %v", err)
	}
}

func TestDownloadMedia_NonMediaTypeIsRejected(t *testing.T) {
	mediaTestServer(t, "x")
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "text",
		`{"text":"hello"}`, "om_msg_1", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a non-media message type")
	}
}

func TestDownloadMedia_OversizedRejectedAndLeavesNoFile(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	big := strings.Repeat("a", 2*1024*1024)
	mediaTestServer(t, big)
	mgr := newMediaManager(t)
	project := t.TempDir()

	_, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"big.bin"}`, "om_msg_1", project)
	if err == nil {
		t.Fatal("expected an error for an oversized attachment")
	}
	if !strings.Contains(err.Error(), "size limit") {
		t.Errorf("error should mention the size limit, got %v", err)
	}

	uploads := filepath.Join(project, ".clawbench", "uploads")
	entries, rerr := os.ReadDir(uploads)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return
		}
		t.Fatalf("read uploads dir: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected oversized download must leave no file, found %d", len(entries))
	}
}

// TestDownloadMedia_ResourceErrorIsReported verifies an API-level failure is
// surfaced rather than read as success. The SDK derives Success() from the
// HTTP status (probed: a 200 with an error code still reports Success=true),
// so the failure has to arrive as a non-2xx status.
func TestDownloadMedia_ResourceErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":234001,"msg":"resource not found"}`))
	}))
	t.Cleanup(srv.Close)

	origBase := feishuOpenBaseURL
	feishuOpenBaseURL = srv.URL
	t.Cleanup(func() { feishuOpenBaseURL = origBase })

	mgr := newMediaManager(t)
	_, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"a.txt"}`, "om_msg_1", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a non-2xx resource response")
	}
	if !strings.Contains(err.Error(), "resource error") {
		t.Errorf("error should mention the resource error, got %v", err)
	}
}

// TestDownloadMedia_TokenFailureIsReported verifies a token-acquisition failure
// aborts before the resource call, so no half-built request reaches the API.
func TestDownloadMedia_TokenFailureIsReported(t *testing.T) {
	origBase := feishuOpenBaseURL
	feishuOpenBaseURL = "http://127.0.0.1:1/unreachable"
	t.Cleanup(func() { feishuOpenBaseURL = origBase })

	// Drop the cached token so getAccessToken must fetch one, which fails.
	mgr := NewManager(&model.FeishuConfig{AppID: "test-app", AppSecret: "test-secret"})

	_, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"a.txt"}`, "om_msg_1", t.TempDir())
	if err == nil {
		t.Fatal("expected an error when the tenant token cannot be obtained")
	}
	if !strings.Contains(err.Error(), "get token") {
		t.Errorf("error should mention the token step, got %v", err)
	}
}

// TestDownloadMedia_NoProjectPathIsRejected verifies the download fails cleanly
// when the target session has no project root.
func TestDownloadMedia_NoProjectPathIsRejected(t *testing.T) {
	mediaTestServer(t, "x")
	mgr := newMediaManager(t)

	_, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc","file_name":"a.txt"}`, "om_msg_1", "")
	if err == nil {
		t.Fatal("expected an error when the session has no project path")
	}
}

// TestDownloadMedia_FileWithoutNameGetsFallback verifies a file callback whose
// payload omits file_name still produces a usable attachment name.
func TestDownloadMedia_FileWithoutNameGetsFallback(t *testing.T) {
	mediaTestServer(t, "bytes")
	mgr := newMediaManager(t)
	project := t.TempDir()

	entry, err := mgr.downloadMedia(context.Background(), "file",
		`{"file_key":"file_abc"}`, "om_msg_1", project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Path != ".clawbench/uploads/attachment" {
		t.Errorf("path = %q, want .clawbench/uploads/attachment", entry.Path)
	}
}
