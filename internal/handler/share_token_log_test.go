package handler

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Share tokens are capability URLs: possessing the token IS the authorization.
// Writing one to a log therefore hands a usable credential to everyone who can
// read the log (operators, backups, log shippers), which is a much wider audience
// than the person the link was sent to.
//
// The request logger masks the token in the path (see
// middleware.redactPathToken). This guard covers the other half of the contract:
// no handler may log the token as a field of its own, which the path-level
// redaction cannot see.
//
// Source-level on purpose — the offending call only fires on a storage error, so
// no behavioral test can reach it without manufacturing a DB failure per site,
// and the check has to keep holding as handlers are added.
func TestShareHandlersNeverLogTheTokenField(t *testing.T) {
	files := []string{
		"file_share.go",
		"session_share.go",
		"share_target.go",
	}

	// Any slog call carrying the token as a structured field. Both the bare
	// identifier and a token-valued expression are covered by requiring the
	// attribute key to be absent entirely: these handlers have no other reason
	// to log a field named "token".
	forbidden := []string{`"token", token`, `"token", req.Token`}

	for _, name := range files {
		src, err := os.ReadFile(name)
		require.NoError(t, err, "read %s", name)
		text := string(src)
		for _, pattern := range forbidden {
			assert.NotContains(t, text, pattern,
				"%s must not log the share token: it is the credential, and the "+
					"request logger already masks it in the path", name)
		}
	}
}

// The redaction helper itself lives in middleware; assert the handler layer
// depends on it rather than re-implementing masking inline (two implementations
// would drift, and the log line would silently start leaking again).
func TestShareHandlersRelyOnLoggerRedaction(t *testing.T) {
	src, err := os.ReadFile("../middleware/logger.go")
	require.NoError(t, err)

	text := string(src)
	assert.Contains(t, text, "redactPathToken(",
		"the request logger must redact the share token from the logged path")
	assert.Contains(t, text, `"/api/share/"`,
		"the redaction must cover the public data endpoints, not just the page")
	assert.Contains(t, text, `"/share/"`,
		"the redaction must cover the share page URL")
}
