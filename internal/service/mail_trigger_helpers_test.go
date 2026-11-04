package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
)

var errNoDial = errors.New("no dial")

func TestRenderMailEventContext(t *testing.T) {
	got := RenderMailEventContext(MailEventContext{
		AccountName: "工作邮箱",
		Folder:      "收件箱",
		From:        "Boss <boss@example.com>",
		To:          "me@example.com",
		Subject:     "周报",
		Date:        "2026-11-04 09:00",
		Body:        "第一行\n第二行",
	})
	for _, want := range []string{"工作邮箱", "收件箱", "boss@example.com", "周报", "2026-11-04 09:00", "> 第一行", "> 第二行"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context missing %q:\n%s", want, got)
		}
	}
	// Empty fields are dropped rather than rendered as "- 发件人: ".
	if strings.Contains(got, "发件人: \n") {
		t.Fatalf("empty fields should be omitted:\n%s", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("trailing newline should be trimmed")
	}
}

func TestRenderMailEventContextOmitsEmptyBody(t *testing.T) {
	got := RenderMailEventContext(MailEventContext{Subject: "只有主题"})
	if strings.Contains(got, ">") {
		t.Fatalf("no body means no quote block:\n%s", got)
	}
}

func TestApplyMailTemplateVars(t *testing.T) {
	mc := MailEventContext{
		Subject: "周报", From: "boss@example.com", To: "me@example.com",
		Date: "2026-11-04", Body: "正文", Folder: "收件箱",
	}
	got := applyMailTemplateVars(
		"主题={{mailSubject}} 来自={{mailFrom}} 收件人={{mailTo}} 时间={{mailDate}} 正文={{mailBody}} 文件夹={{mailFolder}}",
		mc,
	)
	for _, want := range []string{"主题=周报", "来自=boss@example.com", "收件人=me@example.com", "时间=2026-11-04", "正文=正文", "文件夹=收件箱"} {
		if !strings.Contains(got, want) {
			t.Fatalf("replacement missing %q: %s", want, got)
		}
	}
	// Unknown placeholders stay verbatim: they may belong to another feature.
	if got := applyMailTemplateVars("{{somethingElse}}", mc); got != "{{somethingElse}}" {
		t.Fatalf("unknown placeholder was rewritten: %q", got)
	}
}

func TestFormatMailHelpers(t *testing.T) {
	cases := []struct {
		msg  MailMessage
		want string
	}{
		{MailMessage{FromName: "张三", FromAddr: "z@example.com"}, "张三 <z@example.com>"},
		{MailMessage{FromAddr: "z@example.com"}, "z@example.com"},
		{MailMessage{FromName: "张三"}, "张三"},
		{MailMessage{}, ""},
	}
	for _, c := range cases {
		if got := formatMailSender(c.msg); got != c.want {
			t.Errorf("formatMailSender(%+v) = %q, want %q", c.msg, got, c.want)
		}
	}

	got := joinMailAddrs([]MailAddr{{Addr: "a@x.com"}, {Addr: ""}, {Name: "no addr"}, {Addr: "b@x.com"}})
	if got != "a@x.com, b@x.com" {
		t.Fatalf("joinMailAddrs = %q", got)
	}

	if got := formatMailTime(0); got != "" {
		t.Fatalf("a zero timestamp should render empty, got %q", got)
	}
	want := time.Unix(1700000000, 0).Format("2006-01-02 15:04")
	if got := formatMailTime(1700000000); got != want {
		t.Fatalf("formatMailTime = %q, want %q", got, want)
	}
}

func TestContainsFold(t *testing.T) {
	if !containsFold("Hello World", "hello") {
		t.Fatal("matching should be case-insensitive")
	}
	if !containsFold("anything", "   ") {
		t.Fatal("a blank needle is not a constraint")
	}
	if containsFold("Hello", "bye") {
		t.Fatal("non-matching needle should not match")
	}
}

