package service

import (
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// setupMailTriggerDB reuses the mail fixture (which builds InitInMemoryDB plus
// the mail tables) — scheduled_tasks in that fixture carries the email-trigger
// columns.
func setupMailTriggerDB(t *testing.T) {
	t.Helper()
	setupMailDB(t)
}

// insertEmailTask seeds one active email-triggered task and returns its id.
func insertEmailTask(t *testing.T, accountID int64, from, subject, body string, wantAttachment bool) int64 {
	t.Helper()
	res, err := store.WriteExec(`INSERT INTO scheduled_tasks
		(project_id, name, cron_expr, agent_id, prompt, trigger_mode,
		 mail_account_id, mail_folder, mail_from, mail_subject, mail_body, mail_has_attachment, status)
		VALUES (0, '邮件任务', '', 'agent-1', '处理这封邮件', 'email', ?, '', ?, ?, ?, ?, 'active')`,
		accountID, from, subject, body, boolInt(wantAttachment))
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

type firedMail struct {
	taskID int64
	ctx    MailEventContext
}

// captureMailFires swaps the scheduler hook for the duration of a test.
func captureMailFires(t *testing.T) *[]firedMail {
	t.Helper()
	var fired []firedMail
	prev := mailTaskFire
	mailTaskFire = func(taskID int64, mc MailEventContext) bool {
		fired = append(fired, firedMail{taskID: taskID, ctx: mc})
		return true
	}
	t.Cleanup(func() { mailTaskFire = prev })
	return &fired
}

func insertCachedMail(t *testing.T, accountID int64, uid uint32, subject, from string) MailMessage {
	t.Helper()
	msgs := []MailMessage{{
		AccountID: accountID,
		FolderID:  1,
		UID:       uid,
		Subject:   subject,
		FromName:  "老板",
		FromAddr:  from,
		SentAt:    time.Now().Unix(),
	}}
	inserted, err := UpsertMailMessages(msgs)
	if err != nil {
		t.Fatalf("upsert message: %v", err)
	}
	if len(inserted) != 1 {
		t.Fatalf("expected one inserted message, got %d", len(inserted))
	}
	return inserted[0]
}

func TestMailTriggerFiresOncePerMessage(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	taskID := insertEmailTask(t, acc.ID, "boss@corp.com", "周报", "", false)
	fired := captureMailFires(t)

	msg := insertCachedMail(t, acc.ID, 1, "本周项目周报", "boss@corp.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg})

	if len(*fired) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(*fired))
	}
	got := (*fired)[0]
	if got.taskID != taskID {
		t.Fatalf("task id = %d, want %d", got.taskID, taskID)
	}
	if got.ctx.Subject != "本周项目周报" || got.ctx.From != "老板 <boss@corp.com>" {
		t.Fatalf("context = %+v", got.ctx)
	}

	// The same message must not fire again, even if it is evaluated again
	// (every sync re-reads the newest 100 headers).
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg})
	if len(*fired) != 1 {
		t.Fatalf("message fired twice: %d", len(*fired))
	}
}

func TestMailTriggerRequiresEveryGate(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "boss@corp.com", "周报", "", false)
	fired := captureMailFires(t)

	// Right subject, wrong sender → no fire (conditions are AND).
	msg := insertCachedMail(t, acc.ID, 1, "本周项目周报", "someone@else.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg})
	if len(*fired) != 0 {
		t.Fatalf("expected no fire on sender mismatch, got %d", len(*fired))
	}

	// Right sender, wrong subject → still no fire.
	msg2 := insertCachedMail(t, acc.ID, 2, "广告：买一送一", "boss@corp.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg2})
	if len(*fired) != 0 {
		t.Fatalf("expected no fire on subject mismatch, got %d", len(*fired))
	}

	// Both match → fire.
	msg3 := insertCachedMail(t, acc.ID, 3, "本周项目周报", "boss@corp.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg3})
	if len(*fired) != 1 {
		t.Fatalf("expected one fire when all gates match, got %d", len(*fired))
	}
}

func TestMailTriggerBodyGate(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "合同", false)
	fired := captureMailFires(t)

	msg := insertCachedMail(t, acc.ID, 1, "随手一封信", "a@b.com")
	if err := SaveMailMessageBody(msg.ID, "附件是盖章后的合同扫描件", "", "…"); err != nil {
		t.Fatalf("save body: %v", err)
	}
	msg.BodyFetched = true

	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg})
	if len(*fired) != 1 {
		t.Fatalf("expected body gate to match, got %d fires", len(*fired))
	}

	msg2 := insertCachedMail(t, acc.ID, 2, "另一封", "c@d.com")
	if err := SaveMailMessageBody(msg2.ID, "今天天气不错", "", "…"); err != nil {
		t.Fatalf("save body: %v", err)
	}
	msg2.BodyFetched = true
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg2})
	if len(*fired) != 1 {
		t.Fatalf("body gate matched a message it should not have: %d", len(*fired))
	}
}

