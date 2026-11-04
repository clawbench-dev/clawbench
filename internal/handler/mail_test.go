package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMailHandlerEnv(t *testing.T) func() {
	t.Helper()
	_, teardown := setupTestEnv(t)
	// setupTestEnv builds a minimal schema; the mail tables are ours.
	_, err := store.WriteExec(service.MailDDL)
	require.NoError(t, err)
	return teardown
}

func seedMailAccount(t *testing.T) service.MailAccount {
	t.Helper()
	acc, err := service.CreateMailAccount(service.MailAccount{
		Name: "工作", Email: "me@example.com", Username: "me@example.com",
		Password: "top-secret", IMAPHost: "imap.example.com", IMAPPort: 993,
		IMAPSecurity: "tls", SMTPHost: "smtp.example.com", SMTPPort: 465,
		SMTPSecurity: "tls", Enabled: true,
	})
	require.NoError(t, err)
	return acc
}

func TestServeMailAccounts_NeverLeaksPassword(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)

	w := callHandler(ServeMailAccounts, newRequest(t, http.MethodGet, "/api/mail/accounts", nil))
	assertStatus(t, w, http.StatusOK)

	body := w.Body.String()
	assert.NotContains(t, body, "top-secret", "the list must never carry the stored password")
	assert.Contains(t, body, `"hasPassword":true`)

	var resp struct {
		Accounts []service.MailAccount `json:"accounts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Accounts, 1)
	assert.Equal(t, acc.ID, resp.Accounts[0].ID)

	// The explicit reveal is the only path that carries it.
	w = callHandler(ServeMailAccountSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/accounts/1?reveal=1", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "top-secret")
}

func TestServeMailAccounts_CreateFillsDefaults(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailAccounts, newRequest(t, http.MethodPost, "/api/mail/accounts", map[string]any{
		"email":    "me@example.com",
		"password": "pw",
		"imapHost": "imap.example.com",
		"smtpHost": "smtp.example.com",
	}))
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Account service.MailAccount `json:"account"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "me@example.com", resp.Account.Username, "username defaults to the address")
	assert.Equal(t, 993, resp.Account.IMAPPort)
	assert.Equal(t, 465, resp.Account.SMTPPort)
	assert.Equal(t, "tls", resp.Account.IMAPSecurity)
	assert.True(t, resp.Account.Enabled)
}

func TestServeMailAccounts_RejectsInvalidAddress(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailAccounts, newRequest(t, http.MethodPost, "/api/mail/accounts", map[string]any{
		"email":    "not-an-address",
		"imapHost": "imap.example.com",
		"smtpHost": "smtp.example.com",
	}))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestServeMailMessages_RequiresFolderID(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailMessages, newRequest(t, http.MethodGet, "/api/mail/messages", nil))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeMailMessageSubRoutes_UnknownIDIs404(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/999", nil))
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeMailPresets_ListsKnownProviders(t *testing.T) {
	w := callHandler(ServeMailPresets, newRequest(t, http.MethodGet, "/api/mail/presets", nil))
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Presets []struct {
			ID       string `json:"id"`
			IMAPHost string `json:"imapHost"`
			SMTPHost string `json:"smtpHost"`
		} `json:"presets"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Presets)
	var qq bool
	for _, p := range resp.Presets {
		if p.ID == "qq" {
			qq = true
			assert.Equal(t, "imap.qq.com", p.IMAPHost)
			assert.Equal(t, "smtp.qq.com", p.SMTPHost)
		}
	}
	assert.True(t, qq, "qq preset must be offered")
}
