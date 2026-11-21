package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The frontend and the backend declare each plugin independently: TypeScript in
// web/src/plugins/<id>/plugin.ts, Go in internal/plugin/<id>. There is no shared
// code and no generated file, so the only thing keeping them in step is a
// handful of strings:
//
//   - the id (dock tab id == registry ID == directory name);
//   - the master switch (`feature: 'mail.enabled'` == Section "mail");
//   - the API prefix (frontend calls /api/mail/…, backend owns it).
//
// Nothing used to check those across the language boundary. A typo on either
// side is silent: the tab still renders but the switch never hides it, or the
// server thinks a feature exists that no UI can reach. This test is that check.
//
// It deliberately allows "frontend without backend": browse, view, history,
// proxy, settings and chat are pure UI entries whose APIs are mounted by the
// handler itself, so they have no plugin registration. The reverse (a backend
// plugin with no frontend entry) is always a mistake — nobody can reach it.
func TestPluginFrontendContract(t *testing.T) {
	backend := registeredPluginsForTest(t)
	frontend := readFrontendPlugins(t)

	byID := make(map[string]frontendPlugin, len(frontend))
	for _, fp := range frontend {
		require.NotEmptyf(t, fp.id, "%s/plugin.ts declares no id", fp.dir)
		require.NotContainsf(t, byID, fp.id, "two plugin directories declare id %q", fp.id)
		byID[fp.id] = fp
	}

	for _, p := range backend {
		fp, ok := byID[p.ID]
		require.Truef(t, ok,
			"backend plugin %q has no web/src/plugins/%s/plugin.ts — the feature is unreachable from the UI",
			p.ID, p.ID)
		assert.Equalf(t, p.ID, fp.dir,
			"web/src/plugins/%s declares id %q; the directory name must equal the id",
			fp.dir, fp.id)

		want := ""
		if p.Section != "" {
			want = p.Section + ".enabled"
		}
		assert.Equalf(t, want, fp.feature,
			"plugin %q: frontend feature must be %q (backend Section %q), got %q",
			p.ID, want, p.Section, fp.feature)
	}

	// A frontend switch with no backend owner can never be turned off: the
	// frontend asks /api/features for "<x>", gets nothing, and falls back to
	// "on" forever.
	sectionOf := make(map[string]string, len(backend))
	for _, p := range backend {
		sectionOf[p.ID] = p.Section
	}
	for _, fp := range frontend {
		if fp.feature == "" {
			continue
		}
		section, ok := strings.CutSuffix(fp.feature, ".enabled")
		require.Truef(t, ok,
			"plugin %q declares feature %q; the only supported shape is \"<section>.enabled\"",
			fp.id, fp.feature)
		owner, ok := sectionOf[section]
		require.Truef(t, ok,
			"plugin %q declares feature %q but no backend plugin has id %q — the switch can never be turned off",
			fp.id, fp.feature, section)
		assert.Equalf(t, section, owner,
			"plugin %q declares feature %q but backend plugin %q has Section %q",
			fp.id, fp.feature, section, owner)
	}
}

type frontendPlugin struct {
	// dir is the directory name; it must equal id (checked separately so a
	// rename shows up as "directory != id" rather than "no such plugin").
	dir string
	id  string
	// feature is the master switch the plugin gates on, "" when it has none.
	feature string
}

var (
	// Anchored at two-space indentation so only the default-export object's
	// top-level keys match, not nested objects (settingsPage, panel, …).
	frontendIDRe      = regexp.MustCompile(`(?m)^ {2}id: '([^']+)'`)
	frontendFeatureRe = regexp.MustCompile(`(?m)^ {2}feature: '([^']+)'`)

	tsBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	tsLineCommentRe  = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// readFrontendPlugins parses every plugin manifest with a deliberately small
// regex instead of a TypeScript parser: the files are hand-written object
// literals with one key per line, and a parser would drag a build step into the
// Go test suite. Comments are stripped first — several manifests explain their
// switch in prose, and a comment mentioning `feature:` must not be read as a
// declaration.
func readFrontendPlugins(t *testing.T) []frontendPlugin {
	t.Helper()
	paths, err := filepath.Glob("../../web/src/plugins/*/plugin.ts")
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no frontend plugin manifests found — has the directory moved?")

	out := make([]frontendPlugin, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoErrorf(t, err, "read %s", path)
		src := tsLineCommentRe.ReplaceAllString(tsBlockCommentRe.ReplaceAllString(string(raw), ""), "")
		out = append(out, frontendPlugin{
			dir:     filepath.Base(filepath.Dir(path)),
			id:      firstMatch(frontendIDRe, src),
			feature: firstMatch(frontendFeatureRe, src),
		})
	}
	return out
}

func firstMatch(re *regexp.Regexp, src string) string {
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
