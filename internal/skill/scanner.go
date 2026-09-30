package skill

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// skillFileName is the file a skill directory must contain.
const skillFileName = "SKILL.md"

// SharedSkillsDir is the cross-tool skill directory, relative to $HOME. It is
// where `npx skills` installs and is read by Codex, Copilot, OpenCode,
// MiMo-Code, Qoder and dsh. It is scanned unconditionally (see scanSources) so
// those skills are found even when no installed agent declares the path.
const SharedSkillsDir = ".agents/skills"

// maxScanDepth bounds how deep ScanDir descends below the directory it was
// given.
//
// Skill repositories are NOT flat: the ecosystem convention groups skills by
// category, so real repositories look like
//
//	skills/productivity/grill-me/SKILL.md
//	skills/engineering/grill-with-docs/SKILL.md
//
// (both from mattpocock/skills, and exactly what `npx skills add` installs).
// A depth-1 scan finds none of them. The limit exists because the walk runs
// over a cloned repository, where a deeply nested or hostile tree would
// otherwise cost an unbounded number of syscalls.
const maxScanDepth = 6

// skipScanDirs are directory names the walk never descends into. They are
// either VCS metadata, dependency trees, or build output — never skill
// locations — and they are the directories that make an unbounded walk
// expensive.
var skipScanDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	"target":       true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// ScanDir scans dir for skill directories containing SKILL.md, tagging each
// result with src.
//
// The search is recursive up to maxScanDepth, because a skill is defined by
// having a SKILL.md — not by sitting at a particular depth. A non-root
// directory that is itself a skill is not descended into (its subdirectories
// hold the skill's own scripts/references, never nested skills).
//
// The scan root is also tested as a skill, because a repository whose root is
// a single skill is a supported layout (`npx skills add owner/repo`). It is
// still descended into as well: the root doubles as the container, so treating
// it as a skill must not hide the skills below it.
//
// Returns nil when dir does not exist or contains no valid skill.
//
// Not goroutine-safe with respect to concurrent writes to the same dir, but
// safe to call from multiple goroutines reading distinct dirs.
func ScanDir(dir string, src Source) []Skill {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		slog.Debug("skill scan: directory does not exist", "path", dir, "source", src.Key())
		return nil
	}

	var skills []Skill
	if s, ok := readSkill(dir, src, true); ok {
		skills = append(skills, s)
	}
	scanTree(dir, src, 0, &skills)
	if len(skills) == 0 {
		return nil
	}

	// Sort by name for deterministic output.
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Path < skills[j].Path
	})

	slog.Debug("skill scan: found skills", "path", dir, "count", len(skills), "source", src.Key())
	return skills
}

// scanTree walks one directory level, appending any skill it finds and
// recursing into ordinary subdirectories.
//
// depth counts levels below the original scan root: the root itself is depth 0,
// so its direct subdirectories are depth 1.
func scanTree(dir string, src Source, depth int, out *[]Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("skill scan: cannot read directory", "path", dir, "error", err)
		return
	}

	for _, entry := range entries {
		// entry.IsDir() is false for symlinks, so a symlinked directory is
		// skipped — the walk never follows links out of the tree.
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if skipScanDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}

		child := filepath.Join(dir, name)
		if s, ok := readSkill(child, src, false); ok {
			*out = append(*out, s)
			// A skill's subdirectories belong to that skill (scripts,
			// references, assets) — do not look for skills inside it.
			continue
		}

		if depth+1 < maxScanDepth {
			scanTree(child, src, depth+1, out)
		} else {
			slog.Debug("skill scan: depth limit reached, not descending further",
				"path", child, "max_depth", maxScanDepth)
		}
	}
}

// readSkill reports whether dir is a skill directory (it contains a readable,
// valid SKILL.md) and returns the parsed entry.
//
// isScanRoot must be true when dir is the scan root itself. The root is allowed
// to be a skill (the single-skill-repository layout), but the name-matches-
// directory rule cannot apply to it: the root's directory name is a temp dir or
// a slug, not a skill name, so requiring a match would reject every such
// repository.
func readSkill(dir string, src Source, isScanRoot bool) (Skill, bool) {
	skillFile := filepath.Join(dir, skillFileName)
	info, err := os.Stat(skillFile)
	if err != nil || info.IsDir() {
		return Skill{}, false
	}

	data, err := os.ReadFile(skillFile)
	if err != nil {
		slog.Debug("skill scan: cannot read skill file", "path", skillFile, "error", err)
		return Skill{}, false
	}

	name, description, ok := ParseFrontmatter(data)
	if !ok {
		slog.Debug("skill scan: no valid frontmatter in skill file", "path", skillFile)
		return Skill{}, false
	}

	// The spec requires name == parent directory name. A mismatch is not fatal
	// for injection (the table carries an explicit Path, so the agent can still
	// read the file), but it IS fatal for the slash-command menu, which
	// advertises the frontmatter name while the agent resolves by directory.
	// Flag it and let the consumer decide; warn so a user can fix the skill.
	mismatch := !isScanRoot && !nameMatchesDir(dir, name)
	if mismatch {
		slog.Warn("skill scan: frontmatter name does not match its directory name",
			"name", name, "dir", filepath.Base(dir), "path", skillFile)
	}

	return Skill{
		Name:         name,
		Description:  description,
		Path:         skillFile,
		Source:       src,
		NameMismatch: mismatch,
	}, true
}

