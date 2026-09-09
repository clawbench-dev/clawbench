package ai

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSkillFrontmatter(t *testing.T) {
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
			name, desc, ok := parseSkillFrontmatter([]byte(tt.input))
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantName, name)
				assert.Equal(t, tt.wantDesc, desc)
			}
		})
	}
}

func TestScanSkillsFromDir_ValidDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create skill-creator/SKILL.md
	skillDir := filepath.Join(tmpDir, "skill-creator")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: skill-creator
description: Guide for creating effective skills
---

# Skill Creator

Guide for creating skills`), 0o644))

	// Create another-skill/SKILL.md
	skillDir2 := filepath.Join(tmpDir, "another-skill")
	require.NoError(t, os.MkdirAll(skillDir2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte(`---
name: another-skill
description: Another skill description
---

# Another Skill`), 0o644))

	skills := scanSkillsFromDir(tmpDir)
	require.Len(t, skills, 2)

	// Results are sorted by name
	assert.Equal(t, "another-skill", skills[0].Name)
	assert.Equal(t, "Another skill description", skills[0].Description)
	assert.Contains(t, skills[0].Path, filepath.Join("another-skill", "SKILL.md"))
	assert.Equal(t, "skill-creator", skills[1].Name)
	assert.Equal(t, "Guide for creating effective skills", skills[1].Description)
	assert.Contains(t, skills[1].Path, filepath.Join("skill-creator", "SKILL.md"))
}

func TestScanSkillsFromDir_MissingDir(t *testing.T) {
	skills := scanSkillsFromDir("/nonexistent/path")
	assert.Nil(t, skills)
}

func TestScanSkillsFromDir_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	skills := scanSkillsFromDir(tmpDir)
	assert.Nil(t, skills)
}

func TestScanSkillsFromDir_SkipsNonSkillMd(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory with non-SKILL.md file
	skillDir := filepath.Join(tmpDir, "some-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "README.md"), []byte(`---
name: readme
description: Should be skipped
---
Body`), 0o644))

	// Create a valid SKILL.md
	skillDir2 := filepath.Join(tmpDir, "valid-skill")
	require.NoError(t, os.MkdirAll(skillDir2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte(`---
name: valid-skill
description: Valid skill
---
Body`), 0o644))

	skills := scanSkillsFromDir(tmpDir)
	require.Len(t, skills, 1)
	assert.Equal(t, "valid-skill", skills[0].Name)
}

func TestScanSkillsFromDir_InvalidFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()

	// File without frontmatter
	skillDir := filepath.Join(tmpDir, "broken")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("No frontmatter here"), 0o644))

	// File with empty name
	skillDir2 := filepath.Join(tmpDir, "empty-name")
	require.NoError(t, os.MkdirAll(skillDir2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte(`---
name: ""
description: test
---
Body`), 0o644))

	skills := scanSkillsFromDir(tmpDir)
	assert.Nil(t, skills)
}

func TestScanSkillsFromDir_SkipsUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root user can read all files, permission-based test unreliable")
	}

	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "secret-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	unreadable := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(unreadable, []byte(`---
name: secret
description: Cannot read me
---
Body`), 0o644))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	defer os.Chmod(unreadable, 0o644)

	skills := scanSkillsFromDir(tmpDir)
	assert.Nil(t, skills)
}

func TestScanCodeBuddySkills_RealHomeDir(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".codebuddy", "skills")
	skillDir := filepath.Join(skillsDir, "skill-creator")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: skill-creator
description: Guide for creating effective skills
---
Body`), 0o644))

	if runtime.GOOS == "windows" {
		origProfile := os.Getenv("USERPROFILE")
		require.NoError(t, os.Setenv("USERPROFILE", tmpDir))
		defer os.Setenv("USERPROFILE", origProfile)
	} else {
		origHome := os.Getenv("HOME")
		require.NoError(t, os.Setenv("HOME", tmpDir))
		defer os.Setenv("HOME", origHome)
	}

	skills := ScanCodeBuddySkills()
	require.Len(t, skills, 1)
	assert.Equal(t, "skill-creator", skills[0].Name)
	assert.Equal(t, "Guide for creating effective skills", skills[0].Description)
}

