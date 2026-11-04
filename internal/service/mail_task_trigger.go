package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// MailTaskTriggerDDL is the dedupe ledger for email-triggered tasks.
//
// A message that matches a task must fire it exactly once, no matter how many
// times its folder is re-synced (every sync re-reads the newest 100 headers,
// and a restart re-reads them again). The primary key is the whole guarantee:
// the insert either claims the pair or reports that someone else did.
const MailTaskTriggerDDL = `
CREATE TABLE IF NOT EXISTS mail_task_triggers (
	task_id    INTEGER NOT NULL,
	message_id INTEGER NOT NULL,
	fired_at   INTEGER NOT NULL,
	PRIMARY KEY (task_id, message_id)
);
`

// mailTriggerMaxPerTask caps how many messages one task may fire on in a single
// sync pass. A mailbox that suddenly receives 200 matching messages (a
// newsletter burst, a mailing list) must not start 200 AI turns; the first few
// run and the rest are recorded as skipped.
const mailTriggerMaxPerTask = 5

// mailTriggerBodyTimeout bounds the on-demand body fetch used by the body gate.
const mailTriggerBodyTimeout = 30 * time.Second

// taskStatusActive is the only status an email task fires in. Paused and
// completed tasks keep their configuration but are skipped.
const taskStatusActive = "active"

// MailEventContext is what an email-triggered run knows about the message that
// caused it. It is rendered as a read-only block above the task's prompt, the
// same shape as the forge EventContext.
type MailEventContext struct {
	AccountName string
	Folder      string
	From        string
	To          string
	Subject     string
	Date        string
	Body        string
	// MessageID is the cached mail_messages row id, useful for logging.
	MessageID int64
	// URL is empty for mail (there is no public link); kept so the execution
	// payload writer has one shape for both trigger kinds.
	URL string
}

// RenderMailEventContext renders the block prepended to the task prompt.
func RenderMailEventContext(mc MailEventContext) string {
	var b strings.Builder
	b.WriteString("## " + backgroundText("MailContextHeader") + "\n")
	writeCtxLine(&b, backgroundText("MailContextAccount"), mc.AccountName)
	writeCtxLine(&b, backgroundText("MailContextFolder"), mc.Folder)
	writeCtxLine(&b, backgroundText("MailContextFrom"), mc.From)
	writeCtxLine(&b, backgroundText("MailContextTo"), mc.To)
	writeCtxLine(&b, backgroundText("MailContextSubject"), mc.Subject)
	writeCtxLine(&b, backgroundText("MailContextDate"), mc.Date)
	if body := strings.TrimSpace(mc.Body); body != "" {
		b.WriteString("- " + backgroundText("MailContextBody") + ":\n")
		for _, line := range strings.Split(body, "\n") {
			b.WriteString("  > " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeCtxLine(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString("- " + label + ": " + value + "\n")
}

// applyMailTemplateVars substitutes the mail placeholders a task prompt may use.
// Unknown placeholders are left untouched (same convention as the script
// variables), so a prompt that mentions {{something}} keeps it verbatim.
func applyMailTemplateVars(prompt string, mc MailEventContext) string {
	replacer := strings.NewReplacer(
		"{{mailSubject}}", mc.Subject,
		"{{mailFrom}}", mc.From,
		"{{mailTo}}", mc.To,
		"{{mailDate}}", mc.Date,
		"{{mailBody}}", mc.Body,
		"{{mailFolder}}", mc.Folder,
	)
	return replacer.Replace(prompt)
}

// validateEmailTask checks the configuration of an email-triggered task.
//
// The folder is NOT validated: it is chosen from the account's synced folders,
// but a folder can be deleted on the server afterwards, and refusing to save an
// unrelated edit because of that would be hostile.
func validateEmailTask(task *model.ScheduledTask) error {
	if task.MailAccountID <= 0 {
		return errors.New("email trigger requires a mailbox account")
	}
	if _, err := GetMailAccount(task.MailAccountID); err != nil {
		return fmt.Errorf("mailbox account not found: %w", err)
	}
	return nil
}

// mailTaskFire starts one email-triggered run. It is a variable so tests can
// record matches without spawning an AI backend; production always goes through
// the global scheduler.
var mailTaskFire = func(taskID int64, mc MailEventContext) bool {
	if GlobalScheduler == nil {
		return false
	}
	return GlobalScheduler.TriggerTaskWithMailContext(taskID, mc)
}

// FireMailTaskTriggers evaluates every active email task against messages that
// just entered the cache.
//
// It is called from the sync path but runs on its own goroutine: a body gate
// may need an IMAP fetch, and an AI turn takes minutes — neither belongs on the
// connection that is trying to finish a folder sync.
func FireMailTaskTriggers(accountID int64, folderPath string, messages []MailMessage) {
	if len(messages) == 0 {
		return
	}
	go evaluateMailTaskTriggers(accountID, folderPath, messages)
}

func evaluateMailTaskTriggers(accountID int64, folderPath string, messages []MailMessage) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("mail task trigger panicked", "panic", r)
		}
	}()

	tasks, err := GetTasks("")
	if err != nil {
		slog.Warn("mail task trigger: list tasks failed", "error", err)
		return
	}

	acc, err := GetMailAccount(accountID)
	if err != nil {
		slog.Warn("mail task trigger: account missing", "account", accountID, "error", err)
		return
	}
	folderName := folderPath
	if folder, ferr := GetMailFolderByPath(accountID, folderPath); ferr == nil && folder.Name != "" {
		folderName = folder.Name
	}

	for _, task := range tasks {
		if !task.IsEmailTriggered() || task.Status != taskStatusActive {
			continue
		}
		if task.MailAccountID != accountID {
			continue
		}
		if task.MailFolder != "" && task.MailFolder != folderPath {
			continue
		}
		fireForTask(task, acc, folderName, folderPath, messages)
	}
}

func fireForTask(task model.ScheduledTask, acc MailAccount, folderName, folderPath string, messages []MailMessage) {
	fired := 0
	// Newest first: if the cap is hit, the messages the user is most likely to
	// care about are the ones that ran.
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if fired >= mailTriggerMaxPerTask {
			slog.Info("mail task trigger: per-pass cap reached",
				"task", task.ID, "skipped", i+1, "cap", mailTriggerMaxPerTask)
			return
		}
		body := ""
		if strings.TrimSpace(task.MailBody) != "" {
			body = loadMailBodyForGate(msg)
			if body == "" {
				continue
			}
		}
		if !mailMatchesTask(task, msg, body) {
			continue
		}
		claimed, err := claimMailTrigger(task.ID, msg.ID)
		if err != nil {
			slog.Warn("mail task trigger: claim failed", "task", task.ID, "message", msg.ID, "error", err)
			continue
		}
		if !claimed {
			continue // already fired for this message
		}
		ctx := MailEventContext{
			AccountName: acc.Name,
			Folder:      folderName,
			From:        formatMailSender(msg),
			To:          joinMailAddrs(msg.To),
			Subject:     msg.Subject,
			Date:        formatMailTime(msg.SentAt),
			Body:        mailGateBody(msg, body),
			MessageID:   msg.ID,
		}
		if !mailTaskFire(task.ID, ctx) {
			// The task is busy; release the claim so the next sync can retry.
			releaseMailTrigger(task.ID, msg.ID)
			slog.Info("mail task trigger: task busy, deferred", "task", task.ID, "message", msg.ID)
			continue
		}
		fired++
	}
	_ = folderPath
}

