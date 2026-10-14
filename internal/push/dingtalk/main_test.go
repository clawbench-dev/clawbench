package dingtalk

import (
	"os"
	"testing"

	"clawbench/internal/model"
)

// TestMain pins the push locale to Chinese, the production default
// (model.DefaultLanguage).
//
// The push strings are localized through common.T -> i18n.LocalizerForLocale(
// model.Language). In a bare test binary model.Language is the zero value "",
// which the localizer resolves to English — so every assertion on the Chinese
// wording would fail even though production (which sets model.Language from
// config, defaulting to zh) renders Chinese. Pinning it here keeps the tests
// asserting the same output the default deployment produces.
func TestMain(m *testing.M) {
	model.Language = model.DefaultLanguage
	os.Exit(m.Run())
}
