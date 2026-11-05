package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"clawbench/internal/mail"
	"clawbench/internal/service"
)

// JSON keys reused across the mail endpoints. The generic ones already exist in
// this package (jsonTotal / jsonHasMore / messageKey); these are the mail-only
// ones.
const (
	jsonMailAccount  = "account"
	jsonMailAccounts = "accounts"
	jsonMailResult   = "result"
)

// ServeMail dispatches /api/mail/* .
//
// The fixed sub-resources (presets, accounts, folders, messages, unread, send,
// drafts) are registered as their own exact patterns; this handler covers the
// id-bearing tails.
func ServeMailPresets(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": mail.Presets})
}

// --- accounts ---

// ServeMailAccounts handles /api/mail/accounts.
func ServeMailAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		accounts, err := service.ListMailAccounts()
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		if accounts == nil {
			accounts = []service.MailAccount{}
		}
		writeJSON(w, http.StatusOK, map[string]any{jsonMailAccounts: accounts})
	case http.MethodPost:
		var req mailAccountRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		acc := req.toAccount()
		if err := validateMailAccount(acc); err != nil {
			writeMailError(w, r, err)
			return
		}
		created, err := service.CreateMailAccount(acc)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{jsonMailAccount: created})
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// ServeMailAccountSubRoutes handles /api/mail/accounts/{id}[/test|/sync].
func ServeMailAccountSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mail/accounts/")
	idPart, sub, hasSub := strings.Cut(path, "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	if hasSub {
		switch sub {
		case "test":
			serveMailAccountTest(w, r, id)
		case "sync":
			serveMailAccountSync(w, r, id)
		default:
			writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		acc, err := service.GetMailAccount(id)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		if r.URL.Query().Get("reveal") == "1" {
			writeJSON(w, http.StatusOK, map[string]any{jsonMailAccount: acc, "password": acc.Password})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{jsonMailAccount: acc})
	case http.MethodPatch, http.MethodPut:
		var req mailAccountRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		acc := req.toAccount()
		acc.ID = id
		if err := validateMailAccount(acc); err != nil {
			writeMailError(w, r, err)
			return
		}
		updated, err := service.UpdateMailAccount(acc)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{jsonMailAccount: updated})
	case http.MethodDelete:
		if err := service.DeleteMailAccount(id); err != nil {
			writeMailError(w, r, err)
			return
		}
		service.DeleteMailAccountAttachmentFiles(id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

type mailAccountRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	IMAPHost     string `json:"imapHost"`
	IMAPPort     int    `json:"imapPort"`
	IMAPSecurity string `json:"imapSecurity"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"`
	InsecureTLS  bool   `json:"insecureTls"`
	Enabled      *bool  `json:"enabled"`
	SortOrder    int    `json:"sortOrder"`
}

func (r mailAccountRequest) toAccount() service.MailAccount {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	acc := service.MailAccount{
		Name:         strings.TrimSpace(r.Name),
		Email:        strings.TrimSpace(r.Email),
		Username:     strings.TrimSpace(r.Username),
		Password:     r.Password,
		IMAPHost:     strings.TrimSpace(r.IMAPHost),
		IMAPPort:     r.IMAPPort,
		IMAPSecurity: r.IMAPSecurity,
		SMTPHost:     strings.TrimSpace(r.SMTPHost),
		SMTPPort:     r.SMTPPort,
		SMTPSecurity: r.SMTPSecurity,
		InsecureTLS:  r.InsecureTLS,
		Enabled:      enabled,
		SortOrder:    r.SortOrder,
	}
	if acc.Username == "" {
		acc.Username = acc.Email
	}
	if acc.Name == "" {
		acc.Name = acc.Email
	}
	if acc.IMAPSecurity == "" {
		acc.IMAPSecurity = string(mail.SecurityTLS)
	}
	if acc.SMTPSecurity == "" {
		acc.SMTPSecurity = string(mail.SecurityTLS)
	}
	if acc.IMAPPort == 0 {
		acc.IMAPPort = 993
	}
	if acc.SMTPPort == 0 {
		acc.SMTPPort = 465
	}
	return acc
}

func validateMailAccount(a service.MailAccount) error {
	if a.Email == "" || !strings.Contains(a.Email, "@") {
		return service.ErrMailAccountInvalid
	}
	if a.IMAPHost == "" || a.SMTPHost == "" {
		return service.ErrMailAccountInvalid
	}
	return nil
}

func serveMailAccountTest(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := service.TestMailAccount(r.Context(), id); err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func serveMailAccountSync(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	result, err := service.SyncMailAccount(r.Context(), id)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonMailResult: result})
}

