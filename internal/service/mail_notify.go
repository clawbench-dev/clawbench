package service

import (
	"sort"
	"strings"
	"unicode/utf8"

	"clawbench/internal/i18n"
	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/push/feishu"
	"clawbench/internal/push/wechat"
	"clawbench/internal/ws"
)

const (
	// MailEventType is the WS event name for "new inbox mail arrived". Like the
	// usage-meter event it carries pre-rendered title/body instead of an id +
	// status the client would have to translate: the producer is a background
	// sync pass with no request in scope, so it localizes once.
	MailEventType = "mail_event"
	// MailEventReceived is the notification row's event_type for this kind.
	MailEventReceived = "received"

	// mailNotifyTitleRunes / mailNotifyBodyRunes bound what is stored and
	// pushed. A subject is user-controlled and an email can be arbitrarily
	// long; the notification card clamps to two lines anyway, and the IM
	// backends have their own payload limits.
	mailNotifyTitleRunes = 200
	mailNotifyBodyRunes  = 400
)

// notifyNewInboxMail records and broadcasts a new-mail notification for the
// messages a sync pass just cached.
//
// Only the INBOX is announced. A message that appears in an archive/label
// folder is not "new mail" in the sense a notification means, and the initial
// sync of a freshly configured account is excluded by the caller (firstSync):
// announcing a hundred historical messages on connect would train the user to
// ignore the channel.//
// The switch is `mail.notify`, deliberately independent of `mail.enabled`:
// turning mail off stops syncing entirely, while this only silences the alert.
// With it off the sync, the unread badge and the mail panel all keep working.
func notifyNewInboxMail(accountID int64, folder mail.Folder, inserted []MailMessage, firstSync bool) {
	if firstSync || len(inserted) == 0 || !model.ConfigInstance.Mail.Enabled || !model.ConfigInstance.Mail.Notify {
		return
	}
	if !isInboxFolder(folder) {
		return
	}

	fresh := make([]MailMessage, 0, len(inserted))
	for _, m := range inserted {
		// A message already flagged \Seen was read elsewhere (phone, webmail,
		// another device) between arrival and this sync. It is not news.
		if m.ID > 0 && !m.Seen {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 0 {
		return
	}
	// Newest first: the click opens that message, and its subject is the title.
	sort.SliceStable(fresh, func(i, j int) bool {
		if fresh[i].SentAt != fresh[j].SentAt {
			return fresh[i].SentAt > fresh[j].SentAt
		}
		return fresh[i].ID > fresh[j].ID
	})

	newest := fresh[0]
	title, body := renderMailNotification(fresh)
	// The event id follows the standard evt_<instance>_<seq> shape, NOT a
	// domain key: native shells use the id as a pending-events CURSOR, and they
	// only accept that shape (see validEventId in NativeEvents.ts). A custom
	// "mail:..." id would make every recovery poll re-fetch the same row.
	eventID := ws.GenerateEventID()

	StoreNotification(NotificationRecord{
		EventID:       eventID,
		Kind:          NotificationKindMail,
		EventType:     MailEventReceived,
		Title:         title,
		Body:          body,
		MailMessageID: newest.ID,
	})

	// The WS frame is live delivery; the notification-center row above is the
	// durable copy. Gated on push_mode like the other notification paths:
	// "disabled" means the user turned live notifications off entirely.
	if model.ConfigInstance.PushMode != model.PushModeDisabled {
		msg := mailEventMessage(eventID, accountID, newest.ID, title, body, len(fresh))
		// Write-ahead first, so an offline client can recover the event even
		// when nobody is connected to receive the broadcast.
		StoreNotifiableEvent(msg)
		if mgr := ws.GetManager(); mgr != nil {
			mgr.BroadcastEvent(msg)
		}
	}

	pushMailToIM(eventID, title, body)
}

// mailEventMessage builds the mail_event frame. Split out so tests can assert
// the payload without a WS manager (BroadcastEvent with no subscribers is a
// no-op).
func mailEventMessage(eventID string, accountID, messageID int64, title, body string, count int) ws.ServerMessage {
	return ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    eventID,
		Event: MailEventType,
		Data: &ws.MailEventData{
			Title:         title,
			Body:          body,
			MailMessageID: messageID,
			AccountID:     accountID,
			Count:         count,
		},
	}
}

// pushMailToIM forwards the notification to the configured IM channel.
//
// PushForgeEvent-style: the backends own only the transport; the wording is
// rendered here from the user's configured language. A successful push deletes
// the pending event so an Android reconnect does not raise a second
// notification for the same batch.
func pushMailToIM(eventID, title, body string) {
	if dingtalk.IsStarted() && dingtalk.PushMailEvent(title, body) {
		_ = DeletePendingEvent(eventID)
		return
	}
	if feishu.IsStarted() && feishu.PushMailEvent(title, body) {
		_ = DeletePendingEvent(eventID)
		return
	}
	if wechat.IsStarted() && wechat.PushMailEvent(title, body) {
		_ = DeletePendingEvent(eventID)
	}
}

// isInboxFolder recognizes the INBOX across servers. LIST may report the
// \Inbox special-use attribute, but plenty of servers (and every mock) only
// expose the canonical path "INBOX", so both signals count.
func isInboxFolder(f mail.Folder) bool {
	return f.SpecialUse == mail.SpecialUseInbox || strings.EqualFold(f.Path, "INBOX")
}

// renderMailNotification localizes the notification text at write time.
//
// A stored notification records what the user was told at that moment:
// switching the UI language later must not rewrite history. The title is data
// (the subject) when a single message arrived, and a pre-rendered count when a
// sync pass batched several.
func renderMailNotification(msgs []MailMessage) (title, body string) {
	newest := msgs[0]
	sender := strings.TrimSpace(newest.FromName)
	if sender == "" {
		sender = strings.TrimSpace(newest.FromAddr)
	}
	if sender == "" {
		sender = mailNotifyT("MailNotifyUnknownSender")
	}
	subject := strings.TrimSpace(newest.Subject)
	if subject == "" {
		subject = mailNotifyT("MailNotifyNoSubject")
	}

	if len(msgs) > 1 {
		title = mailNotifyT("MailNotifyManyTitle", map[string]any{"Count": len(msgs)})
		body = mailNotifyT("MailNotifyLatest", map[string]any{
			"Subject": subject,
			"Sender":  sender,
		})
		return mailNotifyClip(title, mailNotifyTitleRunes), mailNotifyClip(body, mailNotifyBodyRunes)
	}

	title = subject
	body = sender
	if preview := mailNotifyClip(collapseWhitespace(newest.Snippet), mailNotifyBodyRunes); preview != "" {
		body = sender + " · " + preview
	}
	return mailNotifyClip(title, mailNotifyTitleRunes), mailNotifyClip(body, mailNotifyBodyRunes)
}

// mailNotifyT localizes one string for the notification. Same reasoning as
// liveNoteT: the producer runs on a background goroutine with no HTTP request.
func mailNotifyT(key string, data ...map[string]any) string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), key, data...)
}

// collapseWhitespace folds every whitespace run (including the newlines a
// snippet may carry) into one space, so the card's two-line clamp shows text
// rather than blank lines.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// mailNotifyClip truncates to limit runes with an ellipsis, on a rune boundary.
func mailNotifyClip(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
