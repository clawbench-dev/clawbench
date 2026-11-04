package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"clawbench/internal/mail"
	"clawbench/internal/store"
)

const (
	// mailInitialSyncLimit is how many of the newest messages a folder caches.
	// Pulling an entire 50k-message archive on first connect would take minutes
	// and fill the database with mail nobody is going to read; older pages are
	// fetched on demand when the user scrolls back.
	mailInitialSyncLimit = 100
	// mailFlagRefreshWindow is how many cached messages get their flags
	// re-read on every sync, so read/star changes made on the phone show up.
	mailFlagRefreshWindow = 200
	// mailBodyPrefetch is how many of the newest messages have their body
	// decoded during sync. This is what gives the list its preview lines and
	// makes the first click instant.
	mailBodyPrefetch = 10
	// mailPrefetchMaxSize skips prefetching huge messages (usually a big
	// attachment); they are parsed when opened instead.
	mailPrefetchMaxSize = 300 * 1024
)

// mailDialer / mailSender are variables so tests can inject fakes.
var (
	mailDialer mail.Dialer = mail.DialIMAP
	mailSender mail.Sender = mail.SMTPSender{}
)

// mailSyncMu serializes syncs per account. The background worker and a manual
// "refresh" click must not open two connections to the same mailbox.
var mailSyncMu sync.Map // map[int64]*sync.Mutex

func accountSyncLock(accountID int64) *sync.Mutex {
	if m, ok := mailSyncMu.Load(accountID); ok {
		if lock, ok := m.(*sync.Mutex); ok {
			return lock
		}
	}
	mu := &sync.Mutex{}
	actual, _ := mailSyncMu.LoadOrStore(accountID, mu)
	if lock, ok := actual.(*sync.Mutex); ok {
		return lock
	}
	return mu
}

// MailSyncResult summarizes one sync pass.
type MailSyncResult struct {
	Folders     int      `json:"folders"`
	NewMessages int      `json:"newMessages"`
	Errors      []string `json:"errors,omitempty"`
}

// SyncMailAccount pulls the newest messages of every folder into the local
// cache. A failure in one folder does not abort the others; the per-folder
// errors are reported in the result.
func SyncMailAccount(ctx context.Context, accountID int64) (MailSyncResult, error) {
	var result MailSyncResult

	mu := accountSyncLock(accountID)
	if !mu.TryLock() {
		return result, ErrMailSyncBusy
	}
	defer mu.Unlock()

	acc, err := GetMailAccount(accountID)
	if err != nil {
		return result, err
	}

	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		_ = SetMailAccountSyncResult(accountID, err.Error())
		return result, err
	}
	defer func() { _ = client.Close() }()

	folders, err := client.ListFolders(ctx)
	if err != nil {
		_ = SetMailAccountSyncResult(accountID, err.Error())
		return result, err
	}

	for i, f := range folders {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		folderID, err := UpsertMailFolder(MailFolder{
			AccountID:  accountID,
			Path:       f.Path,
			Name:       f.Name,
			Delimiter:  f.Delimiter,
			SpecialUse: f.SpecialUse,
			SortOrder:  i,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", f.Name, err))
			continue
		}
		result.Folders++

		newCount, err := syncMailFolder(ctx, client, accountID, folderID, f)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", f.Name, err))
			continue
		}
		result.NewMessages += newCount
	}

	lastErr := ""
	if len(result.Errors) > 0 {
		lastErr = strings.Join(result.Errors, "; ")
	}
	_ = SetMailAccountSyncResult(accountID, lastErr)
	return result, nil
}

// ErrMailSyncBusy is returned when a sync for the same account is already running.
var ErrMailSyncBusy = errors.New("mail sync already running")

func syncMailFolder(ctx context.Context, client mail.Client, accountID, folderID int64, f mail.Folder) (int, error) {
	st, err := client.Status(ctx, f.Path)
	if err != nil {
		return 0, err
	}

	cached, err := GetMailFolder(folderID)
	if err != nil {
		return 0, err
	}
	// A changed UIDVALIDITY means every cached UID now points at a different
	// message. There is no way to reconcile that incrementally.
	if cached.UIDValidity != 0 && st.UIDValidity != 0 && cached.UIDValidity != st.UIDValidity {
		if cerr := ClearMailFolderMessages(folderID); cerr != nil {
			return 0, cerr
		}
	}

	headers, err := client.FetchNewest(ctx, f.Path, mailInitialSyncLimit)
	if err != nil {
		return 0, err
	}
	msgs := make([]MailMessage, 0, len(headers))
	for _, h := range headers {
		msgs = append(msgs, headerToRow(accountID, folderID, h))
	}
	firstSync := cached.LastSeenUID == 0
	inserted, err := UpsertMailMessages(msgs)
	if err != nil {
		return 0, err
	}

	if err := refreshFolderFlags(ctx, client, folderID, f.Path); err != nil {
		slog.Debug("mail: refresh flags failed", "folder", f.Name, "error", err)
	}

	var lastSeen uint32
	for _, h := range headers {
		if h.UID > lastSeen {
			lastSeen = h.UID
		}
	}
	if err := UpdateMailFolderState(folderID, st.UIDValidity, st.UIDNext, lastSeen,
		int(st.Unseen), int(st.Messages)); err != nil {
		return len(inserted), err
	}

	prefetchMailBodies(ctx, client, folderID, f.Path, mailBodyPrefetch)
	dispatchMailTriggers(accountID, f.Path, inserted, firstSync)
	return len(inserted), nil
}

