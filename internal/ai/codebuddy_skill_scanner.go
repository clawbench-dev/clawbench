package ai

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillInfo represents a discovered skill from ~/.codebuddy/skills/.
type SkillInfo struct {
	Name        string // skill name from YAML frontmatter
	Description string // description from YAML frontmatter
	Path        string // full path to the SKILL.md file
}

// ScanCodeBuddySkills scans ~/.codebuddy/skills/ for skill definitions.
// Each skill is a directory containing SKILL.md with YAML frontmatter.
// Returns nil if directory doesn't exist or no skills found.
func ScanCodeBuddySkills() []SkillInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("acp skill scan: cannot resolve home dir", "error", err)
		return nil
	}
	skillsDir := filepath.Join(home, ".codebuddy", "skills")
	return scanSkillsFromDir(skillsDir)
}

// scanSkillsFromDir scans the given directory for skill subdirectories containing SKILL.md.
// Separated for testability (injectable directory path).
func scanSkillsFromDir(skillsDir string) []SkillInfo {
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		slog.Debug("acp skill scan: skills directory does not exist", "path", skillsDir)
		return nil
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		slog.Debug("acp skill scan: cannot read skills directory", "path", skillsDir, "error", err)
		return nil
	}

	var skills []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, err := os.Stat(skillFile)
		if err != nil || info.IsDir() {
			continue
		}

		data, err := os.ReadFile(skillFile)
		if err != nil {
			slog.Debug("acp skill scan: cannot read skill file", "path", skillFile, "error", err)
			continue
		}

		name, description, ok := parseSkillFrontmatter(data)
		if !ok {
			slog.Debug("acp skill scan: no valid frontmatter in skill file", "path", skillFile)
			continue
		}

		skills = append(skills, SkillInfo{
			Name:        name,
			Description: description,
			Path:        skillFile,
		})
	}

	if len(skills) == 0 {
		return nil
	}

	// Sort by name for deterministic output
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	slog.Info("acp skill scan: found CodeBuddy skills", "count", len(skills))
	return skills
}

// parseSkillFrontmatter parses YAML frontmatter from a SKILL.md file.
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
func parseSkillFrontmatter(data []byte) (string, string, bool) {
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
		slog.Debug("acp skill scan: invalid YAML frontmatter", "error", err)
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

// SkillsToCommands converts SkillInfo slices to AvailableCommandInfo so skills
// appear in the slash command menu (/) just like plugin commands.
//
// Command names are delivered as the RAW frontmatter name — no "/" prefix is
// added here. The frontend prepends "/" when rendering the slash menu, exactly
// as it does for plugin-command names. Adding "/" in both places would produce
// "//name" in the menu and break IsACPSlashCommand on send.
func SkillsToCommands(skills []SkillInfo) []AvailableCommandInfo {
	if len(skills) == 0 {
		return nil
	}
	cmds := make([]AvailableCommandInfo, 0, len(skills))
	for _, s := range skills {
		cmds = append(cmds, AvailableCommandInfo{
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return cmds
}

// buildSkillsSystemPrompt generates a system prompt section that informs
// CodeBuddy about available skills. This is injected into the session
// system prompt so CodeBuddy can auto-load skills based on triggers.
func buildSkillsSystemPrompt(skills []SkillInfo) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	b.WriteString("The following skills are available in your environment. ")
	b.WriteString("When a skill's description matches the current task, ")
	b.WriteString("load and follow its instructions automatically.\n\n")
	b.WriteString("| Skill | Description |\n")
	b.WriteString("|-------|-------------|\n")
	for _, s := range skills {
		b.WriteString("| ")
		b.WriteString(s.Name)
		b.WriteString(" | ")
		b.WriteString(escapeTableCell(s.Description))
		b.WriteString(" |\n")
	}

	return b.String()
}

// escapeTableCell escapes a value for use inside a markdown table cell:
// pipes and backslashes are backslash-escaped, and newlines are replaced
// with spaces so a description cannot break the table structure.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}
