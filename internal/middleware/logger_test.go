package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestLogger(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})

	wrapped := RequestLogger(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}

func TestRequestLogger_CapturesStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	wrapped := RequestLogger(handler)

	req := httptest.NewRequest(http.MethodGet, "/missing", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRequestLogger_CapturesBytesWritten(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("some response data"))
	})

	wrapped := RequestLogger(handler)

	req := httptest.NewRequest(http.MethodPost, "/submit", http.NoBody)
	rec := httptest.NewRecorder()

	wrapped(rec, req)

	assert.Equal(t, "some response data", rec.Body.String())
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &ResponseWriter{ResponseWriter: rec, status: http.StatusOK}

	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rw.status)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &ResponseWriter{ResponseWriter: rec, status: http.StatusOK}

	n, err := rw.Write([]byte("test data"))
	assert.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Equal(t, 9, rw.bytes)
	assert.Equal(t, "test data", rec.Body.String())
}

func TestResponseWriter_WriteAccumulates(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &ResponseWriter{ResponseWriter: rec, status: http.StatusOK}

	rw.Write([]byte("hello "))
	rw.Write([]byte("world"))
	assert.Equal(t, 11, rw.bytes) // "hello " (6) + "world" (5)
}

// ─── Share-token redaction ───────────────────────────────────────────────────
//
// A share link is a capability URL: the token in the path is the credential, so
// a log line carrying it is a usable credential for anyone who can read the log
// (operators, backups, log shippers). These cases pin both halves of the
// contract — the token must not survive, and the rest of the path must, or the
// log stops answering "which endpoint was hit".

func TestRedactPathToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "share page",
			path: "/share/" + token,
			want: "/share/012345…",
		},
		{
			name: "session payload endpoint",
			path: "/api/share/" + token + "/session",
			want: "/api/share/012345…/session",
		},
		{
			name: "meta endpoint",
			path: "/api/share/" + token + "/meta",
			want: "/api/share/012345…/meta",
		},
		{
			name: "local asset subpath keeps the tail",
			path: "/api/share/" + token + "/local/img/a.png",
			want: "/api/share/012345…/local/img/a.png",
		},
		{
			name: "unrelated path is untouched",
			path: "/api/agents",
			want: "/api/agents",
		},
		{
			name: "path that merely starts with share",
			path: "/shared-notes",
			want: "/shared-notes",
		},
		{
			name: "prefix with no token",
			path: "/api/share/",
			want: "/api/share/",
		},
		{
			name: "prefix with no token and a tail",
			path: "/api/share//session",
			want: "/api/share//session",
		},
		{
			name: "short token is masked whole",
			path: "/share/abc",
			want: "/share/…",
		},
		{
			name: "exactly the keep length is masked whole",
			path: "/share/012345",
			want: "/share/…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, redactPathToken(tc.path))
		})
	}
}

// The regression that matters: no token of realistic length may appear in the
// output. Asserted on the real generator's output rather than a fixture, so a
// future change to token length cannot quietly slip past the 6-char prefix.
func TestRedactPathToken_LeavesNoUsableToken(t *testing.T) {
	token, err := service.GenerateShareToken()
	require.NoError(t, err)
	require.Len(t, token, 32)

	for _, path := range []string{
		"/share/" + token,
		"/api/share/" + token + "/session",
	} {
		got := redactPathToken(path)
		assert.NotContains(t, got, token, "the full token must never be logged")
		// The surviving prefix must be far too short to guess the rest from.
		assert.NotContains(t, got, token[:7], "more than the correlation prefix leaked")
	}
}

// End to end through the middleware: the recorded line must carry the redacted
// path, not the raw one. Without this the helper could be correct and unused.
func TestRequestLogger_RedactsShareToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"

	var logged string
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captureWriter{&logged}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := RequestLogger(handler)

	req := httptest.NewRequest(http.MethodGet, "/share/"+token, http.NoBody)
	wrapped(httptest.NewRecorder(), req)

	require.NotEmpty(t, logged, "the middleware must have logged something")
	assert.NotContains(t, logged, token, "the raw token must not reach the log")
	assert.Contains(t, logged, "/share/012345…", "the redacted path must be what is logged")
}

// captureWriter collects everything written to it so a test can assert on the
// actual log output instead of on an intermediate value.
type captureWriter struct{ buf *string }

func (w *captureWriter) Write(p []byte) (int, error) {
	*w.buf += string(p)
	return len(p), nil
}
