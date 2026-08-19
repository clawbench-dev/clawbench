package ai

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCodeBuddyBackend(t *testing.T) {
	tests := []struct {
		name   string
		agent  *model.Agent
		expect bool
	}{
		{"codebuddy agent", &model.Agent{Backend: "codebuddy"}, true},
		{"claude agent", &model.Agent{Backend: "claude"}, false},
		{"nil agent", nil, false},
		{"empty backend", &model.Agent{Backend: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isCodeBuddyBackend(tt.agent))
		})
	}
}

func TestParseCommandFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDesc string
		wantOK   bool
	}{
		{
			name: "valid frontmatter with description",
			input: `---
description: "Execute plan in batches with review checkpoints"
disable-model-invocation: true
---
Body text here`,
			wantDesc: "Execute plan in batches with review checkpoints",
			wantOK:   true,
		},
		{
			name: "valid frontmatter without quotes",
			input: `---
description: Create detailed implementation plan
---
Body`,
			wantDesc: "Create detailed implementation plan",
			wantOK:   true,
		},
		{
			name: "valid frontmatter with single quotes",
			input: `---
description: 'Some description'
---
Body`,
			wantDesc: "Some description",
			wantOK:   true,
		},
		{
			name:   "no frontmatter",
			input:  "Just a regular file without frontmatter",
			wantOK: false,
		},
		{
			name: "frontmatter without description",
			input: `---
disable-model-invocation: true
---
Body`,
			wantOK: false,
		},
		{
			name: "empty description",
			input: `---
description: ""
---
Body`,
			wantOK: false,
		},
		{
			name:   "missing closing delimiter",
			input:  "---\ndescription: test",
			wantOK: false,
		},
		{
			name:   "--- not at start of file",
			input:  "Some text before\n---\ndescription: test\n---\nBody",
			wantOK: false,
		},
		{
			name:   "--- in body not treated as frontmatter",
			input:  "This file mentions --- as a separator\nand has no frontmatter",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, ok := parseCommandFrontmatter([]byte(tt.input))
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantDesc, desc)
			}
		})
	}
}

