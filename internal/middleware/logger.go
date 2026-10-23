package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ResponseWriter wraps http.ResponseWriter to capture status code and bytes written.
type ResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *ResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// Flush implements http.Flusher so that SSE and other streaming responses
// can flush data through the wrapped ResponseWriter.
func (rw *ResponseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap returns the underlying http.ResponseWriter for interface compatibility
// (e.g., http.Flusher, http.Hijacker). Go 1.20+ http.ResponseController
// uses this method to access underlying interfaces.
func (rw *ResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// shareManagementSegments are the fixed first segments under /api/share/ that
// are AUTHENTICATED management routes, not capability tokens:
//
//	/api/share/session          create / query / revoke one conversation share
//	/api/share/session/list     list / clear this project's shares
//	/api/share/list             list / clear this project's file shares
//
// They must be logged verbatim. Treating them as tokens would mask the very
// endpoints an operator most needs to tell apart (create vs revoke vs clear-all),
// and it cannot leak anything: they are not credentials.
var shareManagementSegments = map[string]bool{
	"session": true,
	"list":    true,
}

// redactPathToken masks the capability token in a share URL path.
//
// Share links are capability URLs: the token in the path IS the credential, so a
// log line carrying it is a usable credential for anyone who can read the log
// (operators, backups, log shippers). The path is still worth logging — it says
// which endpoint was hit — so the token is truncated rather than dropped, keeping
// enough prefix to correlate two lines for the same link without being enough to
// use it.
//
// Covers both the page (/share/{token}) and the data endpoints
// (/api/share/{token}/...), while leaving the management routes intact. Returns
// the path unchanged when it is not a token-bearing share route.
func redactPathToken(path string) string {
	// /api/share/... is the only prefix that mixes tokens and fixed routes.
	if rest, ok := strings.CutPrefix(path, "/api/share/"); ok {
		first, tail, hasTail := strings.Cut(rest, "/")
		if first == "" || shareManagementSegments[first] {
			return path
		}
		return "/api/share/" + maskToken(first) + tailSuffix(tail, hasTail)
	}

	if rest, ok := strings.CutPrefix(path, "/share/"); ok {
		token, tail, hasTail := strings.Cut(rest, "/")
		if token == "" {
			return path
		}
		return "/share/" + maskToken(token) + tailSuffix(tail, hasTail)
	}

	return path
}

// tailSuffix re-attaches the remainder of a path after the masked segment.
func tailSuffix(tail string, hasTail bool) string {
	if !hasTail {
		return ""
	}
	return "/" + tail
}

// maskToken keeps a short prefix so related log lines can still be grouped,
// while removing everything needed to actually present the token.
func maskToken(token string) string {
	const keep = 6
	if len(token) <= keep {
		// Too short to split safely: mask it whole rather than logging it.
		return "…"
	}
	return token[:keep] + "…"
}

// RequestLogger logs method, path, status, duration, client IP, trace_id via slog.
func RequestLogger(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &ResponseWriter{ResponseWriter: w, status: http.StatusOK}
		reqID := GetRequestID(r.Context())
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		slog.Info(
			"http request",
			slog.String("method", r.Method),
			slog.String("path", redactPathToken(r.URL.Path)),
			slog.Int("status", rw.status),
			slog.Duration("duration", duration),
			slog.String("ip", r.RemoteAddr),
			slog.String("trace_id", reqID),
		)
	}
}