// ParseFrontmatter parses YAML frontmatter from a SKILL.md file.
// Returns (name, description, true) on success, ("", "", false) on failure.
//
// Expected format:
//
//	---
//	name: skill-name
//	description: "Some description text"
//	---
//
// The frontmatter is parsed with yaml.v3, so multi-line YAML scalars
// (folded ">" / literal "|" styles) and quoted values are fully supported.
// Only the name and description keys are extracted; description is
// normalized to a single line with inner whitespace collapsed.
func ParseFrontmatter(data []byte) (string, string, bool) {
	content := string(data)

	// Opening --- must be at the start of the file (line 1, column 1).
	if !strings.HasPrefix(content, "---") {
		return "", "", false
	}
	afterOpen := content[3:]

	// Find closing --- (must be on its own line)
	closeIdx := strings.Index(afterOpen, "\n---")
	if closeIdx < 0 {
		return "", "", false
	}
	frontmatter := afterOpen[:closeIdx]

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		slog.Debug("skill scan: invalid YAML frontmatter", "error", err)
		return "", "", false
	}

	name, ok := frontmatterString(fm, "name")
	if !ok || name == "" {
		return "", "", false
	}
	description, ok := frontmatterString(fm, "description")
	if !ok || description == "" {
		return "", "", false
	}

	return name, description, true
}

// frontmatterString extracts a string value from parsed frontmatter.
// yaml.v3 unmarshals into map[string]any, so scalars arrive as concrete Go
// types. Only string values are accepted; maps, lists, and numbers are
// rejected (a numeric description is meaningless for a skill).
func frontmatterString(fm map[string]any, key string) (string, bool) {
	val, exists := fm[key]
	if !exists {
		return "", false
	}
	s, ok := val.(string)
	if !ok {
		return "", false
	}
	return collapseWhitespace(s), true
}

// collapseWhitespace trims the value and collapses internal runs of
// whitespace (including newlines from folded/literal scalars) into single
// spaces, so descriptions stay on one line for the markdown table and the
// slash-command menu.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// BuildSystemPromptSection renders skills as a markdown section for injection
// into an agent's system prompt. Returns "" when there are no skills.
//
// The table carries the SKILL.md path because a skill injected from another
// agent's directory is not something the target agent can discover on its own
// — it needs the path to read it.
func BuildSystemPromptSection(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	b.WriteString("The following skills are available in your environment. ")
	b.WriteString("When a skill's description matches the current task, ")
	b.WriteString("read its SKILL.md at the given path and follow its instructions.\n\n")
	b.WriteString("| Skill | Description | Path |\n")
	b.WriteString("|-------|-------------|------|\n")
	for _, s := range skills {
		b.WriteString("| ")
		b.WriteString(escapeTableCell(s.Name))
		b.WriteString(" | ")
		b.WriteString(escapeTableCell(s.Description))
		b.WriteString(" | ")
		b.WriteString(escapeTableCell(s.Path))
		b.WriteString(" |\n")
	}

	return b.String()
}

// escapeTableCell escapes a value for use inside a markdown table cell:
// pipes and backslashes are backslash-escaped, and newlines are replaced
// with spaces so a value cannot break the table structure.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

// ResolveNativeSkillsDir expands a BackendSpec.NativeSkillsDirs entry (relative
// to the user's home) into an absolute path. Returns "" when rel is empty or
// the home directory cannot be resolved.
func ResolveNativeSkillsDir(rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, rel)
}

// ResolveNativeSkillsDirs expands every declared native directory, dropping
// empties and duplicates (two entries can resolve to the same path).
func ResolveNativeSkillsDirs(rels []string) []string {
	var out []string
	seen := make(map[string]bool, len(rels))
	for _, rel := range rels {
		dir := ResolveNativeSkillsDir(rel)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// nameMatchesDir reports whether a skill's frontmatter name agrees with the
// directory it was found in, after NFKC normalization.
//
// The Agent Skills specification requires this ("must match parent directory
// name"), and it is load-bearing for us: an agent resolves a skill by
// DIRECTORY, while our injected table and slash-command menu are keyed by the
// frontmatter name. When the two disagree the menu advertises something the
// agent cannot actually open.
func nameMatchesDir(skillDir, name string) bool {
	want := norm.NFKC.String(filepath.Base(skillDir))
	got := norm.NFKC.String(name)
	// Tolerate a leading slash: some producers carry "/name".
	return strings.EqualFold(strings.TrimPrefix(got, "/"), want)
}