func TestMailTriggerAttachmentGate(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "", true)
	fired := captureMailFires(t)

	plain := insertCachedMail(t, acc.ID, 1, "没有附件", "a@b.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{plain})
	if len(*fired) != 0 {
		t.Fatalf("attachment gate should skip a plain message")
	}

	msgs := []MailMessage{{
		AccountID: acc.ID, FolderID: 1, UID: 2, Subject: "带附件", FromAddr: "a@b.com",
		SentAt: time.Now().Unix(), HasAttachments: true,
	}}
	inserted, err := UpsertMailMessages(msgs)
	if err != nil || len(inserted) != 1 {
		t.Fatalf("upsert: %v %d", err, len(inserted))
	}
	evaluateMailTaskTriggers(acc.ID, "INBOX", inserted)
	if len(*fired) != 1 {
		t.Fatalf("expected attachment match, got %d", len(*fired))
	}
}

func TestMailTriggerCapsPerPass(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "", false)
	fired := captureMailFires(t)

	var batch []MailMessage
	for i := 1; i <= mailTriggerMaxPerTask+3; i++ {
		batch = append(batch, insertCachedMail(t, acc.ID, uint32(i), "批量", "a@b.com"))
	}
	evaluateMailTaskTriggers(acc.ID, "INBOX", batch)

	if len(*fired) != mailTriggerMaxPerTask {
		t.Fatalf("expected the per-pass cap (%d), got %d", mailTriggerMaxPerTask, len(*fired))
	}
}

func TestMailTriggerIgnoresOtherAccountsAndFolders(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	other := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "", false)
	fired := captureMailFires(t)

	msg := insertCachedMail(t, other.ID, 1, "别人的邮件", "a@b.com")
	evaluateMailTaskTriggers(other.ID, "INBOX", []MailMessage{msg})
	if len(*fired) != 0 {
		t.Fatalf("task fired for a different account")
	}
}

func TestMailTriggerBusyReleasesClaim(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "", false)

	prev := mailTaskFire
	mailTaskFire = func(int64, MailEventContext) bool { return false } // task busy
	t.Cleanup(func() { mailTaskFire = prev })

	msg := insertCachedMail(t, acc.ID, 1, "一封信", "a@b.com")
	evaluateMailTaskTriggers(acc.ID, "INBOX", []MailMessage{msg})

	// The claim must be released so the next pass can retry.
	var n int
	if err := store.ReadDB().QueryRow(`SELECT COUNT(*) FROM mail_task_triggers WHERE message_id = ?`, msg.ID).Scan(&n); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if n != 0 {
		t.Fatalf("busy fire left a claim behind: %d", n)
	}
}

func TestEmailTaskRoundTripsItsGates(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	id := insertEmailTask(t, acc.ID, "boss@corp.com", "周报", "合同", true)

	task, err := GetTaskByID(id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.TriggerMode != "email" || !task.IsEmailTriggered() {
		t.Fatalf("trigger mode = %q", task.TriggerMode)
	}
	if task.MailAccountID != acc.ID || task.MailFrom != "boss@corp.com" ||
		task.MailSubject != "周报" || task.MailBody != "合同" || !task.MailHasAttachment {
		t.Fatalf("gates did not round-trip: %+v", task)
	}
	if !task.HasEventSource() {
		t.Fatal("an email task must be treated as event-driven (no cron schedule)")
	}

	// Editing must persist the new gates through updateTask.
	task.MailSubject = "月报"
	if err := updateTask(task); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := GetTaskByID(id)
	if again.MailSubject != "月报" {
		t.Fatalf("update lost the gate: %q", again.MailSubject)
	}
}

func TestValidateEmailTask(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)

	task := &model.ScheduledTask{TriggerMode: "email"}
	if err := validateEmailTask(task); err == nil {
		t.Fatal("expected an error without an account")
	}

	task.MailAccountID = acc.ID
	if err := validateEmailTask(task); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}

	task.MailAccountID = 9999
	if err := validateEmailTask(task); err == nil {
		t.Fatal("expected an error for an unknown account")
	}
}
