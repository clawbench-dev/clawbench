package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/mail"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMailMessage inserts one cached message and returns its row. Seeding goes
// through the service layer rather than the IMAP client: these tests are about
// the HTTP surface, and the sync path has its own tests.
func seedMailMessage(t *testing.T, accountID int64) (service.MailFolder, service.MailMessage) {
	t.Helper()
	folderID, err := service.UpsertMailFolder(service.MailFolder{
		AccountID: accountID, Path: "INBOX", Name: "INBOX", Delimiter: "/",
		SpecialUse: "inbox", Unread: 1, Total: 1,
	})
	require.NoError(t, err)
	folder, err := service.GetMailFolder(folderID)
	require.NoError(t, err)

	rows, err := service.UpsertMailMessages([]service.MailMessage{{
		AccountID: accountID, FolderID: folderID, UID: 1, MessageID: "m1@example.com",
		Subject: "周报", FromName: "Boss", FromAddr: "boss@example.com",
		To:     []service.MailAddr{{Name: "Me", Addr: "me@example.com"}},
		SentAt: 1700000000, Size: 42, Snippet: "摘要",
	}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return folder, rows[0]
}

func TestServeMailPresets_MethodGuard(t *testing.T) {
	w := callHandler(ServeMailPresets, newRequest(t, http.MethodPost, "/api/mail/presets", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailAccounts_EmptyListIsAnArray(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailAccounts, newRequest(t, http.MethodGet, "/api/mail/accounts", nil))
	assertStatus(t, w, http.StatusOK)
	// The panel maps over the list directly; null would break it.
	assert.Contains(t, w.Body.String(), `"accounts":[]`)
}

func TestServeMailAccounts_MethodGuard(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailAccounts, newRequest(t, http.MethodDelete, "/api/mail/accounts", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailAccounts_RejectsMalformedBody(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/mail/accounts", nil)
	req.Body = http.NoBody
	w := callHandler(ServeMailAccounts, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeMailAccountSubRoutes_BadIDs(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	for _, path := range []string{
		"/api/mail/accounts/abc",
		"/api/mail/accounts/0",
		"/api/mail/accounts/-1",
		"/api/mail/accounts/1/nope",
	} {
		w := callHandler(ServeMailAccountSubRoutes, newRequest(t, http.MethodGet, path, nil))
		assertStatus(t, w, http.StatusNotFound)
	}
}

func TestServeMailAccountSubRoutes_UpdateAndDelete(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)

	w := callHandler(ServeMailAccountSubRoutes, newRequest(t, http.MethodPatch,
		"/api/mail/accounts/1", map[string]any{
			"email":    "new@example.com",
			"imapHost": "imap.example.com",
			"smtpHost": "smtp.example.com",
			"enabled":  false,
		}))
	assertStatus(t, w, http.StatusOK)
	var resp struct {
		Account service.MailAccount `json:"account"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "new@example.com", resp.Account.Email)
	assert.False(t, resp.Account.Enabled)

	w = callHandler(ServeMailAccountSubRoutes, newRequest(t, http.MethodDelete, "/api/mail/accounts/1", nil))
	assertStatus(t, w, http.StatusOK)
	if _, err := service.GetMailAccount(acc.ID); err == nil {
		t.Fatal("the account should be gone")
	}
}

func TestServeMailAccountSubRoutes_MethodGuard(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	// PUT is an accepted alias for PATCH, so the guard is checked with a
	// method the endpoint really does not serve.
	req := newRequest(t, "TRACE", "/api/mail/accounts/1", nil)
	w := callHandler(ServeMailAccountSubRoutes, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailAccountTest_FailsWithoutServer(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	// No IMAP server is listening: the endpoint must report a classified
	// failure rather than hanging or returning 200.
	w := callHandler(ServeMailAccountSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/accounts/1/test", nil))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestServeMailFolders_RequiresAccount(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailFolders, newRequest(t, http.MethodGet, "/api/mail/folders", nil))
	assertStatus(t, w, http.StatusBadRequest)

	acc := seedMailAccount(t)
	seedMailMessage(t, acc.ID)
	w = callHandler(ServeMailFolders, newRequest(t, http.MethodGet, "/api/mail/folders?accountId=1", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "INBOX")
}

func TestServeMailMessages_ReturnsCachedPage(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	folder, _ := seedMailMessage(t, acc.ID)

	w := callHandler(ServeMailMessages,
		newRequest(t, http.MethodGet, "/api/mail/messages?folderId=1&limit=10", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "周报")
	assert.Contains(t, w.Body.String(), `"hasMore":false`)

	// An unknown folder is a 200 with an empty page: the list simply has
	// nothing to show, and 404 would make the panel flash an error.
	w = callHandler(ServeMailMessages,
		newRequest(t, http.MethodGet, "/api/mail/messages?folderId=999", nil))
	assertStatus(t, w, http.StatusOK)
	_ = folder
}

func TestServeMailFolderSubRoutes_LoadOlderAndReadAll(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	folder, _ := seedMailMessage(t, acc.ID)

	// read-all goes to the server first; with nothing listening it must fail
	// rather than clearing the local unread counters optimistically.
	w := callHandler(ServeMailFolderSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/folders/1/read-all", nil))
	assert.NotEqual(t, http.StatusOK, w.Code)

	// load-older with an empty cache returns an empty page.
	w = callHandler(ServeMailFolderSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/folders/1/load-older", nil))
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
	_ = folder
}

func TestServeMailMessageSubRoutes_DetailFlagsAndReply(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	_, msg := seedMailMessage(t, acc.ID)
	require.NoError(t, service.SaveMailMessageBody(msg.ID, "正文", "<p>正文</p>", "正文"))

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/1", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "正文")

	w = callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/1/reply-context?mode=reply", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "Re: 周报")

	// Flags go to the server; with no IMAP server that is a failure, and the
	// local row must be left alone.
	w = callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/messages/1/flags", map[string]any{"seen": true}))
	assert.NotEqual(t, http.StatusOK, w.Code)
	after, err := service.GetMailMessage(msg.ID)
	require.NoError(t, err)
	assert.False(t, after.Seen)
}

func TestServeMailMessageSubRoutes_BadAttachmentIDs(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	for _, path := range []string{
		"/api/mail/messages/1/attachments/abc",
		"/api/mail/messages/1/attachments/0",
		"/api/mail/messages/1/unknown",
	} {
		w := callHandler(ServeMailMessageSubRoutes, newRequest(t, http.MethodGet, path, nil))
		assertStatus(t, w, http.StatusNotFound)
	}
}

func TestServeMailAttachment_ServesFileAndChecksOwnership(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	_, msg := seedMailMessage(t, acc.ID)

	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("attached bytes"), 0o644))
	require.NoError(t, service.SaveMailAttachments(msg.ID, []service.MailAttachment{{
		MessageID: msg.ID, Filename: "note.txt", ContentType: "text/plain",
		Size: int64(len("attached bytes")), LocalPath: path,
	}}))
	atts, err := service.ListMailAttachments(msg.ID)
	require.NoError(t, err)
	require.Len(t, atts, 1)

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/1/attachments/1", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "attached bytes")

	// The same attachment id under a different message is a 404, not a leak.
	w = callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/2/attachments/1", nil))
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeMailUnread_ReturnsTotals(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	folder, _ := seedMailMessage(t, acc.ID)
	// The folder counter is what the badge reads, and it is maintained by the
	// sync/flag paths rather than by the upsert.
	require.NoError(t, service.AdjustMailFolderUnread(folder.ID, 1))

	w := callHandler(ServeMailUnread, newRequest(t, http.MethodGet, "/api/mail/unread", nil))
	assertStatus(t, w, http.StatusOK)
	var resp struct {
		Total    int                         `json:"total"`
		Accounts []service.MailUnreadSummary `json:"accounts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	require.Len(t, resp.Accounts, 1)
	assert.Equal(t, acc.ID, resp.Accounts[0].AccountID)
}

func TestServeMailSend_RejectsEmptyAndMalformed(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	w := callHandler(ServeMailSend, newRequest(t, http.MethodPost, "/api/mail/send", nil))
	assertStatus(t, w, http.StatusBadRequest)

	w = callHandler(ServeMailSend, newRequest(t, http.MethodPost, "/api/mail/send", map[string]any{
		"accountId": 1,
		"subject":   "no recipients",
		"text":      "x",
	}))
	assertStatus(t, w, http.StatusBadRequest)

	w = callHandler(ServeMailSend, newRequest(t, http.MethodGet, "/api/mail/send", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailDrafts_CreateListPatchDelete(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	w := callHandler(ServeMailDrafts, newRequest(t, http.MethodPost, "/api/mail/drafts", map[string]any{
		"accountId": 1,
		"to":        "bob@example.com",
		"subject":   "草稿",
		"bodyText":  "内容",
	}))
	assertStatus(t, w, http.StatusOK)
	var created struct {
		Draft service.MailDraft `json:"draft"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "草稿", created.Draft.Subject)

	w = callHandler(ServeMailDrafts, newRequest(t, http.MethodGet, "/api/mail/drafts?accountId=1", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "草稿")

	w = callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodPatch,
		"/api/mail/drafts/1", map[string]any{"subject": "改过"}))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "改过")

	w = callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodDelete, "/api/mail/drafts/1", nil))
	assertStatus(t, w, http.StatusOK)
	drafts, err := service.ListMailDrafts(1)
	require.NoError(t, err)
	assert.Empty(t, drafts)
}

func TestServeMailDraftSubRoutes_Errors(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	for _, path := range []string{"/api/mail/drafts/abc", "/api/mail/drafts/0"} {
		w := callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodPatch, path, map[string]any{}))
		assertStatus(t, w, http.StatusNotFound)
	}
	w := callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodGet, "/api/mail/drafts/1", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailDrafts_RequiresAccount(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	w := callHandler(ServeMailDrafts, newRequest(t, http.MethodGet, "/api/mail/drafts", nil))
	assertStatus(t, w, http.StatusBadRequest)
	w = callHandler(ServeMailDrafts, newRequest(t, http.MethodPut, "/api/mail/drafts?accountId=1", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestRewriteCidURLs(t *testing.T) {
	atts := []service.MailAttachment{
		{ID: 5, ContentID: "<logo@example.com>"},
		{ID: 6, ContentID: ""},
	}
	html := `<img src="cid:logo@example.com"><img src="cid:missing">`
	got := rewriteCidURLs(html, 9, atts)
	assert.Contains(t, got, "/api/mail/messages/9/attachments/5")
	assert.Contains(t, got, "cid:missing", "an unknown cid must be left alone rather than 404'd into a broken image")

	// No cid at all: the string comes back untouched (and no allocation).
	assert.Equal(t, "<p>x</p>", rewriteCidURLs("<p>x</p>", 1, atts))
	assert.Equal(t, "", rewriteCidURLs("", 1, atts))
}

// writeMailError is the single place that turns a failure into a status code
// and a message key. Getting a kind wrong shows the user the wrong advice
// ("check your password" for a TLS problem), so each branch is pinned.
func TestWriteMailErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"account not found", service.ErrMailAccountNotFound, http.StatusNotFound},
		{"message not found", service.ErrMailMessageNotFound, http.StatusNotFound},
		{"folder not found", service.ErrMailFolderNotFound, http.StatusNotFound},
		{"draft not found", service.ErrMailDraftNotFound, http.StatusNotFound},
		{"attachment not found", service.ErrMailAttachmentNotFound, http.StatusNotFound},
		{"invalid account", service.ErrMailAccountInvalid, http.StatusBadRequest},
		{"sync busy", service.ErrMailSyncBusy, http.StatusConflict},
		{"no recipients", service.ErrMailNoRecipients, http.StatusBadRequest},
		{"auth", &mail.Error{Kind: mail.KindAuth, Err: errors.New("bad password")}, http.StatusBadRequest},
		{"connect", &mail.Error{Kind: mail.KindConnect, Err: errors.New("refused")}, http.StatusBadGateway},
		{"tls", &mail.Error{Kind: mail.KindTLS, Err: errors.New("cert")}, http.StatusBadGateway},
		{"timeout", &mail.Error{Kind: mail.KindTimeout, Err: errors.New("slow")}, http.StatusGatewayTimeout},
		{"server", &mail.Error{Kind: mail.KindServer, Err: errors.New("no")}, http.StatusBadGateway},
		{"unclassified", errors.New("whatever"), http.StatusBadGateway},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeMailError(rec, httptest.NewRequest(http.MethodGet, "/api/mail/x", nil), c.err)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestServeMailAccountSubRoutes_UnknownIDAndSyncFailure(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	w := callHandler(ServeMailAccountSubRoutes, newRequest(t, http.MethodGet, "/api/mail/accounts/42", nil))
	assertStatus(t, w, http.StatusNotFound)

	// Sync opens a real connection; with nothing listening it must fail with a
	// classified gateway error rather than 200 "synced".
	w = callHandler(ServeMailAccountSubRoutes, newRequest(t, http.MethodPost, "/api/mail/accounts/1/sync", nil))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestServeMailMessages_SearchAndPaging(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	seedMailMessage(t, acc.ID)

	w := callHandler(ServeMailMessages,
		newRequest(t, http.MethodGet, "/api/mail/messages?folderId=1&q=%E5%91%A8%E6%8A%A5", nil))
	assertStatus(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "周报")

	// Keyset paging: a cursor that matches nothing still returns an empty page.
	w = callHandler(ServeMailMessages,
		newRequest(t, http.MethodGet, "/api/mail/messages?folderId=1&beforeAt=1&beforeId=1", nil))
	assertStatus(t, w, http.StatusOK)

	// A non-numeric folder id is a 400, not a 500.
	w = callHandler(ServeMailMessages, newRequest(t, http.MethodGet, "/api/mail/messages?folderId=abc", nil))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeMailMessageMoveToSameFolderIsOk(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	folder, _ := seedMailMessage(t, acc.ID)

	// Same-folder moves short-circuit before any network call, which is also
	// what makes this reachable without an IMAP server.
	w := callHandler(ServeMailMessageSubRoutes, newRequest(t, http.MethodPost,
		"/api/mail/messages/1/move", map[string]any{"folderId": folder.ID}))
	assertStatus(t, w, http.StatusOK)

	w = callHandler(ServeMailMessageSubRoutes, newRequest(t, http.MethodGet, "/api/mail/messages/1/move", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)

	w = callHandler(ServeMailMessageSubRoutes, newRequest(t, http.MethodPost, "/api/mail/messages/1/move", nil))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeMailMessageDeleteNeedsServer(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	_, msg := seedMailMessage(t, acc.ID)

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/messages/1/delete", map[string]any{"permanent": true}))
	assert.NotEqual(t, http.StatusOK, w.Code)
	// The local row survives a failed delete.
	if _, err := service.GetMailMessage(msg.ID); err != nil {
		t.Fatal("a failed delete must not drop the cached row")
	}

	w = callHandler(ServeMailMessageSubRoutes, newRequest(t, http.MethodGet, "/api/mail/messages/1/delete", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeMailDraftSubRoutes_UnknownDraft(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	// PATCH on a missing draft reports not-found instead of creating one with
	// that id.
	w := callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodPatch,
		"/api/mail/drafts/42", map[string]any{"subject": "x"}))
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeMailAttachment_FileGoneIsReported(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	_, msg := seedMailMessage(t, acc.ID)

	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	require.NoError(t, service.SaveMailAttachments(msg.ID, []service.MailAttachment{{
		MessageID: msg.ID, Filename: "gone.txt", ContentType: "", Size: 0, LocalPath: path,
	}}))
	// The cached file disappears between listing and download.
	require.NoError(t, os.Remove(path))

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/1/attachments/1", nil))
	// OpenMailAttachment re-fetches; with no IMAP server that is a gateway
	// error rather than a 200 with an empty body.
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestServeMailAttachment_SetsHeaders(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	acc := seedMailAccount(t)
	_, msg := seedMailMessage(t, acc.ID)

	dir := t.TempDir()
	path := filepath.Join(dir, "报告.pdf")
	require.NoError(t, os.WriteFile(path, []byte("pdf"), 0o644))
	require.NoError(t, service.SaveMailAttachments(msg.ID, []service.MailAttachment{{
		MessageID: msg.ID, Filename: "报告.pdf", ContentType: "", Size: 3, LocalPath: path,
	}}))

	w := callHandler(ServeMailMessageSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/messages/1/attachments/1", nil))
	assertStatus(t, w, http.StatusOK)
	// An unknown content type must not leave the browser guessing.
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, "3", w.Header().Get("Content-Length"))
}

func TestServeMailSend_MalformedBody(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	w := callHandler(ServeMailSend, newRequest(t, http.MethodPost, "/api/mail/send", nil))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeMailDrafts_MalformedBodies(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)

	w := callHandler(ServeMailDrafts, newRequest(t, http.MethodPost, "/api/mail/drafts", nil))
	assertStatus(t, w, http.StatusBadRequest)

	w = callHandler(ServeMailDrafts, newRequest(t, http.MethodGet, "/api/mail/drafts?accountId=abc", nil))
	assertStatus(t, w, http.StatusBadRequest)

	w = callHandler(ServeMailDraftSubRoutes, newRequest(t, http.MethodPatch, "/api/mail/drafts/1", nil))
	assertStatus(t, w, http.StatusBadRequest)
}

// Every endpoint must turn a database failure into a 5xx instead of a panic or
// a silent 200. One closed handle exercises the whole error surface cheaply.
func TestServeMail_DatabaseFailureIsReported(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()
	seedMailAccount(t)
	seedMailMessage(t, 1)

	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	require.NoError(t, db.Close())
	restore := store.SetDBForTest(db, db)
	defer restore()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		req     *http.Request
	}{
		{"list accounts", ServeMailAccounts, newRequest(t, http.MethodGet, "/api/mail/accounts", nil)},
		{"create account", ServeMailAccounts, newRequest(t, http.MethodPost, "/api/mail/accounts", map[string]any{
			"email": "a@example.com", "imapHost": "i", "smtpHost": "s",
		})},
		{"get account", ServeMailAccountSubRoutes, newRequest(t, http.MethodGet, "/api/mail/accounts/1", nil)},
		{"delete account", ServeMailAccountSubRoutes, newRequest(t, http.MethodDelete, "/api/mail/accounts/1", nil)},
		{"list folders", ServeMailFolders, newRequest(t, http.MethodGet, "/api/mail/folders?accountId=1", nil)},
		{"list messages", ServeMailMessages, newRequest(t, http.MethodGet, "/api/mail/messages?folderId=1", nil)},
		{"unread", ServeMailUnread, newRequest(t, http.MethodGet, "/api/mail/unread", nil)},
		{"list drafts", ServeMailDrafts, newRequest(t, http.MethodGet, "/api/mail/drafts?accountId=1", nil)},
	}
	for _, c := range cases {
		w := callHandler(c.handler, c.req)
		if w.Code < 500 {
			t.Errorf("%s: status = %d, want 5xx", c.name, w.Code)
		}
	}
}

func TestServeMailAccountTestAndSyncUnknownAccount(t *testing.T) {
	teardown := setupMailHandlerEnv(t)
	defer teardown()

	// Both sub-actions report not-found for an account that does not exist,
	// rather than attempting a connection to a zero-valued account.
	w := callHandler(ServeMailAccountSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/accounts/42/test", nil))
	assertStatus(t, w, http.StatusNotFound)

	w = callHandler(ServeMailAccountSubRoutes,
		newRequest(t, http.MethodPost, "/api/mail/accounts/42/sync", nil))
	assertStatus(t, w, http.StatusNotFound)

	// The actions are POST-only.
	w = callHandler(ServeMailAccountSubRoutes,
		newRequest(t, http.MethodGet, "/api/mail/accounts/1/test", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}
