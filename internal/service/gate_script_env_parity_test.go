package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGateScriptVarsMatchFrontendRegistry locks the environment-variable
// registry to the task form's copy.
//
// The backend sets these names on the child process and the form documents
// them; if the two drift, the form promises a variable that is never set (or
// hides one that is). Names, labels and order must all agree.
func TestGateScriptVarsMatchFrontendRegistry(t *testing.T) {
	raw := readGateScriptRegistry(t)
	re := regexp.MustCompile(`\{ name: '([A-Z0-9_]+)', labelKey: '([^']+)', scope: '(\w+)' \}`)
	matches := re.FindAllStringSubmatch(raw, -1)
	require.NotEmpty(t, matches, "failed to parse web/src/utils/gateScriptVars.ts")

	require.Len(t, matches, len(gateScriptVars), "the frontend registry lists a different number of variables")

	scopes := map[gateScriptScope]string{
		gateScriptCommon: "common",
		gateScriptMail:   "mail",
		gateScriptEvent:  "event",
	}
	for i, m := range matches {
		be := gateScriptVars[i]
		assert.Equal(t, be.Name, m[1], "variable %d name drifted", i)
		assert.Equal(t, be.LabelKey, m[2], "variable %d label key drifted", i)
		assert.Equal(t, scopes[be.Scope], m[3], "variable %d scope drifted", i)
	}
}

// TestGateScriptVarLabelsExist asserts every label key the registry points at
// is actually translated. A missing key renders as the raw key path in the
// form, which reads as a bug to the user.
func TestGateScriptVarLabelsExist(t *testing.T) {
	for _, loc := range []struct{ code, path string }{
		{"zh", filepath.Join("..", "..", "web", "src", "i18n", "locales", "zh.ts")},
		{"en", filepath.Join("..", "..", "web", "src", "i18n", "locales", "en.ts")},
	} {
		b, err := os.ReadFile(loc.path)
		require.NoError(t, err, "read %s locale", loc.code)
		text := string(b)
		for _, v := range gateScriptVars {
			key := strings.TrimPrefix(v.LabelKey, "task.form.")
			re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*'[^']+'`)
			assert.Regexp(t, re, text, "%s locale is missing %s", loc.code, v.LabelKey)
		}
	}
}

// readGateScriptRegistry reads web/src/utils/gateScriptVars.ts.
func readGateScriptRegistry(t *testing.T) string {
	t.Helper()
	for _, base := range []string{filepath.Join("..", ".."), filepath.Join("..", "..", "..")} {
		p := filepath.Join(base, "web", "src", "utils", "gateScriptVars.ts")
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	t.Skip("frontend registry not found (running outside the repo tree)")
	return ""
}
