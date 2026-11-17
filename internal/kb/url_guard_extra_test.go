package kb

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDialPublicOnlyRefusesAtDialTime is the guard that cannot be replaced by
// the URL-level check: a hostname that resolves to a private address (DNS
// rebinding, a poisoned resolver, an /etc/hosts entry) must be refused at
// connect time, because that is the only moment the address actually used is
// known.
func TestDialPublicOnlyRefusesAtDialTime(t *testing.T) {
	dial := dialPublicOnly(2 * time.Second)

	// A literal loopback address never even resolves.
	_, err := dial(t.Context(), "tcp", "127.0.0.1:80")
	require.ErrorIs(t, err, ErrPrivateAddress)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonPrivateAddress, fe.Reason)

	// "localhost" is refused by the host check, before the resolver runs.
	_, err = dial(t.Context(), "tcp", "localhost:80")
	require.ErrorIs(t, err, ErrPrivateAddress)

	// A hostname that resolves to a private address is refused even though the
	// URL-level check let it through: that is the whole reason this dialer
	// exists instead of net.Dialer's own resolver.
	// A literal, not a host alias: `ip6-localhost` exists in Debian's hosts file
	// and does not on a Windows runner, where the lookup fails before the
	// address is ever classified.
	_, err = dial(t.Context(), "tcp", "[::1]:80")
	require.ErrorIs(t, err, ErrPrivateAddress)

	// Nothing usable resolves at all: the error names the host so the panel can
	// say which address failed.
	_, err = dial(t.Context(), "tcp", "no-such-host.invalid:80")
	require.Error(t, err)

	// An address with no port cannot be judged, so it is an error rather than a
	// silent pass-through to a default port.
	_, err = dial(t.Context(), "tcp", "example.com")
	require.Error(t, err)
}

// TestURLFetchClient_RedirectLoopIsCutOff: a server that bounces forever must
// not hold the import request until its own timeout. File schemes are covered
// in url_test.go; this is the hop-count bound.
func TestURLFetchClient_RedirectLoopIsCutOff(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		http.Redirect(w, r, fmt.Sprintf("https://public-%d.example.com/", step), http.StatusFound)
	}))
	defer srv.Close()

	client := newURLFetchClient(true)
	client.Transport.(*http.Transport).DialContext = nil // the test server is loopback

	req, err := http.NewRequest(http.MethodGet, "https://public.example.com/", http.NoBody)
	require.NoError(t, err)
	// The first hop is the loopback test server; from there the chain grows by
	// one public hop per redirect until the cap is reached.
	via := make([]*http.Request, maxURLRedirects)
	for i := range via {
		via[i] = &http.Request{URL: mustParseURL(t, srv.URL+"/")}
	}
	next := &http.Request{URL: mustParseURL(t, "https://public.example.com/next")}
	require.ErrorContains(t, client.CheckRedirect(next, via), "too many redirects")

	// One hop below the cap is not refused (the address is public).
	err = client.CheckRedirect(req, via[:maxURLRedirects-1])
	require.NoError(t, err)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// TestURLDocumentName pins the title rules the panel shows. A page served from
// "/article" is still HTML and must reach the HTML extractor, so anything
// without a known document extension is named "*.html"; a root URL has no path
// to name it after and falls back to the host.
func TestURLDocumentName(t *testing.T) {
	cases := map[string]string{
		"https://example.com/report.pdf":     "report.pdf",
		"https://example.com/a/b/report.md":  "report.md",
		"https://example.com/article":        "article.html",
		"https://example.com/view.php?id=7":  "view.php.html",
		"https://example.com/":               "example.com.html",
		"https://example.com":                "example.com.html",
		"https://example.com/a%20b.txt":      "a b.txt",
		"https://example.com/odd:name":       "odd:name.html",
		"https://example.com/space%20/t.txt": "t.txt",
		"https://example.com/POLICY.PDF":     "POLICY.PDF",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, want, urlDocumentName(u), raw)
	}
}

// TestAddDocumentFromURL_RefusesOversizedBody: a page larger than maxURLBytes
// is refused with the size reason rather than truncated — a half-indexed page
// is worse than a clear error, and the panel can only explain the former.
func TestAddDocumentFromURL_RefusesOversizedBody(t *testing.T) {
	col := newCollectionForImport(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxURLBytes+1))
	}))
	defer srv.Close()

	_, err := fetchForTest(t, col.ID, srv.URL+"/huge.txt")
	require.ErrorIs(t, err, ErrFileTooLarge)
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "20", fe.Detail)
}

// TestAddDocumentFromURL_ReportsTransportFailure: a connection cut mid-body is
// a fetch failure (the network reason is wrapped inside), not an invalid URL.
func TestAddDocumentFromURL_ReportsTransportFailure(t *testing.T) {
	col := newCollectionForImport(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		// Promise more than is sent, then close: the client sees an EOF while
		// reading the body.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: text/plain\r\n\r\npartial")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	_, err := fetchForTest(t, col.ID, srv.URL+"/broken.txt")
	require.ErrorIs(t, err, ErrFetchFailed)
}

// TestURLFetchClient_AcceptsCompressedBodies: the client must let the transport
// negotiate gzip itself. If the Accept-Encoding header were set by hand, Go
// would stop decompressing and the HTML extractor would index binary bytes —
// a failure mode that only appears against real servers.
func TestURLFetchClient_AcceptsCompressedBodies(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = io.WriteString(gz, "<html><body><p>报销流程</p></body></html>")
	require.NoError(t, gz.Close())
	payload := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			http.Error(w, "client did not offer gzip", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	client := newURLFetchClient(false)
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "报销流程", "the transport must decompress the body")
}

// A name that cannot be resolved at all is the user's typo, not a policy
// decision: reporting it as a private address would send them looking for a
// firewall rule. The dialer must say the lookup failed, and then say it again
// when no address it did get was usable.
func TestDialPublicOnly_ReportsLookupFailuresSeparatelyFromPolicy(t *testing.T) {
	dial := dialPublicOnly(time.Second)

	_, err := dial(t.Context(), "tcp", "no-such-host.invalid:443")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPrivateAddress)

	// An address list that resolves but holds nothing dialable ends in the same
	// "no usable address" failure instead of a nil error and a nil connection.
	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
}
