package service

import (
	"os"
	"testing"

	"clawbench/internal/model"
)

// TestMain pins model.Language to the production default (Chinese).
//
// Several service strings are localized at WRITE time from a background
// goroutine with no HTTP request in scope — the group-timeline system rows
// (member joined/left, the unknown-mention notice) and the forge event-context
// block — via i18n.LocalizerForLocale(model.Language). In a bare test binary
// model.Language is the zero value "", which the localizer resolves to English,
// so assertions on the Chinese wording would fail even though production (which
// sets model.Language from config, defaulting to zh) renders Chinese.
//
// Tests that need another locale set model.Language themselves and restore it
// (see auto_continue_test.go), so this only fixes the baseline.
func TestMain(m *testing.M) {
	model.Language = model.DefaultLanguage
	os.Exit(m.Run())
}
