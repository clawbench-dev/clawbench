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
// Background: a scan found user-visible strings written as Chinese literals in
// both the frontend and the Go backend — the IM push bodies, group system rows,
// forge event-context labels, PDF/voice/split UI strings. They were all keyed
// into i18n (active.{en,zh}.yaml / web locales). This guard stops the list from
// growing again: it scans the source for CJK outside the locale tables and
// fails on any occurrence that is not in the explicit allowlist below.
//
// The allowlist is deliberately a list of FILE + REASON pairs, not a blanket
// path ignore, so adding a legitimate Chinese string is a conscious act with a
// justification rather than a silent path exclusion.

// cjkRe matches a Han character.
var cjkRe = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)

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
	// Backend registry metadata: the Specialty/Name fields ship from each
	// backend's registration and are surfaced in the agent picker. They are
	// product-provided agent descriptions, not UI chrome, and are intentionally
	// left in the language of the vendor that owns them.
	"internal/ai/backends/antigravity/register.go": "backend Specialty metadata",
	"internal/ai/backends/claude/cli.go":           "backend Specialty metadata",
	"internal/ai/backends/codebuddy/cli.go":        "backend Specialty metadata",
	"internal/ai/backends/codex/custom.go":         "backend Specialty metadata",
	"internal/ai/backends/copilot/cli.go":          "backend Specialty metadata",
	"internal/ai/backends/deepseek/cli.go":         "backend Specialty metadata",
	"internal/ai/backends/dsh/register.go":         "backend Specialty metadata",
	"internal/ai/backends/grok/register.go":        "backend Specialty metadata",
	"internal/ai/backends/kimi/cli.go":             "backend Specialty metadata",
	"internal/ai/backends/mimo/cli.go":             "backend Specialty metadata",
	"internal/ai/backends/opencode/cli.go":         "backend Specialty metadata",
	"internal/ai/backends/pi/cli.go":               "backend Specialty metadata",
	"internal/ai/backends/qoder/cli.go":            "backend Specialty metadata",
	"internal/ai/backends/vecli/custom.go":         "backend Specialty metadata",
	"internal/ai/backends/zcode/register.go":       "backend Specialty metadata",
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
// literals, so only real code (including ordinary string literals) remains.
//
// Raw strings are stripped because they hold SQL DDL whose `--` comments carry
// Chinese prose; that text is never rendered.
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

// TestNoNewHardcodedUserVisibleCJK fails on any CJK in Go source outside the
// allowlist and the locale tables. See the file header for the rationale.
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

// TestNoNewHardcodedUserVisibleCJKFrontend does the same for web/src, excluding
// the locale tables and tests. Only .ts/.js/.vue script+template bodies are
// scanned; SFC <style> blocks and comments are ignored.
func TestNoNewHardcodedUserVisibleCJKFrontend(t *testing.T) {
	root := repoRoot(t)
	srcRoot := filepath.Join(root, "web", "src")
	if _, err := os.Stat(srcRoot); err != nil {
		t.Skip("web/src not present")
	}

	// Frontend files whose CJK is legitimate. Each entry states why.
	allow := map[string]string{
		// The backend AI prompts/registry already carry Chinese; these are the
		// frontend mirrors of parser regexes and probes.
		"utils/askQuestion.ts":  "ordered-list marker regex (mirrors internal/askquestion)",
		"utils/groupRouting.ts": "mention-tag parser (mirrors internal/grouprouting)",
		"utils/fontConfig.ts":   "CJK font-availability probe text",
		// The event-context sample values mirror the backend's literal 是/否
		// rendering (see the file's own comment); they are data, not chrome.
		"utils/forgeEventContextVars.ts": "sample values mirroring backend 是/否",
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
		if strings.Contains(path, "__tests__") || strings.Contains(path, string(filepath.Separator)+"i18n"+string(filepath.Separator)+"locales") {
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
		body := string(b)
		if ext == ".vue" {
			body = vueTemplateAndScript(body)
		}
		body = stripGoComments(body) // same comment/string scanner works for JS-ish text
		if cjkRe.MatchString(body) {
			offenders = append(offenders, rel)
		}
		return nil
	})

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("CJK found in frontend source outside the allowlist:\n  %s\n\n"+
			"Move user-visible text into web/src/i18n/locales/{en,zh}.ts and reference it via t(). "+
			"If the string is genuinely not user-visible, add the file to the allow list in this test.",
			strings.Join(offenders, "\n  "))
	}
}

// vueTemplateAndScript returns an SFC's <template> and <script> bodies, dropping
// <style> (which may hold font names with CJK) and the SFC's own comments.
func vueTemplateAndScript(src string) string {
	var out strings.Builder
	for _, tag := range []string{"template", "script"} {
		re := regexp.MustCompile(`(?s)<` + tag + `[^>]*>(.*?)</` + tag + `>`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out.WriteString(m[1])
			out.WriteString("\n")
		}
	}
	// Strip HTML comments.
	return regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(out.String(), "")
}