func TestMailGateBodyTruncatesAndFallsBackToSnippet(t *testing.T) {
	msg := MailMessage{Snippet: "摘要"}
	if got := mailGateBody(msg, ""); got != "摘要" {
		t.Fatalf("empty body should fall back to the snippet, got %q", got)
	}

	long := strings.Repeat("字", 4100)
	got := mailGateBody(msg, long)
	// 4000 runes of body plus the localized "truncated" marker.
	if !strings.Contains(got, backgroundText("MailContextTruncated")) {
		t.Fatalf("truncation marker missing: %q", got[len(got)-40:])
	}
	if len([]rune(got)) < 4000 {
		t.Fatalf("body was cut too aggressively (len %d)", len([]rune(got)))
	}

	if got := mailGateBody(msg, "  body  "); got != "body" {
		t.Fatalf("body should be trimmed, got %q", got)
	}
}

func TestMailMatchesTaskGates(t *testing.T) {
	msg := MailMessage{
		Subject: "Weekly Report", FromName: "Boss", FromAddr: "boss@example.com",
		HasAttachments: true,
	}
	cases := []struct {
		name string
		task model.ScheduledTask
		body string
		want bool
	}{
		{"no gates", model.ScheduledTask{}, "", true},
		{"attachment required and present", model.ScheduledTask{MailHasAttachment: true}, "", true},
		{"subject matches case-insensitively", model.ScheduledTask{MailSubject: "weekly"}, "", true},
		{"subject misses", model.ScheduledTask{MailSubject: "invoice"}, "", false},
		{"from matches the display name", model.ScheduledTask{MailFrom: "boss"}, "", true},
		{"from misses", model.ScheduledTask{MailFrom: "alice"}, "", false},
		{"body matches", model.ScheduledTask{MailBody: "关键字"}, "这里有关键字", true},
		{"body misses", model.ScheduledTask{MailBody: "关键字"}, "没有", false},
		{"all gates must pass", model.ScheduledTask{MailSubject: "weekly", MailFrom: "alice"}, "", false},
	}
	for _, c := range cases {
		if got := mailMatchesTask(c.task, msg, c.body); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	noAtt := MailMessage{Subject: "x"}
	if mailMatchesTask(model.ScheduledTask{MailHasAttachment: true}, noAtt, "") {
		t.Error("a message without attachments must not pass the attachment gate")
	}
}

func TestClaimAndReleaseMailTrigger(t *testing.T) {
	setupMailDB(t)

	claimed, err := claimMailTrigger(1, 2)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Fatal("the first claim should win")
	}
	again, err := claimMailTrigger(1, 2)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if again {
		t.Fatal("a second pass must not claim the same pair")
	}

	// Releasing (the task was busy) makes the pair claimable again.
	releaseMailTrigger(1, 2)
	reclaimed, err := claimMailTrigger(1, 2)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !reclaimed {
		t.Fatal("a released pair should be claimable")
	}
}

func TestEvaluateMailTaskTriggersUnknownAccount(t *testing.T) {
	setupMailDB(t)
	// The account disappeared between the sync and the goroutine: the pass must
	// log and return rather than panic.
	evaluateMailTaskTriggers(999, "INBOX", []MailMessage{{ID: 1, Subject: "x"}})
}

func TestFireMailTaskTriggersWithNoMessages(t *testing.T) {
	setupMailDB(t)
	// No messages means no goroutine and no work.
	FireMailTaskTriggers(1, "INBOX", nil)
}

func TestValidateEmailTaskMissingAccount(t *testing.T) {
	setupMailDB(t)
	task := &model.ScheduledTask{TriggerMode: "email", MailAccountID: 0}
	if err := validateEmailTask(task); err == nil {
		t.Fatal("an email task without an account must be rejected")
	}
	task.MailAccountID = 999
	if err := validateEmailTask(task); err == nil {
		t.Fatal("an unknown account must be rejected")
	}
	acc := newTestAccount(t)
	task.MailAccountID = acc.ID
	if err := validateEmailTask(task); err != nil {
		t.Fatalf("a valid account should pass: %v", err)
	}
}

func TestLoadMailBodyForGateCachedAndMissing(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	msg.BodyFetched = true
	if err := SaveMailMessageBody(msg.ID, "缓存正文", "", "缓存正文"); err != nil {
		t.Fatal(err)
	}
	if got := loadMailBodyForGate(msg); got != "缓存正文" {
		t.Fatalf("cached body = %q", got)
	}

	// Not fetched and no server: the gate must yield "" so the message is
	// skipped instead of firing on an unverifiable match.
	unfetched := MailMessage{ID: msg.ID, AccountID: acc.ID, FolderID: msg.FolderID, UID: msg.UID}
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		return nil, errNoDial
	}
	t.Cleanup(func() { mailDialer = prev })
	if got := loadMailBodyForGate(unfetched); got != "" {
		t.Fatalf("a failed fetch should yield an empty body, got %q", got)
	}
}