// --- folders / messages ---

// ServeMailFolders handles GET /api/mail/folders?accountId=.
func ServeMailFolders(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	accountID, err := strconv.ParseInt(r.URL.Query().Get("accountId"), 10, 64)
	if err != nil || accountID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	folders, err := service.ListMailFolders(accountID)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if folders == nil {
		folders = []service.MailFolder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

// ServeMailMessages handles GET /api/mail/messages.
func ServeMailMessages(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query()
	folderID, err := strconv.ParseInt(q.Get("folderId"), 10, 64)
	if err != nil || folderID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	beforeAt, _ := strconv.ParseInt(q.Get("beforeAt"), 10, 64)
	beforeID, _ := strconv.ParseInt(q.Get("beforeId"), 10, 64)

	msgs, err := service.ListMailMessages(service.MailMessageFilter{
		FolderID: folderID,
		Limit:    limit,
		BeforeAt: beforeAt,
		BeforeID: beforeID,
		Query:    q.Get("q"),
	})
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if msgs == nil {
		msgs = []service.MailMessage{}
	}
	cached, _ := service.MailFolderCachedCount(folderID)
	hasMore := false
	if folder, ferr := service.GetMailFolder(folderID); ferr == nil {
		hasMore = cached < folder.Total
	}
	writeJSON(w, http.StatusOK, map[string]any{strMessages: msgs, jsonHasMore: hasMore})
}

// ServeMailFolderSubRoutes handles POST /api/mail/folders/{id}/load-older and
// POST /api/mail/folders/{id}/read-all.
func ServeMailFolderSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mail/folders/")
	idPart, sub, hasSub := strings.Cut(path, "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 || !hasSub || (sub != "load-older" && sub != "read-all") {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if sub == "read-all" {
		if rerr := service.MarkMailFolderRead(r.Context(), id); rerr != nil {
			writeMailError(w, r, rerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	msgs, err := service.FetchOlderMailMessages(r.Context(), id)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if msgs == nil {
		msgs = []service.MailMessage{}
	}
	cached, _ := service.MailFolderCachedCount(id)
	hasMore := false
	if folder, ferr := service.GetMailFolder(id); ferr == nil {
		hasMore = cached < folder.Total
	}
	writeJSON(w, http.StatusOK, map[string]any{strMessages: msgs, jsonHasMore: hasMore})
}

// ServeMailMessageSubRoutes handles /api/mail/messages/{id}/...
func ServeMailMessageSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mail/messages/")
	idPart, sub, hasSub := strings.Cut(path, "/")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	if !hasSub {
		serveMailMessageDetail(w, r, id)
		return
	}
	switch {
	case sub == "flags":
		serveMailMessageFlags(w, r, id)
	case sub == "move":
		serveMailMessageMove(w, r, id)
	case sub == "delete":
		serveMailMessageDelete(w, r, id)
	case sub == "reply-context":
		serveMailReplyContext(w, r, id)
	case strings.HasPrefix(sub, "attachments/"):
		attID, err := strconv.ParseInt(strings.TrimPrefix(sub, "attachments/"), 10, 64)
		if err != nil || attID <= 0 {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
			return
		}
		serveMailAttachment(w, r, id, attID)
	default:
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
	}
}

func serveMailMessageDetail(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	detail, err := service.GetMailMessageDetail(r.Context(), id, true)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	detail.BodyHTML = rewriteCidURLs(detail.BodyHTML, detail.ID, detail.Attachments)
	writeJSON(w, http.StatusOK, map[string]any{messageKey: detail})
}

var cidRe = regexp.MustCompile(`(?i)cid:([^"'\s)<>]+)`)

// rewriteCidURLs points inline images at the attachment endpoint. Doing it on
// the server keeps the browser from ever seeing a cid: URL it cannot resolve,
// and the mapping stays next to the attachment ids.
func rewriteCidURLs(htmlBody string, messageID int64, atts []service.MailAttachment) string {
	if htmlBody == "" || !strings.Contains(strings.ToLower(htmlBody), "cid:") {
		return htmlBody
	}
	byCID := make(map[string]int64, len(atts))
	for _, a := range atts {
		if a.ContentID != "" {
			byCID[strings.ToLower(strings.Trim(a.ContentID, "<>"))] = a.ID
		}
	}
	return cidRe.ReplaceAllStringFunc(htmlBody, func(m string) string {
		key := strings.ToLower(strings.Trim(m[4:], "<>"))
		if id, ok := byCID[key]; ok {
			return fmt.Sprintf("/api/mail/messages/%d/attachments/%d", messageID, id)
		}
		return m
	})
}

func serveMailMessageFlags(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var patch service.MailFlagPatch
	if !decodeJSON(w, r, &patch) {
		return
	}
	msg, err := service.SetMailMessageFlags(r.Context(), id, patch)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{messageKey: msg})
}

func serveMailMessageMove(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		FolderID int64 `json:"folderId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := service.MoveMailMessage(r.Context(), id, req.FolderID); err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func serveMailMessageDelete(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Permanent bool `json:"permanent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := service.DeleteMailMessage(r.Context(), id, req.Permanent); err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func serveMailReplyContext(w http.ResponseWriter, r *http.Request, id int64) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	ctx, err := service.BuildMailReplyContext(id, r.URL.Query().Get("mode"))
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"context": ctx})
}

