package wechat

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
	"github.com/stretchr/testify/assert"
)

// cdnServer serves AES-128-ECB encrypted bytes, the format DownloadMedia expects.
func cdnServer(t *testing.T, plaintext []byte) (*httptest.Server, string) {
	t.Helper()

	key := []byte("0123456789abcdef")
	ciphertext, err := ilink.EncryptAESECB(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ciphertext)
	}))
	t.Cleanup(srv.Close)

	// base64(raw 16 bytes) is the image encoding.
	return srv, base64.StdEncoding.EncodeToString(key)
}

func TestDownloadOne_ImageUsesFallbackName(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("PNGDATA"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	ref := mediaRef{
		kind:     "image",
		filename: "image.png",
		item: ilink.MessageItem{
			Type: ilink.ItemImage,
			ImageItem: &ilink.ImageItem{
				Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1},
			},
		},
	}

	entry, err := mgr.downloadOne(context.Background(), ref, projectPath, common.AttachmentMaxBytes())
	if err != nil {
		t.Fatalf("downloadOne failed: %v", err)
	}
	if filepath.Ext(entry.Path) != ".png" {
		t.Errorf("saved as %q, want a .png", entry.Path)
	}
	got, err := os.ReadFile(filepath.Join(projectPath, entry.Path))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != "PNGDATA" {
		t.Errorf("content = %q, want the decrypted payload", got)
	}
}

func TestDownloadOne_FileUsesProvidedName(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("PDFDATA"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	ref := mediaRef{
		kind:     "file",
		filename: "report.pdf",
		item: ilink.MessageItem{
			Type: ilink.ItemFile,
			FileItem: &ilink.FileItem{
				Media:    &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1},
				FileName: "report.pdf",
			},
		},
	}

	entry, err := mgr.downloadOne(context.Background(), ref, projectPath, common.AttachmentMaxBytes())
	if err != nil {
		t.Fatalf("downloadOne failed: %v", err)
	}
	if filepath.Base(entry.Path) != "report.pdf" {
		t.Errorf("saved as %q, want report.pdf", filepath.Base(entry.Path))
	}
}

// TestDownloadAll_RejectsDeclaredOversize pins the pre-download check: the SDK
// buffers the whole payload in memory, so the only thing standing between a huge
// attachment and a huge allocation is refusing on the declared size.
func TestDownloadAll_RejectsDeclaredOversize(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true

	huge := common.AttachmentMaxBytes() + 1
	refs := []mediaRef{{
		kind:         "file",
		filename:     "big.bin",
		declaredSize: huge,
		item: ilink.MessageItem{
			Type:     ilink.ItemFile,
			FileItem: &ilink.FileItem{Media: &ilink.CDNMedia{EncryptQueryParam: "q", AESKey: "k"}},
		},
	}}

	entries, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
	if out.failed != 1 || !out.tooLarge {
		t.Errorf("outcome = %+v, want 1 failed with tooLarge", out)
	}
}

// TestDownloadAll_PartialFailureKeepsGoodOnes: one bad attachment must not
// discard a message that also carried a valid one.
func TestDownloadAll_PartialFailureKeepsGoodOnes(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("OK"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	refs := []mediaRef{
		{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey}},
			},
		},
		{
			kind: "image", filename: "broken.png",
			item: ilink.MessageItem{
				Type: ilink.ItemImage,
				// No URL and no query param: resolution fails.
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{AESKey: aesKey}},
			},
		},
	}

	entries, out := mgr.downloadAll(context.Background(), refs, projectPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 successful entry, got %d", len(entries))
	}
	if out.failed != 1 || out.sent != 1 {
		t.Errorf("outcome = %+v, want 1 sent and 1 failed", out)
	}
}

func TestDownloadAll_TruncatesToCap(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	// One more than the cap, all broken so nothing is actually downloaded.
	n := common.AttachmentMaxFiles() + 1
	refs := make([]mediaRef, 0, n)
	for range n {
		refs = append(refs, mediaRef{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{}},
			},
		})
	}

	_, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	if !out.truncated {
		t.Error("expected the outcome to report truncation")
	}
	if out.failed != common.AttachmentMaxFiles() {
		t.Errorf("attempted %d, want the cap %d", out.failed, common.AttachmentMaxFiles())
	}
}

// TestDownloadAll_ExactlyAtCapIsNotTruncated pins the boundary: flipping the
// comparison to >= would silently drop the last attachment of a message that
// fits exactly, and the +1 fixture above cannot tell the two apart.
func TestDownloadAll_ExactlyAtCapIsNotTruncated(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	n := common.AttachmentMaxFiles()
	refs := make([]mediaRef, 0, n)
	for range n {
		refs = append(refs, mediaRef{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{}},
			},
		})
	}

	_, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	assert.False(t, out.truncated, "exactly at the cap must not be reported as truncated")
	assert.Equal(t, n, out.failed, "every attachment must be attempted")
}
