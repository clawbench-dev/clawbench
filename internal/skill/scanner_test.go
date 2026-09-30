package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantDesc string
		wantOK   bool
	}{
		{
			name: "valid frontmatter with name and description",
			input: `---
name: skill-creator
description: Guide for creating effective skills
---
Body text here`,
			wantName: "skill-creator",
			wantDesc: "Guide for creating effective skills",
			wantOK:   true,
		},
		{
			name: "valid frontmatter with quotes",
			input: `---
name: "test-skill"
description: "Test description"
---
Body`,
			wantName: "test-skill",
			wantDesc: "Test description",
			wantOK:   true,
		},
		{
			name: "valid frontmatter with single quotes",
			input: `---
name: 'my-skill'
description: 'My description'
---
Body`,
			wantName: "my-skill",
			wantDesc: "My description",
			wantOK:   true,
		},
		{
			name:   "no frontmatter",
			input:  "Just a regular file without frontmatter",
			wantOK: false,
		},
		{
			name: "frontmatter without name",
			input: `---
description: Missing name
---
Body`,
			wantOK: false,
		},
		{
			name: "frontmatter without description",
			input: `---
name: missing-desc
---
Body`,
			wantOK: false,
		},
		{
			name: "empty name",
			input: `---
name: ""
description: test
---
Body`,
			wantOK: false,
		},
		{
			name: "empty description",
			input: `---
name: test
description: ""
---
Body`,
			wantOK: false,
		},
		{
			name:   "missing closing delimiter",
			input:  "---\nname: test\ndescription: test",
			wantOK: false,
		},
		{
			name:   "--- not at start of file",
			input:  "Some text before\n---\nname: test\ndescription: test\n---\nBody",
			wantOK: false,
		},
		{
			name: "folded multiline description",
			input: `---
name: minimax-docx
description: >
  Professional DOCX document creation, editing, and formatting using OpenXML SDK (.NET).
  Three pipelines: (A) create new documents from scratch, (B) fill/edit content in existing
---
Body`,
			wantName: "minimax-docx",
			wantDesc: "Professional DOCX document creation, editing, and formatting using OpenXML SDK (.NET). Three pipelines: (A) create new documents from scratch, (B) fill/edit content in existing",
			wantOK:   true,
		},
		{
			name: "literal multiline description",
			input: `---
name: flutter-dev
description: |
  Flutter cross-platform development guide covering widget patterns,
  Riverpod/Bloc state management, GoRouter navigation.
---
Body`,
			wantName: "flutter-dev",
			wantDesc: "Flutter cross-platform development guide covering widget patterns, Riverpod/Bloc state management, GoRouter navigation.",
			wantOK:   true,
		},
		{
			name: "description with pipe character inside",
			input: `---
name: docx
description: "Create | edit | read Word documents"
---
Body`,
			wantName: "docx",
			wantDesc: "Create | edit | read Word documents",
			wantOK:   true,
		},
		{
			name: "invalid yaml frontmatter",
			input: `---
name: broken
description: [
---
Body`,
			wantOK: false,
		},
		{
			name: "description is a non-string scalar",
			input: `---
name: numeric
description: 42
---
Body`,
			wantOK: false,
		},
		{
			name: "name and description out of order with other keys",
			input: `---
license: MIT
metadata:
  version: "1.0.0"
name: ordered-skill
description: Appears after metadata block
---
Body`,
			wantName: "ordered-skill",
			wantDesc: "Appears after metadata block",
			wantOK:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, desc, ok := ParseFrontmatter([]byte(tt.input))
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantName, name)
				assert.Equal(t, tt.wantDesc, desc)
			}
		})
	}
}

// writeSkill creates <dir>/<folder>/SKILL.md with the given frontmatter body.
func writeSkill(t *testing.T, dir, folder, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, folder)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(content), 0o644))
}