// dispatchMailTriggers hands freshly cached messages to the email-trigger
// matcher.
//
// firstSync is the guard that makes a new mailbox safe to connect: the initial
// sync caches the newest 100 messages, and without it every email task would
// fire once per pre-existing message the moment an account is added.
func dispatchMailTriggers(accountID int64, folderPath string, inserted []MailMessage, firstSync bool) {
	if firstSync || len(inserted) == 0 {
		return
	}
	FireMailTaskTriggers(accountID, folderPath, inserted)
}

func refreshFolderFlags(ctx context.Context, client mail.Client, folderID int64, path string) error {
	cached, err := ListMailMessages(MailMessageFilter{FolderID: folderID, Limit: mailFlagRefreshWindow})
	if err != nil || len(cached) == 0 {
		return err
	}
	uids := make([]uint32, 0, len(cached))
	byUID := make(map[uint32]MailMessage, len(cached))
	for _, m := range cached {
		uids = append(uids, m.UID)
		byUID[m.UID] = m
	}
	fresh, err := client.FetchHeaders(ctx, path, uids)
	if err != nil {
		return err
	}
	for _, h := range fresh {
		old, ok := byUID[h.UID]
		if !ok {
			continue
		}
		if old.Seen == h.Seen && old.Flagged == h.Flagged && old.Answered == h.Answered {
			continue
		}
		if err := UpdateMailMessageFlags(old.ID, h.Seen, h.Flagged, h.Answered); err != nil {
			return err
		}
	}
	return nil
}

func headerToRow(accountID, folderID int64, h mail.MessageHeader) MailMessage {
	to := make([]MailAddr, 0, len(h.To))
	for _, a := range h.To {
		to = append(to, MailAddr{Name: a.Name, Addr: a.Addr})
	}
	cc := make([]MailAddr, 0, len(h.Cc))
	for _, a := range h.Cc {
		cc = append(cc, MailAddr{Name: a.Name, Addr: a.Addr})
	}
	sentAt := h.Date.Unix()
	if h.Date.IsZero() {
		sentAt = 0
	}
	return MailMessage{
		AccountID:      accountID,
		FolderID:       folderID,
		UID:            h.UID,
		MessageID:      h.MessageID,
		Subject:        h.Subject,
		FromName:       h.FromName,
		FromAddr:       h.FromAddr,
		To:             to,
		Cc:             cc,
		ReplyTo:        firstMailAddr(h.ReplyTo),
		InReplyTo:      h.InReplyTo,
		References:     strings.Join(h.References, " "),
		SentAt:         sentAt,
		Size:           h.Size,
		Seen:           h.Seen,
		Flagged:        h.Flagged,
		Answered:       h.Answered,
		HasAttachments: h.HasAttachments,
		Snippet:        h.Preview,
	}
}

func firstMailAddr(addrs []mail.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0].Addr
}

// FetchOlderMailMessages loads one page of messages older than the oldest
// cached row. The list endpoint calls this when the user scrolls past what is
// already cached.
func FetchOlderMailMessages(ctx context.Context, folderID int64) ([]MailMessage, error) {
	folder, err := GetMailFolder(folderID)
	if err != nil {
		return nil, err
	}
	acc, err := GetMailAccount(folder.AccountID)
	if err != nil {
		return nil, err
	}

	oldest, err := oldestCachedMessage(folderID)
	if err != nil {
		return nil, err
	}
	if oldest == nil {
		return nil, nil
	}

	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	headers, err := client.FetchBefore(ctx, folder.Path, oldest.UID, 50)
	if err != nil {
		return nil, err
	}
	msgs := make([]MailMessage, 0, len(headers))
	for _, h := range headers {
		msgs = append(msgs, headerToRow(folder.AccountID, folderID, h))
	}
	if _, err := UpsertMailMessages(msgs); err != nil {
		return nil, err
	}
	return ListMailMessages(MailMessageFilter{
		FolderID: folderID, Limit: 50,
		BeforeAt: oldest.SentAt, BeforeID: oldest.ID,
	})
}

func oldestCachedMessage(folderID int64) (*MailMessage, error) {
	row := store.ReadDB().QueryRow(`SELECT `+mailMessageColumns+` FROM mail_messages
		WHERE folder_id = ? ORDER BY sent_at ASC, id ASC LIMIT 1`, folderID)
	m, err := scanMailMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