func TestSetMailMessageFlagsFlaggedOnly(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{}
	installFakeDialer(t, f)

	flagged := true
	got, err := SetMailMessageFlags(context.Background(), msg.ID, MailFlagPatch{Flagged: &flagged})
	if err != nil {
		t.Fatalf("flag: %v", err)
	}
	if !got.Flagged || got.Seen {
		t.Fatalf("only \\Flagged should change: %+v", got)
	}
	if len(f.flagOps) != 1 || !strings.Contains(f.flagOps[0], "\\Flagged") {
		t.Fatalf("expected a single \\Flagged push, got %+v", f.flagOps)
	}
}

func TestSetMailMessageFlagsUnknownMessage(t *testing.T) {
	setupMailDB(t)
	seen := true
	if _, err := SetMailMessageFlags(context.Background(), 999, MailFlagPatch{Seen: &seen}); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestSetMailMessageFlagsDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) { return nil, errNoDial }
	t.Cleanup(func() { mailDialer = prev })

	seen := true
	if _, err := SetMailMessageFlags(context.Background(), msg.ID, MailFlagPatch{Seen: &seen}); err == nil {
		t.Fatal("expected the dial failure to surface")
	}
}

func TestMarkMailFolderReadUnknownFolder(t *testing.T) {
	setupMailDB(t)
	if err := MarkMailFolderRead(context.Background(), 999); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestMarkMailFolderReadDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, _ := seedOneMessage(t, acc.ID)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) { return nil, errNoDial }
	t.Cleanup(func() { mailDialer = prev })

	if err := MarkMailFolderRead(context.Background(), folder.ID); err == nil {
		t.Fatal("expected the dial failure to surface")
	}
}

func TestDeleteMailMessageServerFailureKeepsRow(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	seedFolder(f, folder.Path, mail.SpecialUseInbox)
	f.raw[folder.Path] = map[uint32][]byte{}
	f.failOn = "delete"
	installFakeDialer(t, f)

	if err := DeleteMailMessage(context.Background(), msg.ID, true); err == nil {
		t.Fatal("expected the server rejection to surface")
	}
	if _, err := GetMailMessage(msg.ID); err != nil {
		t.Fatal("a failed delete must keep the cached row")
	}
}

func TestMoveMailMessageServerFailureKeepsRow(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	destID, err := UpsertMailFolder(MailFolder{AccountID: acc.ID, Path: "Archive", Name: "Archive", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{}
	f.failOn = "move"
	installFakeDialer(t, f)

	if err := MoveMailMessage(context.Background(), msg.ID, destID); err == nil {
		t.Fatal("expected the server rejection to surface")
	}
	if _, err := GetMailMessage(msg.ID); err != nil {
		t.Fatal("a failed move must keep the cached row")
	}
}

func TestReadOutgoingAttachmentRejectsDirectoryAndOversize(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := readOutgoingAttachment(MailOutgoingAttachment{Filename: "dir", Path: dir}); err == nil {
		t.Fatal("a directory is not an attachment")
	}

	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(mailMaxAttachmentBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, _, err := readOutgoingAttachment(MailOutgoingAttachment{Filename: "big.bin", Path: big}); err == nil {
		t.Fatal("an oversized attachment must be rejected")
	}

	small := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(small, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, name, err := readOutgoingAttachment(MailOutgoingAttachment{Path: small})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("data = %q", data)
	}
	// An empty display name falls back to the file's base name.
	if name != "note.txt" {
		t.Fatalf("name = %q", name)
	}
}

func TestSendMailUnknownAccount(t *testing.T) {
	setupMailDB(t)
	err := SendMail(context.Background(), MailSendRequest{
		AccountID: 999, To: []string{"a@example.com"}, Text: "x",
	})
	if err == nil {
		t.Fatal("expected not-found")
	}
}