func TestScanDir_ValidDir(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "skill-creator", `---
name: skill-creator
description: Guide for creating effective skills
---
Body`)
	writeSkill(t, tmpDir, "another-skill", `---
name: another-skill
description: Another skill description
---
Body`)

	src := Source{Kind: SourceUserDir, Label: "user dir", Dir: tmpDir}
	skills := ScanDir(tmpDir, src)
	require.Len(t, skills, 2)

	// Sorted by name.
	assert.Equal(t, "another-skill", skills[0].Name)
	assert.Equal(t, "Another skill description", skills[0].Description)
	assert.Contains(t, skills[0].Path, filepath.Join("another-skill", "SKILL.md"))
	assert.Equal(t, src, skills[0].Source)
	assert.Equal(t, "skill-creator", skills[1].Name)
	assert.Contains(t, skills[1].Path, filepath.Join("skill-creator", "SKILL.md"))
}

func TestScanDir_MissingDir(t *testing.T) {
	assert.Nil(t, ScanDir("/nonexistent/path", Source{}))
	assert.Nil(t, ScanDir("", Source{}))
}

func TestScanDir_EmptyDir(t *testing.T) {
	assert.Nil(t, ScanDir(t.TempDir(), Source{}))
}

func TestScanDir_SkipsNonSkillMd(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "some-skill", "placeholder")
	// Replace SKILL.md with README.md so only the valid one is counted.
	require.NoError(t, os.Remove(filepath.Join(tmpDir, "some-skill", skillFileName)))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "some-skill", "README.md"), []byte("---\nname: readme\ndescription: skip\n---\n"), 0o644))

	writeSkill(t, tmpDir, "valid-skill", `---
name: valid-skill
description: Valid skill
---
Body`)

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 1)
	assert.Equal(t, "valid-skill", skills[0].Name)
}

func TestScanDir_InvalidFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "broken", "No frontmatter here")
	writeSkill(t, tmpDir, "empty-name", "---\nname: \"\"\ndescription: test\n---\nBody")
	assert.Nil(t, ScanDir(tmpDir, Source{}))
}

func TestScanDir_SkipsUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root user can read all files, permission-based test unreliable")
	}

	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "secret-skill", `---
name: secret
description: Cannot read me
---
Body`)
	unreadable := filepath.Join(tmpDir, "secret-skill", skillFileName)
	require.NoError(t, os.Chmod(unreadable, 0o000))
	defer os.Chmod(unreadable, 0o644)

	assert.Nil(t, ScanDir(tmpDir, Source{}))
}

// TestScanDir_RootIsAlsoASkill pins the single-skill-repository layout: a repo
// whose root is itself a skill (`npx skills add owner/repo`) is a supported
// shape, so the scan root is tested as a skill too.
func TestScanDir_RootIsAlsoASkill(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, skillFileName),
		[]byte("---\nname: root-skill\ndescription: the repo root is the skill\n---\n"), 0o644))

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 1)
	assert.Equal(t, "root-skill", skills[0].Name)
}

// TestScanDir_RootSkillDoesNotHideNestedSkills pins that treating the root as a
// skill does not stop the walk: the root doubles as the container.
func TestScanDir_RootSkillDoesNotHideNestedSkills(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, skillFileName),
		[]byte("---\nname: root-skill\ndescription: root\n---\n"), 0o644))
	writeSkill(t, tmpDir, "nested", "---\nname: nested-skill\ndescription: nested\n---\n")

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 2)
	assert.Equal(t, "nested-skill", skills[0].Name)
	assert.Equal(t, "root-skill", skills[1].Name)
}

// TestScanDir_RecursesIntoNestedLayouts pins the real ecosystem layouts: skill
// repositories group skills by category, so the SKILL.md is several levels
// down. A depth-1 scan found none of these.
func TestScanDir_RecursesIntoNestedLayouts(t *testing.T) {
	tmpDir := t.TempDir()
	// mattpocock/skills style.
	writeSkill(t, tmpDir, filepath.Join("skills", "productivity", "grill-me"), "---\nname: grill-me\ndescription: d\n---\n")
	writeSkill(t, tmpDir, filepath.Join("skills", "engineering", "grill-with-docs"), "---\nname: grill-with-docs\ndescription: d\n---\n")
	// vercel-labs/skills style.
	writeSkill(t, tmpDir, filepath.Join("skills", "find-skills"), "---\nname: find-skills\ndescription: d\n---\n")
	// A plain top-level skill still works.
	writeSkill(t, tmpDir, "top-level", "---\nname: top-level\ndescription: d\n---\n")

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 4)
	assert.Equal(t, []string{"find-skills", "grill-me", "grill-with-docs", "top-level"},
		[]string{skills[0].Name, skills[1].Name, skills[2].Name, skills[3].Name})
}

