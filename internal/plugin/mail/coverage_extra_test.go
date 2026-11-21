package mail

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	mailcore "clawbench/internal/mail"

	"github.com/stretchr/testify/assert"
)

// A server that refuses a command until the client identifies itself is an
// upstream problem (502 with its own key), not something the user can fix by
// re-entering their password.
func TestWriteMailError_ClientIDRequiredIsUpstreamFailure(t *testing.T) {
	err := &mailcore.Error{Kind: mailcore.KindClientID, Op: "send", Err: errors.New("server requires CLIENTID")}
	rec := httptest.NewRecorder()
	writeMailError(rec, httptest.NewRequest(http.MethodPost, "/api/mail/send", http.NoBody), err)
	assert.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "MailClientIDRequired")
}

// ── providers: the model-list conflict ─────────────────────────────────────

// Emptying a provider's model list breaks every config-directory agent bound to
// it (the file no longer declares any model the CLI can resolve), so the edit is
// refused with a 409 that NAMES those agents: without the names the user cannot
// know which agent to rebind.
