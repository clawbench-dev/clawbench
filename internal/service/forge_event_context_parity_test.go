package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventContextLabelsMatchFrontendRegistry locks the backend's label keys
// and their wording to the frontend registry that previews the SAME block.
//
// The two used to be independent tables — the backend held Chinese literals
// while the frontend rendered i18n keys — so an English user's task-form
// preview and the prompt actually injected into the session disagreed. The
// labels are now keyed on both sides; this guards the keys AND the resolved
// text so they cannot drift apart again.
func TestEventContextLabelsMatchFrontendRegistry(t *testing.T) {
	// Restore via Cleanup, not a trailing assignment: a require/assert failure
	// mid-loop would otherwise return early and leak the last locale into every
	// subsequent test in the package.
	t.Cleanup(func() { model.Language = model.DefaultLanguage })

	beKeys := make([]string, 0, len(eventContextVars))
	for _, v := range eventContextVars {
		beKeys = append(beKeys, v.labelKey)
	}

	// Parse the frontend registry's labelKey values.
	feRaw := readFrontendRegistry(t)
	feMatches := regexp.MustCompile(`labelKey:\s*'task\.form\.var(\w+)'`).FindAllStringSubmatch(feRaw, -1)
	feKeys := make([]string, 0, len(feMatches))
	for _, m := range feMatches {
		feKeys = append(feKeys, "EventVar"+m[1])
	}
	require.NotEmpty(t, feKeys, "failed to parse the frontend registry")

	// Same keys, same order (the order is what makes the preview read like the
	// real prompt).
	assert.Equal(t, feKeys, beKeys,
		"backend labelKeys must mirror the frontend registry's, in order")

	// The resolved wording must match the frontend locale files too.
	for _, loc := range []struct{ code, path string }{
		{"zh", filepath.Join("..", "..", "web", "src", "i18n", "locales", "zh.ts")},
		{"en", filepath.Join("..", "..", "web", "src", "i18n", "locales", "en.ts")},
	} {
		feText := parseFrontendVarLabels(t, loc.path)
		model.Language = loc.code
		for _, v := range eventContextVars {
			want, ok := feText[v.labelKey]
			require.True(t, ok, "%s: frontend locale missing %s", loc.code, v.labelKey)
			assert.Equal(t, want, eventContextLabel(v),
				"%s: label %s disagrees between backend and frontend", loc.code, v.labelKey)
		}
		// The label/value separator must agree too — the frontend interpolates
		// task.form.eventContextSeparator, so a mismatch makes the preview read
		// differently from the injected prompt.
		feSep := parseFrontendScalar(t, loc.path, "eventContextSeparator")
		require.NotEmpty(t, feSep, "%s: frontend locale missing eventContextSeparator", loc.code)
		assert.Equal(t, feSep, eventContextSeparator(),
			"%s: label/value separator disagrees between backend and frontend", loc.code)
		feHeader := parseFrontendScalar(t, loc.path, "eventContextHeader")
		assert.Equal(t, feHeader, eventContextText("EventContextHeader"),
			"%s: block header disagrees between backend and frontend", loc.code)
		// The yes/no flag values are injected by the backend and previewed by
		// the card's sample rows. A mismatch here is the same class of defect
		// the labels guard against: the preview would show the other language.
		for _, pair := range []struct{ key, backendKey string }{
			{"eventContextYes", "EventContextYes"},
			{"eventContextNo", "EventContextNo"},
		} {
			feVal := parseFrontendScalar(t, loc.path, pair.key)
			require.NotEmpty(t, feVal, "%s: frontend locale missing %s", loc.code, pair.key)
			assert.Equal(t, feVal, eventContextText(pair.backendKey),
				"%s: %s disagrees between backend and frontend", loc.code, pair.backendKey)
		}
	}
}

// readFrontendRegistry reads web/src/utils/forgeEventContextVars.ts.
func readFrontendRegistry(t *testing.T) string {
	t.Helper()
	for _, base := range []string{filepath.Join("..", ".."), filepath.Join("..", "..", "..")} {
		p := filepath.Join(base, "web", "src", "utils", "forgeEventContextVars.ts")
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	t.Skip("frontend registry not found (running outside the repo tree)")
	return ""
}

// parseFrontendVarLabels extracts `varX: '...'` entries from a locale file.
func parseFrontendVarLabels(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`var(\w+):\s*'([^']*)'`).FindAllStringSubmatch(string(b), -1) {
		out["EventVar"+m[1]] = strings.ReplaceAll(m[2], `\'`, `'`)
	}
	return out
}

// parseFrontendScalar extracts one `key: '...'` string from a locale file.
func parseFrontendScalar(t *testing.T, path, key string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	m := regexp.MustCompile(key + `:\s*'([^']*)'`).FindStringSubmatch(string(b))
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], `\'`, `'`)
}