// TestScanDir_SkipsVCSAndDependencyDirs pins that the walk does not descend into
// directories that can never hold skills but can be huge.
func TestScanDir_SkipsVCSAndDependencyDirs(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, filepath.Join(".git", "modules", "ghost"), "---\nname: ghost\ndescription: d\n---\n")
	writeSkill(t, tmpDir, filepath.Join("node_modules", "pkg"), "---\nname: dep-skill\ndescription: d\n---\n")
	writeSkill(t, tmpDir, filepath.Join("dist", "built"), "---\nname: built-skill\ndescription: d\n---\n")
	// A dot-directory is skipped wholesale too.
	writeSkill(t, tmpDir, filepath.Join(".hidden", "hidden-skill"), "---\nname: hidden-skill\ndescription: d\n---\n")
	// The only real skill.
	writeSkill(t, tmpDir, "real", "---\nname: real\ndescription: d\n---\n")

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 1)
	assert.Equal(t, "real", skills[0].Name)
}

// TestScanDir_DoesNotNestInsideASkill pins that a skill's own subdirectories are
// not searched: they hold the skill's scripts/references, not nested skills.
func TestScanDir_DoesNotNestInsideASkill(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "outer", "---\nname: outer\ndescription: d\n---\n")
	// A SKILL.md inside the skill's own references dir must not become a skill.
	writeSkill(t, tmpDir, filepath.Join("outer", "references", "inner"), "---\nname: inner\ndescription: d\n---\n")

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 1)
	assert.Equal(t, "outer", skills[0].Name)
}

// TestScanDir_RespectsDepthLimit pins the bound on the walk: a skill nested
// beyond maxScanDepth is not found, so a hostile or accidental deep tree cannot
// make the scan unbounded.
func TestScanDir_RespectsDepthLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Exactly at the limit: depth maxScanDepth-1 below the root is still walked
	// (the root's direct child is depth 1).
	atLimit := strings.Repeat("d"+string(filepath.Separator), maxScanDepth-2) + "at-limit"
	writeSkill(t, tmpDir, atLimit, "---\nname: at-limit\ndescription: d\n---\n")

	// One level deeper than the walk allows.
	beyond := strings.Repeat("d"+string(filepath.Separator), maxScanDepth) + "beyond-limit"
	writeSkill(t, tmpDir, beyond, "---\nname: beyond-limit\ndescription: d\n---\n")

	names := map[string]bool{}
	for _, s := range ScanDir(tmpDir, Source{}) {
		names[s.Name] = true
	}
	assert.True(t, names["at-limit"], "a skill within the depth limit must be found")
	assert.False(t, names["beyond-limit"], "a skill beyond the depth limit must not be found")
}

func TestBuildSystemPromptSection(t *testing.T) {
	t.Run("nil and empty produce nothing", func(t *testing.T) {
		assert.Empty(t, BuildSystemPromptSection(nil))
		assert.Empty(t, BuildSystemPromptSection([]Skill{}))
	})

	t.Run("renders name, description and path", func(t *testing.T) {
		got := BuildSystemPromptSection([]Skill{
			{Name: "mmx-cli", Description: "MiniMax CLI", Path: "/home/u/.qoder/skills/mmx-cli/SKILL.md"},
		})
		assert.Contains(t, got, "Skills")
		assert.Contains(t, got, "mmx-cli")
		assert.Contains(t, got, "MiniMax CLI")
		// The path is what lets an agent read a skill it does not own.
		assert.Contains(t, got, "/home/u/.qoder/skills/mmx-cli/SKILL.md")
		assert.Contains(t, got, "| Path |")
	})

	t.Run("escapes table-breaking characters", func(t *testing.T) {
		got := BuildSystemPromptSection([]Skill{
			{Name: "docx", Description: "Create | edit | read", Path: "/p/a\\b/SKILL.md"},
		})
		assert.Contains(t, got, `Create \| edit \| read`)
		assert.Contains(t, got, `/p/a\\b/SKILL.md`)
	})
}

