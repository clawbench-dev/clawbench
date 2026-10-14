package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file is the "no new hardcoded user-visible CJK" guard.
//
// Background: an audit found user-visible strings written as Chinese literals
// in the frontend, the Go backend, the desktop main process and the IM push
// backends. They were all keyed into i18n (active.{en,zh}.yaml / web locales).
// These guards stop the list from growing: they scan the source for CJK outside
// the locale tables and fail on anything not in an explicit allowlist.
//
// The allowlist is a list of FILE + REASON pairs, not a blanket path ignore, so
// adding a legitimate Chinese string is a conscious act with a justification.
//
// Scanner scope (deliberately wide, see the tests below for the guarantees):
//   - Go:   internal/**/*.go   (comments and raw strings stripped)
//   - Web:  web/src/**/*.{ts,js,vue} + web/index.html + web/share.html
//   - Android: android/**/*.java (values/ must be English; values-zh/ is the
//     Chinese mirror and is excluded)
//
// The desktop main process has its own guard (desktop/src/main/i18nGuard.test.ts).

// cjkRe matches Han characters plus CJK punctuation and fullwidth forms.
//
// Han alone is not enough: a user-visible string can be punctuation-only (e.g.
// the "（specialty）" wrapper in group_store.go), and a Han-only regex let that
// through. The ranges are:
//   - \x{4e00}-\x{9fff}  CJK Unified Ideographs
//   - \x{3000}-\x{303f}  CJK punctuation (、。「」…)
//   - \x{ff00}-\x{ffef}  Fullwidth forms (（）：，)
//   - \x{fe30}-\x{fe4f}  CJK compatibility forms
var cjkRe = regexp.MustCompile(`[\x{4e00}-\x{9fff}\x{3000}-\x{303f}\x{ff00}-\x{ffef}\x{fe30}-\x{fe4f}]`)

// allowedCJKFiles maps a repo-relative path to the reason its CJK is legitimate
// (an AI prompt, a parser regex, a font probe — never a string shown to a human
// user). Keep this list small and justified.
var allowedCJKFiles = map[string]string{
	// AI-facing system/role prompts. The models are told to reply in the
	// user's language; the instruction text itself is authored in Chinese and
	// is not rendered to any user.
	"internal/service/group_prompt.go": "group-chat AI system prompts (host/member/free)",
	"internal/service/group_inject.go": "AI context-injection framing ([系统]/主持人… lines)",
	"internal/summarize/task.go":       "AI summarization prompt",
	"internal/summarize/recommend.go":  "AI recommendation prompt + its quick-command header",
	"internal/summarize/title.go":      "AI title-output prefix stripping (标题：)",
	// Parsers that must recognize CJK punctuation/numerals in model or CLI
	// output; the literals are matched, not displayed.
	"internal/askquestion/markdown.go":           "ordered-list marker regex (CJK numerals)",
	"internal/ai/backends/deepseek/discovery.go": "model-list output parser (（默认：/ 的模型)",
	"internal/rag/chunker.go":                    "sentence-boundary detector (。！？) for chunking",
	// Backend registry metadata — KNOWN GAP, not "not user-visible".
	//
	// `Specialty` IS shown to users (the onboarding agent picker reads it live
	// from the registry via /api/backends; the settings agent list reads a
	// one-time snapshot seeded into the user-editable agents.specialty column).
	// It is allowlisted rather than localized because a correct fix needs a
	// product decision: localize the registry (fixes the picker, but existing
	// installs keep their seeded snapshot) vs. re-seed per locale (would clobber
	// user edits). Tracked as a follow-up; listed here so the guard does not
	// silently hide it behind a false "not user-visible" claim.
	"internal/ai/backends/antigravity/register.go": "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/claude/cli.go":           "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/codebuddy/cli.go":        "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/codex/custom.go":         "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/copilot/cli.go":          "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/cursor/register.go":      "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/deepseek/cli.go":         "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/dsh/register.go":         "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/grok/register.go":        "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/hermes/register.go":      "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/kimi/cli.go":             "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/mimo/cli.go":             "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/opencode/cli.go":         "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/pi/cli.go":               "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/qoder/cli.go":            "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/vecli/custom.go":         "Specialty metadata (user-visible; localization pending a product decision)",
	"internal/ai/backends/zcode/register.go":       "Specialty metadata (user-visible; localization pending a product decision)",
}

// repoRoot walks up until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("go.mod not found — running outside the repo tree")
	return ""
}

