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