func serveMailAttachment(w http.ResponseWriter, r *http.Request, messageID, attID int64) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	att, path, err := service.OpenMailAttachment(r.Context(), attID)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if att.MessageID != messageID {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()

	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": att.Filename}))
	if att.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(att.Size, 10))
	}
	_, _ = io.Copy(w, f)
}

// --- unread / send / drafts ---

// ServeMailUnread handles GET /api/mail/unread (the dock badge).
func ServeMailUnread(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	accounts, total, err := service.MailUnreadTotals()
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if accounts == nil {
		accounts = []service.MailUnreadSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonMailAccounts: accounts, jsonTotal: total})
}

// ServeMailSend handles POST /api/mail/send.
func ServeMailSend(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req service.MailSendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := service.SendMail(r.Context(), req); err != nil {
		writeMailError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ServeMailDrafts handles GET/POST /api/mail/drafts.
func ServeMailDrafts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		accountID, err := strconv.ParseInt(r.URL.Query().Get("accountId"), 10, 64)
		if err != nil || accountID <= 0 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		drafts, err := service.ListMailDrafts(accountID)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		if drafts == nil {
			drafts = []service.MailDraft{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"drafts": drafts})
	case http.MethodPost:
		var d service.MailDraft
		if !decodeJSON(w, r, &d) {
			return
		}
		d.ID = 0
		saved, err := service.SaveMailDraft(d)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"draft": saved})
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// ServeMailDraftSubRoutes handles PATCH/DELETE /api/mail/drafts/{id}.
func ServeMailDraftSubRoutes(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/mail/drafts/"), 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	switch r.Method {
	case http.MethodPatch, http.MethodPut:
		var d service.MailDraft
		if !decodeJSON(w, r, &d) {
			return
		}
		d.ID = id
		saved, err := service.SaveMailDraft(d)
		if err != nil {
			writeMailError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"draft": saved})
	case http.MethodDelete:
		if err := service.DeleteMailDraft(id); err != nil {
			writeMailError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// --- errors ---

// writeMailError maps a mail failure onto a localized message key so the UI
// can say "wrong password" instead of dumping an IMAP protocol line.
func writeMailError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrMailAccountNotFound),
		errors.Is(err, service.ErrMailMessageNotFound),
		errors.Is(err, service.ErrMailFolderNotFound),
		errors.Is(err, service.ErrMailDraftNotFound),
		errors.Is(err, service.ErrMailAttachmentNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
	case errors.Is(err, service.ErrMailAccountInvalid):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MailAccountInvalid")
	case errors.Is(err, service.ErrMailSyncBusy):
		writeLocalizedErrorf(w, r, http.StatusConflict, "MailSyncBusy")
	case errors.Is(err, service.ErrMailNoRecipients):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MailNoRecipients")
	default:
		switch mail.KindOf(err) {
		case mail.KindAuth:
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "MailAuthFailed")
		case mail.KindConnect:
			writeLocalizedErrorf(w, r, http.StatusBadGateway, "MailConnectFailed")
		case mail.KindTLS:
			writeLocalizedErrorf(w, r, http.StatusBadGateway, "MailTLSFailed")
		case mail.KindTimeout:
			writeLocalizedErrorf(w, r, http.StatusGatewayTimeout, "MailTimeout")
		case mail.KindUnsupported:
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "MailUnsupportedAddress")
		default:
			writeLocalizedErrorf(w, r, http.StatusBadGateway, "MailServerError")
		}
	}
}