// stripGoComments removes // and /* */ comments and the body of raw string
// literals, leaving ordinary ("…") string literals — where a user-visible Go
// string lives.
//
// Raw strings are stripped because in Go they hold SQL DDL and long prompts
// whose `--`/prose carries Chinese; that text is never rendered. (Go raw
// strings are NOT template literals — the JS scanner below keeps backtick
// bodies precisely because in JS they are.)
func stripGoComments(src string) string {
	var out strings.Builder
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		var next byte
		if i+1 < n {
			next = src[i+1]
		}
		switch {
		case c == '/' && next == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && next == '*':
			i += 2
			for i+1 < n && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			i += 2
		case c == '`':
			// Skip the raw string body entirely.
			i++
			for i < n && src[i] != '`' {
				i++
			}
			i++
		case c == '"' || c == '\'':
			quote := c
			out.WriteByte(c)
			i++
			for i < n {
				out.WriteByte(src[i])
				if src[i] == '\\' && i+1 < n {
					i++
					out.WriteByte(src[i])
					i++
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// stripJSComments removes // and /* */ comments but KEEPS string literals,
// including backtick template literals — in JS/TS a backtick body is where
// user-visible text most often lives, so it must be scanned.
func stripJSComments(src string) string {
	var out strings.Builder
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		var next byte
		if i+1 < n {
			next = src[i+1]
		}
		switch {
		case c == '/' && next == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && next == '*':
			i += 2
			for i+1 < n && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			i += 2
		case c == '"' || c == '\'' || c == '`':
			quote := c
			out.WriteByte(c)
			i++
			for i < n {
				out.WriteByte(src[i])
				if src[i] == '\\' && i+1 < n {
					i++
					out.WriteByte(src[i])
					i++
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// TestNoNewHardcodedUserVisibleCJK fails on any CJK in Go source outside the
// allowlist and the locale tables.
func TestNoNewHardcodedUserVisibleCJK(t *testing.T) {
	root := repoRoot(t)
	var offenders []string

	_ = filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// The allowlist keys are slash-separated repo paths; filepath.Rel uses
		// the OS separator (backslash on Windows), so normalize before lookup or
		// every allowlisted file is reported as an offender there.
		rel = filepath.ToSlash(rel)
		if _, ok := allowedCJKFiles[rel]; ok {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // unreadable file is skipped, not a scan failure
		}
		if cjkRe.MatchString(stripGoComments(string(b))) {
			offenders = append(offenders, rel)
		}
		return nil
	})

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("CJK found in Go source outside the allowlist:\n  %s\n\n"+
			"If the string is user-visible, move it into internal/i18n/locales/active.{en,zh}.yaml "+
			"and reference it by key. If it is genuinely not user-visible (an AI prompt, a parser "+
			"regex, a font probe), add the file to allowedCJKFiles in this test with a reason.",
			strings.Join(offenders, "\n  "))
	}
}

// TestNoNewHardcodedUserVisibleCJKFrontend scans web/src plus the top-level HTML
// entry points, excluding the locale tables and tests.
func TestNoNewHardcodedUserVisibleCJKFrontend(t *testing.T) {
	root := repoRoot(t)
	srcRoot := filepath.Join(root, "web", "src")
	if _, err := os.Stat(srcRoot); err != nil {
		t.Skip("web/src not present")
	}

	// Frontend files whose CJK is legitimate. Each entry states why.
	allow := map[string]string{
		// Frontend mirrors of backend parser regexes / probes.
		"utils/askQuestion.ts":      "ordered-list marker regex (mirrors internal/askquestion)",
		"utils/groupRouting.ts":     "mention-tag parser (mirrors internal/grouprouting)",
		"utils/fontConfig.ts":       "CJK font-availability probe text",
		"utils/doubleClickUtils.ts": "leading-marker stripper regex (CJK numerals/punctuation)",
		// Pre-existing ad-hoc bilingual strings: each already branches on the
		// locale (zh ? '…' : '…'), so neither language is stuck with the other's
		// text. Not the "Chinese-only, no English path" defect this guard
		// targets, but they SHOULD eventually move into the locale tables —
		// listed so the guard cannot hide a regression behind them.
		"composables/useLocale.ts":             "language-toggle label, intentionally native (EN / 中)",
		"utils/exportMarkdownHtml.ts":          "standalone export HTML embeds labels via ad-hoc isZh branch",
		"components/file/CodeMirrorViewer.vue": "CodeMirror search panel labels via ad-hoc zh branch",
	}

	var offenders []string
	_ = filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.Contains(path, "__tests__") ||
			strings.Contains(path, string(filepath.Separator)+"i18n"+string(filepath.Separator)+"locales") {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".js" && ext != ".vue" {
			return nil
		}
		if strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".test.js") {
			return nil
		}
		rel, _ := filepath.Rel(srcRoot, path)
		rel = filepath.ToSlash(rel)
		if _, ok := allow[rel]; ok {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // unreadable file is skipped, not a scan failure
		}
		if cjkRe.MatchString(scanFrontendSource(ext, string(b))) {
			offenders = append(offenders, rel)
		}
		return nil
	})

	// Top-level HTML entry points (SEO/share-preview text lives here).
	for _, rel := range []string{"web/index.html", "web/share.html"} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		// HTML comments are not rendered; the <style> block may hold font names.
		body := regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(string(b), "")
		body = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`).ReplaceAllString(body, "")
		if cjkRe.MatchString(body) {
			offenders = append(offenders, rel)
		}
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("CJK found in frontend source outside the allowlist:\n  %s\n\n"+
			"Move user-visible text into web/src/i18n/locales/{en,zh}.ts and reference it via t(). "+
			"If the string is genuinely not user-visible, add the file to the allow list in this test.",
			strings.Join(offenders, "\n  "))
	}
}

// scanFrontendSource returns the scannable body of a frontend file.
//
// A .vue file is scanned in full EXCEPT its <style> blocks. Extracting only the
// `<template>`/`<script>` blocks with a non-greedy regex silently truncated the
// scan at the first nested `<template #slot>` — 18+ files (some 85% of their
// body) were never checked. Whole-file-minus-style avoids that class entirely.
func scanFrontendSource(ext, src string) string {
	if ext == ".vue" {
		src = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`).ReplaceAllString(src, "")
		src = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(src, "")
	}
	return stripJSComments(src)
}

// TestNoNewHardcodedUserVisibleCJKAndroid scans Android Java for hardcoded CJK.
//
// Android native strings live in res/values (English) and res/values-zh
// (Chinese mirror); a Java literal is a string that bypassed the resource
// system and will not follow the locale. values-zh is excluded (its CJK is the
// point), values/ is NOT (an English resource file must not contain CJK).
func TestNoNewHardcodedUserVisibleCJKAndroid(t *testing.T) {
	root := repoRoot(t)
	androidRoot := filepath.Join(root, "android")
	if _, err := os.Stat(androidRoot); err != nil {
		t.Skip("android/ not present")
	}

	var offenders []string
	_ = filepath.Walk(androidRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Exclude the Chinese resource mirror and test sources.
		if strings.Contains(path, "values-zh") || strings.Contains(path, string(filepath.Separator)+"test"+string(filepath.Separator)) {
			return nil
		}
		if !strings.HasSuffix(path, ".java") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // unreadable file is skipped, not a scan failure
		}
		// Java: strip comments, keep string literals.
		if cjkRe.MatchString(stripJavaComments(string(b))) {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("CJK found in Android Java outside res/values-zh:\n  %s\n\n"+
			"Move native strings into res/values/strings.xml (English) + res/values-zh/strings.xml.",
			strings.Join(offenders, "\n  "))
	}
}

// stripJavaComments removes // and /* */ comments, keeping string literals.
func stripJavaComments(src string) string {
	// Java has no backtick strings; reuse the JS scanner (same comment syntax).
	return stripJSComments(src)
}

// TestCJKScannerCatchesKnownShapes is the scanner's own regression test: it
// feeds the exact shapes the first version missed and asserts they are caught.
// Without this, a future edit to the scanner could silently narrow it and every
// other test would still pass.
func TestCJKScannerCatchesKnownShapes(t *testing.T) {
	han := "加载中"
	fullwidth := "（产品经理）：、"

	// JS: backtick template literals must be scanned (the first version
	// stripped them, treating a backtick as a Go raw string).
	jsCases := []struct{ name, src string }{
		{"js single", `const s = '` + han + `'`},
		{"js double", `const s = "` + han + `"`},
		{"js backtick", "const s = `" + han + "`"},
		{"js backtick expr", "const s = `共 " + han + " ${n} 项`"},
		{"js fullwidth", `const s = '` + fullwidth + `'`},
		{"js template-literal comment-like", "const s = `a // " + han + "`"},
	}
	for _, tc := range jsCases {
		if !cjkRe.MatchString(stripJSComments(tc.src)) {
			t.Errorf("JS scanner missed %s: %q -> %q", tc.name, tc.src, stripJSComments(tc.src))
		}
	}

	// Go: ordinary string literals must be scanned; raw strings (SQL/prompts)
	// must NOT (their Chinese prose is not rendered).
	goCases := []struct{ name, src string }{
		{"go string", `s := "` + han + `"`},
		{"go fullwidth", `s := "` + fullwidth + `"`},
	}
	for _, tc := range goCases {
		if !cjkRe.MatchString(stripGoComments(tc.src)) {
			t.Errorf("Go scanner missed %s: %q", tc.name, tc.src)
		}
	}
	if cjkRe.MatchString(stripGoComments("s := `" + han + "`")) {
		t.Error("Go scanner must skip raw-string bodies (SQL/prompt prose)")
	}

	// .vue: a nested <template #slot> must not truncate the scan.
	vue := "<template>\n  <template #header>ok</template>\n  <p>" + han + "</p>\n</template>\n<style>.x{}</style>"
	if !cjkRe.MatchString(scanFrontendSource(".vue", vue)) {
		t.Error("Vue scanner truncated at a nested <template> and missed a later literal")
	}
	// <style> is excluded (font names may be Chinese).
	if cjkRe.MatchString(scanFrontendSource(".vue", "<template>ok</template><style>.x{content:'"+han+"'}</style>")) {
		t.Error("Vue scanner must exclude <style> content")
	}
}