func TestScanCodeBuddyPluginCommands_ValidDir(t *testing.T) {
	// Create a temporary directory mimicking the plugin cache structure
	tmpDir := t.TempDir()

	// superpowers/4.0.3/commands/brainstorm.md
	cmdDir := filepath.Join(tmpDir, "codebuddy-plugins-official", "superpowers", "4.0.3", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "brainstorm.md"), []byte(`---
description: "You MUST use this before any creative work"
disable-model-invocation: true
---
Invoke the superpowers:brainstorming skill`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "execute-plan.md"), []byte(`---
description: "Execute plan in batches with review checkpoints"
---
Invoke the superpowers:executing-plans skill`), 0o644))

	// code-review-ai/1.2.0/commands/ai-review.md
	cmdDir2 := filepath.Join(tmpDir, "codebuddy-plugins-official", "code-review-ai", "1.2.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir2, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cmdDir2, "ai-review.md"), []byte(`---
description: "AI-powered code review specialist"
---
Review code`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	require.Len(t, cmds, 3)

	// Results are sorted by name
	assert.Equal(t, "ai-review", cmds[0].Name)
	assert.Equal(t, "AI-powered code review specialist", cmds[0].Description)
	assert.Equal(t, "brainstorm", cmds[1].Name)
	assert.Equal(t, "You MUST use this before any creative work", cmds[1].Description)
	assert.Equal(t, "execute-plan", cmds[2].Name)
	assert.Equal(t, "Execute plan in batches with review checkpoints", cmds[2].Description)
}

func TestScanCodeBuddyPluginCommands_MissingDir(t *testing.T) {
	cmds := scanPluginCommandsFromDir("/nonexistent/path")
	assert.Nil(t, cmds)
}

func TestScanCodeBuddyPluginCommands_NoCommandsDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a plugin directory without commands/
	pluginDir := filepath.Join(tmpDir, "some-plugin", "1.0.0", "skills")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "SKILL.md"), []byte(`---
name: test
description: test skill
---
Body`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	assert.Nil(t, cmds)
}

func TestScanCodeBuddyPluginCommands_InvalidFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, "test-plugin", "1.0.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))

	// File without frontmatter — should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "broken.md"), []byte("No frontmatter here"), 0o644))

	// File with empty description — should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "empty-desc.md"), []byte(`---
description: ""
---
Body`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	assert.Nil(t, cmds)
}

func TestScanCodeBuddyPluginCommands_SkipsNonCommandsDirs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .md files outside commands/ dirs — should be skipped
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "plugin", "1.0.0", "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "plugin", "1.0.0", "skills", "test.md"), []byte(`---
description: "Should be ignored"
---
Body`), 0o644))

	// Create valid commands/ with a file
	cmdDir := filepath.Join(tmpDir, "plugin", "1.0.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "valid.md"), []byte(`---
description: "Valid command"
---
Body`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	require.Len(t, cmds, 1)
	assert.Equal(t, "valid", cmds[0].Name)
}

func TestScanCodeBuddyPluginCommands_SkipsDotMdFilename(t *testing.T) {
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, "test-plugin", "1.0.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))

	// File named ".md" — cmdName becomes empty after TrimSuffix, should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, ".md"), []byte(`---
description: "Should be skipped"
---
Body`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	assert.Nil(t, cmds)
}

func TestScanCodeBuddyPluginCommands_SkipsUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root user can read all files, permission-based test unreliable")
	}
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, "test-plugin", "1.0.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))

	// Create a file with no read permission
	unreadable := filepath.Join(cmdDir, "unreadable.md")
	require.NoError(t, os.WriteFile(unreadable, []byte(`---
description: "Cannot read me"
---
Body`), 0o644))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	defer os.Chmod(unreadable, 0o644) // restore for cleanup

	cmds := scanPluginCommandsFromDir(tmpDir)
	assert.Nil(t, cmds) // unreadable file skipped, no valid commands
}

func TestScanCodeBuddyPluginCommands_SkipsNonMdFilesInCommands(t *testing.T) {
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, "test-plugin", "1.0.0", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))

	// Non-.md file in commands/ — should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "helper.py"), []byte("print('hello')"), 0o644))

	// Valid .md file
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "valid.md"), []byte(`---
description: "Valid command"
---
Body`), 0o644))

	cmds := scanPluginCommandsFromDir(tmpDir)
	require.Len(t, cmds, 1)
	assert.Equal(t, "valid", cmds[0].Name)
}

func TestScanCodeBuddyPluginCommands_RealHomeDir(t *testing.T) {
	// Test ScanCodeBuddyPluginCommands by overriding the home directory.
	// On Windows, os.UserHomeDir reads USERPROFILE; on Unix, it reads HOME.
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".codebuddy", "plugins", "cache")
	cmdDir := filepath.Join(cacheDir, "superpowers", "4.0.3", "commands")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "brainstorm.md"), []byte(`---
description: "Brainstorm ideas"
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

	cmds := ScanCodeBuddyPluginCommands()
	require.Len(t, cmds, 1)
	assert.Equal(t, "brainstorm", cmds[0].Name)
}

func TestScanCodeBuddyPluginCommands_RealHomeDir_NoCacheDir(t *testing.T) {
	// Test ScanCodeBuddyPluginCommands when cache dir doesn't exist.
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

	cmds := ScanCodeBuddyPluginCommands()
	assert.Nil(t, cmds)
}

func TestScanCodeBuddyPluginCommands_CannotResolveHomeDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir on Windows uses USERPROFILE which always has a value")
	}
	// Test the os.UserHomeDir error path by unsetting HOME.
	// On Linux, os.UserHomeDir falls back to $HOME; clearing it returns an error.
	origHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", origHome)

	cmds := ScanCodeBuddyPluginCommands()
	assert.Nil(t, cmds)
}
