package common

import (
	"os"
	"testing"

	"clawbench/internal/model"
)

// TestMain pins the push locale to Chinese, the production default
// (model.DefaultLanguage), so assertions on the Chinese wording keep matching
// what a default deployment renders. In a bare test binary model.Language is
// "", which resolves to English.
func TestMain(m *testing.M) {
	model.Language = model.DefaultLanguage
	os.Exit(m.Run())
}
