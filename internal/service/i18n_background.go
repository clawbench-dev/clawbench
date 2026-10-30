package service

import (
	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// backgroundText localizes one string for a path that runs without an HTTP
// request — a scheduler goroutine, a forge event hook, an IM bot callback.
//
// Such paths cannot use the per-request localizer, so they read the language the
// frontend persists (model.Language) instead. Every one of them must go through
// this single helper: a second copy of the same three lines is how one caller
// silently keeps using a stale locale after the policy changes.
//
// templateData is variadic to match i18n.T; pass at most one map.
func backgroundText(messageID string, templateData ...map[string]any) string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), messageID, templateData...)
}