func TestEscapeTableCell(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"pipe escaped", "Create | edit", `Create \| edit`},
		{"backslash escaped", `a\b`, `a\\b`},
		{"newline flattened", "line one\nline two", "line one line two"},
		{"empty string", "", ""},
		{"plain text untouched", "just some text", "just some text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeTableCell(tt.in))
		})
	}
}

func TestResolveNativeSkillsDir(t *testing.T) {
	t.Run("empty stays empty", func(t *testing.T) {
		assert.Equal(t, "", ResolveNativeSkillsDir(""))
	})

	t.Run("absolute is returned as-is", func(t *testing.T) {
		// Build the absolute path with the host separator: a POSIX literal
		// like "/opt/skills" is NOT absolute on Windows, so filepath.IsAbs
		// would be false and the value would be joined onto home instead.
		abs := filepath.Join(t.TempDir(), "skills")
		require.True(t, filepath.IsAbs(abs), "fixture path must be absolute on this host")
		assert.Equal(t, abs, ResolveNativeSkillsDir(abs))
	})

	t.Run("relative is joined onto home", func(t *testing.T) {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, ".codebuddy", "skills"), ResolveNativeSkillsDir(".codebuddy/skills"))
	})
}

func TestCanonicalName(t *testing.T) {
	assert.Equal(t, "mmx-cli", canonicalName("mmx-cli"))
	assert.Equal(t, "mmx-cli", canonicalName("/mmx-cli"))
	assert.Equal(t, "mmx-cli", canonicalName("MMX-CLI"))
	assert.Equal(t, "mmx-cli", canonicalName(" /MMX-Cli "))
}

// TestScanDir_NameMustMatchDirectory pins the spec rule that a skill's
// frontmatter name equals its directory name, and that a violation is flagged
// (not silently accepted) while the skill is still returned — it stays
// injectable via its explicit path, but must not become a slash command.
func TestScanDir_NameMustMatchDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Directory "good-name", frontmatter "good-name" → fine.
	writeSkill(t, tmpDir, "good-name", "---\nname: good-name\ndescription: ok\n---\n")
	// Directory "dir-name", frontmatter "other-name" → mismatch.
	writeSkill(t, tmpDir, "dir-name", "---\nname: other-name\ndescription: mismatch\n---\n")

	byName := map[string]Skill{}
	for _, s := range ScanDir(tmpDir, Source{}) {
		byName[s.Name] = s
	}
	require.Contains(t, byName, "good-name")
	assert.False(t, byName["good-name"].NameMismatch)
	require.Contains(t, byName, "other-name")
	assert.True(t, byName["other-name"].NameMismatch,
		"a name that disagrees with its directory must be flagged")
}

// TestScanDir_NameMatchIsCaseInsensitiveAndNFKC pins the comparison rules:
// case-insensitive, NFKC-normalized, and tolerant of a leading slash.
func TestScanDir_NameMatchIsCaseInsensitiveAndNFKC(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkill(t, tmpDir, "MixedCase", "---\nname: mixedcase\ndescription: d\n---\n")
	writeSkill(t, tmpDir, "slashed", "---\nname: /slashed\ndescription: d\n---\n")

	for _, s := range ScanDir(tmpDir, Source{}) {
		assert.False(t, s.NameMismatch, "name %q should match its directory", s.Name)
	}
}

// TestScanDir_RootSkillIsNotNameChecked pins that the single-skill-repository
// layout is exempt: the scan root's directory name is a temp dir or a slug, so
// requiring a match would reject every such repository.
func TestScanDir_RootSkillIsNotNameChecked(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, skillFileName),
		[]byte("---\nname: any-name\ndescription: d\n---\n"), 0o644))

	skills := ScanDir(tmpDir, Source{})
	require.Len(t, skills, 1)
	assert.False(t, skills[0].NameMismatch, "the scan root must be exempt from the name check")
}
