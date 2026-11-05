package common

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

// common.T must follow model.Language at call time (not capture it once), since
// the frontend can switch the UI language while the server keeps running — the
// push backends read the persisted value on every message.
func TestT_FollowsModelLanguage(t *testing.T) {
	orig := model.Language
	defer func() { model.Language = orig }()

	model.Language = "en"
	en := T("PushMessageSent")
	model.Language = "zh"
	zh := T("PushMessageSent")

	if en == zh {
		t.Fatalf("T did not follow model.Language: en=%q zh=%q", en, zh)
	}
	if strings.Contains(zh, "Message sent") {
		t.Errorf("zh rendering leaked English: %q", zh)
	}
}

// A missing key falls back to the key itself (i18n.T's contract), so a typo is
// visible rather than rendering an empty message.
func TestT_MissingKeyFallsBackToKey(t *testing.T) {
	orig := model.Language
	defer func() { model.Language = orig }()
	model.Language = "en"

	if got := T("NoSuchPushKeyXYZ"); got != "NoSuchPushKeyXYZ" {
		t.Errorf("expected key fallback, got %q", got)
	}
}

// Template data is interpolated, not left as a literal placeholder.
func TestT_InterpolatesTemplateData(t *testing.T) {
	orig := model.Language
	defer func() { model.Language = orig }()
	model.Language = "en"

	got := T("PushSessionNotFound", map[string]any{"ID": "deadbeef"})
	if strings.Contains(got, "{{") {
		t.Errorf("placeholder not interpolated: %q", got)
	}
	if !strings.Contains(got, "deadbeef") {
		t.Errorf("expected the ID in the output, got %q", got)
	}
}

// FormatSessionLabel localizes its prefix and falls back to the short id when
// the session has no title.
func TestFormatSessionLabel_LocalizedPrefix(t *testing.T) {
	orig := model.Language
	defer func() { model.Language = orig }()

	model.Language = "en"
	if got := FormatSessionLabel("a1b2c3d4e5f6", ""); got != "Session a1b2c3d4" {
		t.Errorf("en label = %q", got)
	}
	model.Language = "zh"
	if got := FormatSessionLabel("a1b2c3d4e5f6", ""); got != "会话 a1b2c3d4" {
		t.Errorf("zh label = %q", got)
	}
	// A titled session is returned verbatim regardless of locale.
	if got := FormatSessionLabel("a1b2c3d4e5f6", "My Session"); got != "My Session" {
		t.Errorf("titled label = %q", got)
	}
}

// The reply must name the command: without it the user cannot tell which part
// of their request was dropped.
func TestCommandWithAttachmentText_NamesTheCommand(t *testing.T) {
	got := CommandWithAttachmentText("new")
	if !strings.Contains(got, "new") {
		t.Fatalf("reply must name the command, got %q", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("reply must not be empty")
	}
}

// /help is the only way an IM user discovers the commands, so every command the
// parser accepts must appear in the body. The text is hand-written prose, which
// is exactly why a command added without a matching line would stay invisible.
func TestHelpText_ListsEveryCommand(t *testing.T) {
	orig := model.Language
	defer func() { model.Language = orig }()

	words := []string{
		SessionListCommand,
		SessionShowCommand,
		SessionStopCommand,
		SessionNewCommand,
		SessionHelpCommand,
	}

	for _, lang := range []string{"en", "zh"} {
		model.Language = lang
		body := HelpText()
		if strings.TrimSpace(body) == "" || body == "PushHelpBody" {
			t.Fatalf("%s: help body is missing or untranslated: %q", lang, body)
		}
		// Each command must be its own list item. Markdown renderers (DingTalk,
		// Feishu cards) merge consecutive plain lines into a single paragraph, so
		// a one-line-per-command body without bullets arrives as one run-on blob.
		lines := strings.Split(body, "\n")
		for _, word := range words {
			documented := false
			for _, line := range lines {
				if strings.HasPrefix(line, "- ") && strings.Contains(line, word) {
					documented = true
					break
				}
			}
			if !documented {
				t.Errorf("%s: %q must be documented as its own list item, got body:\n%s", lang, word, body)
			}
		}
		// The "@{shortID}" form is not a command, but it is the other half of
		// the addressing model — a body without it omits how to target a session.
		if !strings.Contains(body, "@") {
			t.Errorf("%s: help body does not explain the @sessionID form", lang)
		}
	}
}