func TestScanCodeBuddySkills_RealHomeDir_NoSkillsDir(t *testing.T) {
	tmpDir := t.TempDir()
	if runtime.GOOS == "windows" {
		origProfile := os.Getenv("USERPROFILE")
		require.NoError(t, os.Setenv("USERPROFILE", tmpDir))
		defer os.Setenv("USERPROFILE", origProfile)
	} else {
		origHome := os.Getenv("HOME")
		require.NoError(t, os.Setenv("HOME", tmpDir))
		defer os.Setenv("HOME", origHome)
	}

	skills := ScanCodeBuddySkills()
	assert.Nil(t, skills)
}

func TestBuildSkillsSystemPrompt(t *testing.T) {
	tests := []struct {
		name    string
		skills  []SkillInfo
		want    string
		wantLen int
	}{
		{
			name:    "nil skills",
			skills:  nil,
			want:    "",
			wantLen: 0,
		},
		{
			name:    "empty skills",
			skills:  []SkillInfo{},
			want:    "",
			wantLen: 0,
		},
		{
			name: "single skill",
			skills: []SkillInfo{
				{Name: "skill-creator", Description: "Guide for creating skills"},
			},
			wantLen: 1,
		},
		{
			name: "multiple skills",
			skills: []SkillInfo{
				{Name: "another", Description: "Another skill"},
				{Name: "skill-creator", Description: "Guide for creating skills"},
			},
			wantLen: 2,
		},
		{
			name: "description with pipe is escaped",
			skills: []SkillInfo{
				{Name: "docx", Description: "Create | edit | read Word documents"},
			},
			wantLen: 1,
		},
		{
			name: "description with newline is flattened",
			skills: []SkillInfo{
				{Name: "multi", Description: "line one\nline two"},
			},
			wantLen: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSkillsSystemPrompt(tt.skills)
			if tt.wantLen == 0 {
				assert.Empty(t, got)
			} else {
				assert.NotEmpty(t, got)
				assert.Contains(t, got, "Skills")
				for _, s := range tt.skills {
					assert.Contains(t, got, s.Name)
					assert.Contains(t, got, escapeTableCell(s.Description))
				}
			}
		})
	}
}

func TestEscapeTableCell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pipe escaped",
			in:   "Create | edit",
			want: `Create \| edit`,
		},
		{
			name: "backslash escaped",
			in:   `a\b`,
			want: `a\\b`,
		},
		{
			name: "newline flattened",
			in:   "line one\nline two",
			want: "line one line two",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "plain text untouched",
			in:   "just some text",
			want: "just some text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeTableCell(tt.in))
		})
	}
}

func TestSkillsToCommands(t *testing.T) {
	tests := []struct {
		name      string
		skills    []SkillInfo
		wantNil   bool
		wantNames []string
		wantDescs []string
		wantLen   int
	}{
		{
			name:    "nil skills",
			skills:  nil,
			wantNil: true,
		},
		{
			name:    "empty skills",
			skills:  []SkillInfo{},
			wantNil: true,
		},
		{
			name: "single skill without slash",
			skills: []SkillInfo{
				{Name: "skill-creator", Description: "Guide for creating skills"},
			},
			// Bare frontmatter name must be delivered as-is — the frontend adds
			// the "/" prefix when rendering the slash menu. A backend-added "/"
			// would double up to "//" in the menu and break IsACPSlashCommand.
			wantNames: []string{"skill-creator"},
			wantDescs: []string{"Guide for creating skills"},
			wantLen:   1,
		},
		{
			name: "single skill with slash prefix",
			skills: []SkillInfo{
				{Name: "/skill-creator", Description: "Guide for creating skills"},
			},
			// A name that already carries a slash is preserved verbatim (no
			// stripping, no re-adding). The frontend is responsible for
			// rendering a single leading slash either way.
			wantNames: []string{"/skill-creator"},
			wantDescs: []string{"Guide for creating skills"},
			wantLen:   1,
		},
		{
			name: "multiple skills",
			skills: []SkillInfo{
				{Name: "skill-creator", Description: "Guide for creating skills"},
				{Name: "docx", Description: "Word doc manipulation"},
			},
			wantNames: []string{"skill-creator", "docx"},
			wantDescs: []string{"Guide for creating skills", "Word doc manipulation"},
			wantLen:   2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SkillsToCommands(tt.skills)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.Len(t, got, tt.wantLen)
			for i, wantName := range tt.wantNames {
				assert.Equal(t, wantName, got[i].Name, "command name mismatch at index %d", i)
				assert.Equal(t, tt.wantDescs[i], got[i].Description, "command description mismatch at index %d", i)
			}
		})
	}
}
