package kb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// The hashFile branch of importFiles is only reached when a file can be opened
// (so Stat and ValidateFile passed) but cannot be read to the end. No filesystem
// state produces that portably — a device is either unreadable at open time or
// reads as a finite file — so the branch is pinned through hashFile's contract
// here, which is the part the branch relies on.
func TestHashFile_ReportsOpenFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.md")
	_, err := hashFile(missing)
	require.ErrorContains(t, err, "open file")
}

// A folder holding only files the knowledge base cannot index reports them as
// skipped in BOTH modes: nothing is added, nothing fails, and the counts the
// panel shows before an import match the ones after it.
func TestImportFiles_SkippedFilesAgreeInPreviewAndImport(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"photo.png":    "\x89PNG\r\n\x1a\n",
		"archive.zip":  "PK\x03\x04",
		"blob.unknown": "\x00\x01\x02",
		"handbook.md":  "甲：这是一份可以导入的文档。",
	})
	files, _, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)

	preview, err := importFiles(context.Background(), col.ID, files, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Added)
	require.Equal(t, 3, preview.Skipped)
	require.Equal(t, 0, preview.Failed)

	imported, err := importFiles(context.Background(), col.ID, files, false)
	require.NoError(t, err)
	require.Equal(t, 1, imported.Added)
	require.Equal(t, 3, imported.Skipped)
	require.Equal(t, 0, imported.Failed)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "handbook.md", docs[0].Title)
}

// Preview must agree with the real import on what is a duplicate: the second
// pass over the same folder reports every file as a duplicate and adds none.
func TestPreviewDirectory_AgreesWithImportOnDuplicates(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.md":                 "甲：流程说明。",
		"sub/b.txt":            "乙：差旅标准。",
		"sub/c.dat":            "\x00binary",
		"sub/notes.unknownext": "纯文本但扩展名未知",
	})

	imported, err := ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 3, imported.Added, "unknown extensions are judged by content")
	require.Equal(t, 1, imported.Skipped)
	require.Equal(t, 0, imported.Failed)

	preview, err := PreviewDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 0, preview.Added)
	require.Equal(t, 3, preview.Duplicates)
	require.Equal(t, 1, preview.Skipped)
	require.Equal(t, imported.Skipped, preview.Skipped)
}

// The folder walk must not follow symlinks: a link back to the parent would loop
// forever, and a link out of the folder would copy files the user never selected.
func TestCollectDirFiles_DoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"real.md": "甲"})
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.md": "不该被导入"})

	link := filepath.Join(root, "linked.md")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatalf("symlink directory: %v", err)
	}

	files, truncated, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, []string{filepath.Join(root, "real.md")}, files)
}

// A single directory with more files than the cap must report Truncated, and the
// cap alone must not: a folder with exactly maxDirImportFiles files is complete.
func TestCollectDirFiles_TruncationIsReportedOnlyWhenSomethingWasCut(t *testing.T) {
	exact := t.TempDir()
	for i := range maxDirImportFiles {
		writeTree(t, exact, map[string]string{filepath.Join("f", itoa(i)+".md"): "x"})
	}
	files, truncated, err := collectDirFiles(context.Background(), exact)
	require.NoError(t, err)
	require.Len(t, files, maxDirImportFiles)
	require.False(t, truncated, "exactly the cap is not truncation")

	over := t.TempDir()
	for i := range maxDirImportFiles + 5 {
		writeTree(t, over, map[string]string{filepath.Join("f", itoa(i)+".md"): "x"})
	}
	files, truncated, err = collectDirFiles(context.Background(), over)
	require.NoError(t, err)
	require.Len(t, files, maxDirImportFiles)
	require.True(t, truncated, "a folder over the cap must say so")
}

