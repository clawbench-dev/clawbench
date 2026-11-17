package kb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fetchForTest drives the fetch path against an httptest server, which always
// listens on loopback: the SSRF filter has to be off for that to reach it.
func fetchForTest(t *testing.T, collectionID int64, raw string) (*Document, error) {
	t.Helper()
	return addDocumentFromURL(context.Background(), collectionID, raw, false)
}

func TestAddDocumentFromURL(t *testing.T) {
	col := newCollectionForImport(t)

	page := `<html><head><title>制度</title></head><body><h1>报销流程</h1><p>先提交申请，再等财务审批。</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	}))
	defer srv.Close()

	doc, err := fetchForTest(t, col.ID, srv.URL+"/policy")
	require.NoError(t, err)
	// A page without a document extension is treated as HTML.
	require.Equal(t, "policy.html", doc.Title)

	blocks, _, err := ExtractFile(doc.StoredPath, doc.Ext, 1<<20)
	require.NoError(t, err)
	var text strings.Builder
	for _, b := range blocks {
		text.WriteString(b.Text)
	}
	require.Contains(t, text.String(), "报销流程")
	require.NotContains(t, text.String(), "<h1>")

	// Non-http schemes are refused before any network call.
	_, err = fetchForTest(t, col.ID, "file:///etc/passwd")
	require.ErrorIs(t, err, ErrInvalidURL)
	_, err = fetchForTest(t, col.ID, "not a url")
	require.ErrorIs(t, err, ErrInvalidURL)

	// A non-2xx response is a fetch failure, not a bad URL.
	_, err = fetchForTest(t, col.ID, srv.URL+"/missing")
	require.ErrorIs(t, err, ErrFetchFailed)

	// A URL that ends in a known document extension keeps it.
	srvPDF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.4")
	}))
	defer srvPDF.Close()
	pdfDoc, err := fetchForTest(t, col.ID, srvPDF.URL+"/manual.pdf")
	require.NoError(t, err)
	require.Equal(t, "manual.pdf", pdfDoc.Title)
}

// TestAddDocumentFromURL_RejectsPrivateTargets pins the SSRF guard on the
// path that never dials anything: a literal private host is refused before the
// request is built. The content would otherwise be indexed and auto-injected
// into AI prompts, which makes "fetch an arbitrary URL" a read primitive.
func TestAddDocumentFromURL_RejectsPrivateTargets(t *testing.T) {
	col := newCollectionForImport(t)

	for _, raw := range []string{
		"http://127.0.0.1:8080/admin",
		"http://localhost/",
		"http://10.0.0.5/",
		"http://172.20.1.1/",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"http://[fe80::1]/",
		"http://[fd00::1]/",
	} {
		_, err := AddDocumentFromURL(context.Background(), col.ID, raw)
		require.ErrorIs(t, err, ErrPrivateAddress, raw)
		var fe *FileError
		require.ErrorAs(t, err, &fe, raw)
		require.Equal(t, ReasonPrivateAddress, fe.Reason, raw)
	}
}

// TestURLFetchClient_RedirectGuard exercises the client directly: a request
// that starts at a loopback test server must be refused the moment it is asked
// to follow a hop into the LAN, so the metadata service never answers.
func TestURLFetchClient_RedirectGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/to-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	defer srv.Close()

	client := newURLFetchClient(true)
	// The first hop is loopback (the test server). A transport that dials
	// normally is the only way to reach it, so the connection itself is allowed
	// here and the assertion is about CheckRedirect.
	client.Transport.(*http.Transport).DialContext = nil

	resp, err := client.Get(srv.URL + "/to-metadata")
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPrivateAddress)

	resp, err = client.Get(srv.URL + "/to-file")
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-http")
}
