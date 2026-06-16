package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveCLIPath_NonExistentCommand(t *testing.T) {
	result := ResolveCLIPath("definitely_not_a_real_command_12345")
	if result != "" {
		t.Errorf("expected empty string for non-existent command, got %q", result)
	}
}

func TestResolveCLIPath_ExistingCommand(t *testing.T) {
	// "go" should be on PATH in any test environment
	result := ResolveCLIPath("go")
	if result == "" {
		t.Error("expected non-empty result for 'go' command")
	}
}

func TestResolveCmdWrapper_NpmStyleCmd(t *testing.T) {
	// Create a temporary .cmd file that mimics npm's wrapper format
	tmpDir := t.TempDir()

	// Create a fake node_modules structure
	pkgDir := filepath.Join(tmpDir, "node_modules", "@scope", "my-pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create the JS entry point file
	entryFile := filepath.Join(pkgDir, "mycli")
	if err := os.WriteFile(entryFile, []byte("#!/usr/bin/env node\nconsole.log('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the .cmd wrapper
	cmdContent := `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b %ERRORLEVEL%
:start
SETLOCAL
CALL :find_dp0
IF EXIST "%dp0%\node.exe" (
  "%dp0%\node.exe"  "%dp0%\node_modules\@scope\my-pkg\bin\mycli" %*
) ELSE (
  SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%dp0%\node_modules\@scope\my-pkg\bin\mycli" %*
)
`
	cmdFile := filepath.Join(tmpDir, "mycli.cmd")
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Test resolveCmdWrapper directly
	result := resolveCmdWrapper(cmdFile)
	if result == "" {
		t.Fatal("expected non-empty result from resolveCmdWrapper")
	}

	expected := filepath.Join(tmpDir, "node_modules", "@scope", "my-pkg", "bin", "mycli")
	if result != expected {
		t.Errorf("resolveCmdWrapper returned %q, want %q", result, expected)
	}
}

func TestResolveCmdWrapper_CodebuddyStyleCmd(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake node_modules structure for codebuddy
	pkgDir := filepath.Join(tmpDir, "node_modules", "@tencent-ai", "codebuddy-code", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entryFile := filepath.Join(pkgDir, "codebuddy")
	if err := os.WriteFile(entryFile, []byte("#!/usr/bin/env node\nconsole.log('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmdContent := `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b %ERRORLEVEL%
:start
SETLOCAL
CALL :find_dp0
IF EXIST "%dp0%\node.exe" (
  "%dp0%\node.exe"  "%dp0%\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" %*
) ELSE (
  SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%dp0%\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" %*
)
`
	cmdFile := filepath.Join(tmpDir, "codebuddy.cmd")
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result := resolveCmdWrapper(cmdFile)
	if result == "" {
		t.Fatal("expected non-empty result from resolveCmdWrapper")
	}

	expected := filepath.Join(tmpDir, "node_modules", "@tencent-ai", "codebuddy-code", "bin", "codebuddy")
	if result != expected {
		t.Errorf("resolveCmdWrapper returned %q, want %q", result, expected)
	}
}

func TestResolveCmdWrapper_KimiStyleCmd(t *testing.T) {
	tmpDir := t.TempDir()

	// Gemini uses bundle/gemini as the entry point
	pkgDir := filepath.Join(tmpDir, "node_modules", "@google", "kimi-cli", "bundle")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entryFile := filepath.Join(pkgDir, "gemini")
	if err := os.WriteFile(entryFile, []byte("#!/usr/bin/env node\nconsole.log('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmdContent := `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b %ERRORLEVEL%
:start
SETLOCAL
CALL :find_dp0
IF EXIST "%dp0%\node.exe" (
  "%dp0%\node.exe"  "%dp0%\node_modules\@google\kimi-cli\bundle\gemini" %*
) ELSE (
  SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%dp0%\node_modules\@google\kimi-cli\bundle\gemini" %*
)
`
	cmdFile := filepath.Join(tmpDir, "gemini.cmd")
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result := resolveCmdWrapper(cmdFile)
	if result == "" {
		t.Fatal("expected non-empty result from resolveCmdWrapper")
	}

	expected := filepath.Join(tmpDir, "node_modules", "@google", "kimi-cli", "bundle", "gemini")
	if result != expected {
		t.Errorf("resolveCmdWrapper returned %q, want %q", result, expected)
	}
}

func TestResolveCmdWrapper_NoNodeModulesPath(t *testing.T) {
	tmpDir := t.TempDir()

	// .cmd file without node_modules reference
	cmdContent := `@ECHO off
echo hello
`
	cmdFile := filepath.Join(tmpDir, "test.cmd")
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result := resolveCmdWrapper(cmdFile)
	if result != "" {
		t.Errorf("expected empty result for .cmd without node_modules, got %q", result)
	}
}

func TestResolveCmdWrapper_ResolvedPathNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// .cmd file references a path that doesn't exist
	cmdContent := `@ECHO off
node  "%dp0%\node_modules\@scope\missing-pkg\bin\missing" %*
`
	cmdFile := filepath.Join(tmpDir, "missing.cmd")
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result := resolveCmdWrapper(cmdFile)
	if result != "" {
		t.Errorf("expected empty result when resolved path doesn't exist, got %q", result)
	}
}

func TestResolveCLIPath_NonCmdOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	// When a non-.cmd command is found, it should use EvalSymlinks
	result := ResolveCLIPath("go")
	if result == "" {
		t.Error("expected non-empty result for 'go' command")
	}
}

func TestResolveCmdWrapper_NonExistentFile(t *testing.T) {
	result := resolveCmdWrapper("/nonexistent/path/to/file.cmd")
	if result != "" {
		t.Errorf("expected empty string for non-existent file, got %q", result)
	}
}

func TestResolveCmdWrapper_UnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based test unreliable on Windows")
	}

	tmpDir := t.TempDir()
	cmdFile := filepath.Join(tmpDir, "unreadable.cmd")
	if err := os.WriteFile(cmdFile, []byte("some content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove read permissions
	if err := os.Chmod(cmdFile, 0o000); err != nil {
		t.Fatal(err)
	}
	// Re-enable permissions for cleanup (t.TempDir cleanup needs this)
	t.Cleanup(func() { os.Chmod(cmdFile, 0o644) })

	result := resolveCmdWrapper(cmdFile)
	if result != "" {
		t.Errorf("expected empty string for unreadable file, got %q", result)
	}
}

func TestCmdEntryRe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // the captured group, or empty if no match
	}{
		{
			name:     "codebuddy style",
			input:    `  "%dp0%\node.exe"  "%dp0%\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" %*`,
			expected: `@tencent-ai\codebuddy-code\bin\codebuddy`,
		},
		{
			name:     "gemini style",
			input:    `  node  "%dp0%\node_modules\@google\kimi-cli\bundle\gemini" %*`,
			expected: `@google\kimi-cli\bundle\gemini`,
		},
		{
			name:     "codex style",
			input:    `  "%dp0%\node.exe"  "%dp0%\node_modules\@openai\codex\bin\codex" %*`,
			expected: `@openai\codex\bin\codex`,
		},
		{
			name:     "vecli style",
			input:    `  node  "%dp0%\node_modules\@volcengine\vecli\bin\vecli" %*`,
			expected: `@volcengine\vecli\bin\vecli`,
		},
		{
			name:     "dp0 with tilde",
			input:    `  "%~dp0%\node_modules\@scope\pkg\bin\cmd" %*`,
			expected: `@scope\pkg\bin\cmd`,
		},
		{
			name:     "no node_modules",
			input:    `  echo hello`,
			expected: "",
		},
		{
			name:     "forward slashes",
			input:    `  node  "%dp0%/node_modules/@scope/pkg/bin/cmd" %*`,
			expected: `@scope/pkg/bin/cmd`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := cmdEntryRe.FindStringSubmatch(tt.input)
			got := ""
			if len(matches) >= 2 {
				got = matches[1]
			}
			if got != tt.expected {
				t.Errorf("cmdEntryRe match = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveCLIPath_SymlinkResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not reliable on Windows without admin")
	}

	// Create a symlink and verify ResolveCLIPath resolves it
	tmpDir := t.TempDir()

	// Create a real script
	realScript := filepath.Join(tmpDir, "real_script")
	if err := os.WriteFile(realScript, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink in a "bin" directory
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(binDir, "myscript")
	if err := os.Symlink(realScript, linkPath); err != nil {
		t.Fatal(err)
	}

	// Add binDir to PATH
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	result := ResolveCLIPath("myscript")
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Should resolve the symlink to the real path
	realResult, err := filepath.EvalSymlinks(realScript)
	if err != nil {
		t.Fatal(err)
	}
	if result != realResult {
		t.Errorf("ResolveCLIPath = %q, want %q", result, realResult)
	}
}

func TestResolveCLIPath_CmdFileOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific .cmd resolution test")
	}

	// This test only runs on Windows where .cmd files are actually found by LookPath.
	// The test verifies that when a .cmd file is found, it gets resolved properly.
	// Since we can't easily install an npm package in tests, we test the
	// resolveCmdWrapper function directly instead (see TestResolveCmdWrapper_*).
	t.Log("resolveCmdWrapper is tested directly via TestResolveCmdWrapper_*")
}