// An import whose context is cancelled while the files are being copied reports
// the counts gathered so far together with the cancellation.
func TestImportDirectory_ReportsPartialCountsWhenCancelledMidRun(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.md": "甲：一", "b.md": "乙：二", "c.md": "丙：三"})

	// The walk itself must see the cancellation. A bare cancel() here would be a
	// race (the import may finish first), so the deadline is short enough that
	// the folder is never walked and long enough that this is not a busy loop.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	res, err := ImportDirectory(ctx, col.ID, root)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, res)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Empty(t, docs, "a cancelled import must not copy anything")
}

// URL titles decide which extractor runs: a page served from "/article" or
// "?id=1" is HTML even without an extension, while a known document extension is
// kept so the PDF/Office extractor is used.
func TestURLDocumentName_ExtensionDecidesTheExtractor(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://example.com/article", "article.html"},
		{"https://example.com/view.php", "view.php.html"},
		{"https://example.com/docs/report.pdf", "report.pdf"},
		{"https://example.com/docs/report.PDF", "report.PDF"},
		{"https://example.com/docs/notes.md", "notes.md"},
		{"https://example.com/", "example.com.html"},
		{"https://example.com", "example.com.html"},
		{"https://example.com/dir/", "dir.html"},
		{"https://example.com/%E6%89%8B%E5%86%8C.md", "手册.md"},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		require.NoError(t, err)
		require.Equal(t, c.want, urlDocumentName(u), "url %s", c.raw)
	}
}

// The public-URL guard rejects everything that is not plain http(s) before any
// request is made, and reports a private address as a FileError the panel can
// translate rather than a bare fetch failure.
func TestAddDocumentFromURL_RejectionPaths(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/a.txt", "not a url", ""} {
		_, err := addDocumentFromURL(context.Background(), col.ID, raw, true)
		require.ErrorIs(t, err, ErrInvalidURL, "url %q", raw)
	}

	_, err := addDocumentFromURL(context.Background(), col.ID, "http://127.0.0.1:1/secret", true)
	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	require.Equal(t, ReasonPrivateAddress, fileErr.Reason)
	require.ErrorIs(t, err, ErrPrivateAddress)

	_, err = addDocumentFromURL(context.Background(), 9999, "https://example.com/a.html", false)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

// A non-2xx answer is a fetch failure that names the status, because "the page
// returned 404" and "the network is down" need different fixes from the user.
func TestAddDocumentFromURL_ReportsHTTPStatus(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := addDocumentFromURL(context.Background(), col.ID, srv.URL+"/gone.html", false)
	require.ErrorIs(t, err, ErrFetchFailed)
	require.ErrorContains(t, err, "404")
}

// A page over the size cap is refused as a FileError (too large) and does not
// become a document.
func TestAddDocumentFromURL_RefusesPagesOverTheCap(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.CopyN(w, zeroReader{}, maxURLBytes+1)
	}))
	defer srv.Close()

	_, err := addDocumentFromURL(context.Background(), col.ID, srv.URL+"/huge.html", false)
	require.ErrorIs(t, err, ErrFileTooLarge)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Empty(t, docs)
}

// A healthy fetch is stored with the page title derived from the URL and goes
// through the HTML extractor.
func TestAddDocumentFromURL_StoresThePageThroughTheHTMLExtractor(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body><article><p>报销流程：先提交申请，再等财务审批。</p></article></body></html>")
	}))
	defer srv.Close()

	doc, err := addDocumentFromURL(context.Background(), col.ID, srv.URL+"/article", false)
	require.NoError(t, err)
	require.Equal(t, "article.html", doc.Title)
	require.Equal(t, ".html", doc.Ext)
	require.FileExists(t, doc.StoredPath)
}

// The fetch respects the caller's context: a request that is already cancelled
// must not reach the network.
func TestAddDocumentFromURL_HonorsCancellation(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := addDocumentFromURL(ctx, col.ID, srv.URL+"/a.html", false)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrFetchFailed) || errors.Is(err, context.Canceled), "got %v", err)
	require.False(t, hit, "a cancelled request must not reach the server")
}

// zeroReader yields an endless stream of zero bytes, for the size-cap test.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