// mailMatchesTask applies the gates. Every configured condition must match
// (AND); an empty condition is not a constraint, so a task with no conditions
// fires for every new message in its folder.
func mailMatchesTask(task model.ScheduledTask, msg MailMessage, body string) bool {
	if task.MailHasAttachment && !msg.HasAttachments {
		return false
	}
	if !containsFold(formatMailSender(msg), task.MailFrom) {
		return false
	}
	if !containsFold(msg.Subject, task.MailSubject) {
		return false
	}
	if !containsFold(body, task.MailBody) {
		return false
	}
	return true
}

func containsFold(haystack, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func formatMailSender(msg MailMessage) string {
	if msg.FromName == "" {
		return msg.FromAddr
	}
	if msg.FromAddr == "" {
		return msg.FromName
	}
	return msg.FromName + " <" + msg.FromAddr + ">"
}

func joinMailAddrs(addrs []MailAddr) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Addr != "" {
			parts = append(parts, a.Addr)
		}
	}
	return strings.Join(parts, ", ")
}

func formatMailTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04")
}

// mailGateBody returns the text handed to the prompt: the decoded body when we
// have it, otherwise the list snippet. It is truncated so a 2 MB newsletter
// does not blow up the prompt.
func mailGateBody(msg MailMessage, body string) string {
	const maxRunes = 4000
	text := body
	if strings.TrimSpace(text) == "" {
		text = msg.Snippet
	}
	r := []rune(strings.TrimSpace(text))
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "\n" + backgroundText("MailContextTruncated")
	}
	return string(r)
}

// loadMailBodyForGate returns the decoded text body, fetching it when the cache
// only holds headers. A failure yields "" and the message is skipped rather
// than firing on an unverifiable match.
func loadMailBodyForGate(msg MailMessage) string {
	if msg.BodyFetched {
		text, _, _, err := GetMailMessageBody(msg.ID)
		if err != nil {
			return ""
		}
		return text
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailTriggerBodyTimeout)
	defer cancel()
	if err := fetchAndStoreMailBody(ctx, msg, false); err != nil {
		slog.Debug("mail task trigger: body fetch failed", "message", msg.ID, "error", err)
		return ""
	}
	text, _, _, err := GetMailMessageBody(msg.ID)
	if err != nil {
		return ""
	}
	return text
}

// claimMailTrigger records that taskID has fired for messageID. It returns
// false when another pass already claimed the pair.
func claimMailTrigger(taskID, messageID int64) (bool, error) {
	res, err := store.WriteExec(`INSERT OR IGNORE INTO mail_task_triggers (task_id, message_id, fired_at)
		VALUES (?, ?, ?)`, taskID, messageID, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func releaseMailTrigger(taskID, messageID int64) {
	_, _ = store.WriteExec(`DELETE FROM mail_task_triggers WHERE task_id = ? AND message_id = ?`,
		taskID, messageID)
}
