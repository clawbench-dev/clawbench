package wechat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ilink "github.com/openilink/openilink-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doerServer serves a fixed body for any path.
func doerServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func doRequest(t *testing.T, srv *httptest.Server, path string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader("{}"))
	require.NoError(t, err)
	return apiErrorDoer{inner: srv.Client()}.Do(req)
}

// TestAPIErrorDoer_TranslatesBodyError is the guard on the whole reason the
// wrapper exists: the SDK's send path only checks the HTTP status, so a
// 200 + {"ret":-1} would otherwise be reported as a delivered notification.
func TestAPIErrorDoer_TranslatesBodyError(t *testing.T) {
	srv := doerServer(t, `{"ret":-1,"errcode":40001,"errmsg":"bad token"}`)

	resp, err := doRequest(t, srv, "/ilink/bot/sendmessage")
	require.Error(t, err)
	assert.Nil(t, resp)

	var apiErr *ilink.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 40001, apiErr.ErrCode)
}

func TestAPIErrorDoer_PassesThroughSuccess(t *testing.T) {
	srv := doerServer(t, `{"ret":0}`)

	resp, err := doRequest(t, srv, "/ilink/bot/sendmessage")
	require.NoError(t, err)
	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	_ = resp.Body.Close()
	// The body must be restored for the SDK's own read, not consumed.
	assert.JSONEq(t, `{"ret":0}`, string(body))
}

// TestAPIErrorDoer_SkipsGetUpdates pins the -14 path: GetUpdates parses `ret`
// itself and needs to see -14, so an error here would turn an expired session
// into an ordinary poll failure with backoff.
func TestAPIErrorDoer_SkipsGetUpdates(t *testing.T) {
	srv := doerServer(t, `{"ret":-14,"errcode":-14}`)

	resp, err := doRequest(t, srv, "/ilink/bot/getupdates")
	require.NoError(t, err, "getupdates must not be error-translated")
	_ = resp.Body.Close()
}

// TestAPIErrorDoer_SkipsNonBotPaths keeps media downloads out of the wrapper:
// buffering a ciphertext body here would double peak memory per attachment.
func TestAPIErrorDoer_SkipsNonBotPaths(t *testing.T) {
	srv := doerServer(t, `{"ret":-1}`)

	resp, err := doRequest(t, srv, "/download/some-media")
	require.NoError(t, err, "CDN downloads must not be inspected")
	_ = resp.Body.Close()
}

// TestAPIErrorDoer_Non2xxLeftToSDK: the SDK turns these into *ilink.HTTPError
// itself, so the wrapper must not swallow or duplicate that.
func TestAPIErrorDoer_Non2xxLeftToSDK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ret":-1}`))
	}))
	defer srv.Close()

	resp, err := doRequest(t, srv, "/ilink/bot/sendmessage")
	require.NoError(t, err, "a non-2xx must pass through so the SDK can build its own error")
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}
