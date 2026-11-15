package kb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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

	doc, err := AddDocumentFromURL(col.ID, srv.URL+"/policy")
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
	_, err = AddDocumentFromURL(col.ID, "file:///etc/passwd")
	require.ErrorIs(t, err, ErrInvalidURL)
	_, err = AddDocumentFromURL(col.ID, "not a url")
	require.ErrorIs(t, err, ErrInvalidURL)

	// A non-2xx response is a fetch failure, not a bad URL.
	_, err = AddDocumentFromURL(col.ID, srv.URL+"/missing")
	require.ErrorIs(t, err, ErrFetchFailed)

	// A URL that ends in a known document extension keeps it.
	srvPDF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "%PDF-1.4")
	}))
	defer srvPDF.Close()
	pdfDoc, err := AddDocumentFromURL(col.ID, srvPDF.URL+"/manual.pdf")
	require.NoError(t, err)
	require.Equal(t, "manual.pdf", pdfDoc.Title)
}
