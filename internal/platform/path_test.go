package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsWindows(t *testing.T) {
	result := IsWindows()
	expected := runtime.GOOS == "windows"
	if result != expected {
		t.Errorf("IsWindows() = %v, want %v", result, expected)
	}
}

func TestUserHomeDir(t *testing.T) {
	home := UserHomeDir()
	if home == "" {
		t.Error("UserHomeDir() returned empty string")
	}
}

func TestClaudeConfigDir(t *testing.T) {
	dir := ClaudeConfigDir()
	if dir == "" {
		t.Error("ClaudeConfigDir() returned empty string")
	}
	// Should end with .claude
	if len(dir) < 7 || dir[len(dir)-7:] != ".claude" {
		t.Errorf("ClaudeConfigDir() = %q, want path ending with .claude", dir)
	}
}

func TestManglePathForOS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		goos     string
		expected string
	}{
		// Unix paths
		{
			name:     "unix path on linux",
			input:    "/home/user/project",
			goos:     "linux",
			expected: "-home-user-project",
		},
		{
			name:     "root path on linux",
			input:    "/",
			goos:     "linux",
			expected: "-",
		},
		// Windows paths (always testable regardless of runtime OS)
		{
			name:     "windows path on windows",
			input:    `C:\Users\user\project`,
			goos:     "windows",
			expected: "C--Users-user-project",
		},
		{
			name:     "windows drive root",
			input:    `C:\`,
			goos:     "windows",
			expected: "C--",
		},
		{
			name:     "windows drive only",
			input:    `C:`,
			goos:     "windows",
			expected: "C-",
		},
		// Edge cases
		{
			name:     "mixed separators treated as windows",
			input:    `C:\Users/admin\project`,
			goos:     "windows",
			expected: "C--Users-admin-project",
		},
		{
			name:     "unix path on windows no drive",
			input:    "/home/user/project",
			goos:     "windows",
			expected: "-home-user-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ManglePathForOS(tt.input, tt.goos)
			if result != tt.expected {
				t.Errorf("ManglePathForOS(%q, %q) = %q, want %q", tt.input, tt.goos, result, tt.expected)
			}
		})
	}
}

func TestManglePath(t *testing.T) {
	// Test that ManglePath delegates correctly to ManglePathForOS with runtime.GOOS
	result := ManglePath("/home/user/project")
	expected := ManglePathForOS("/home/user/project", runtime.GOOS)
	if result != expected {
		t.Errorf("ManglePath(%q) = %q, want %q (same as ManglePathForOS with runtime.GOOS)", "/home/user/project", result, expected)
	}
}

func TestExpandTilde(t *testing.T) {
	home := UserHomeDir()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde with slash",
			input:    "~/Documents",
			expected: filepath.Join(home, "Documents"),
		},
		{
			name:     "bare tilde",
			input:    "~",
			expected: home,
		},
		{
			name:     "absolute path unchanged",
			input:    "/home/user/project",
			expected: "/home/user/project",
		},
		{
			name:     "relative path unchanged",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandTilde(tt.input)
			if result != tt.expected {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}

	// Windows backslash tilde
	if runtime.GOOS == "windows" {
		result := ExpandTilde(`~\Documents`)
		expected := filepath.Join(home, "Documents")
		if result != expected {
			t.Errorf("ExpandTilde(%q) = %q, want %q", `~\Documents`, result, expected)
		}
	}
}

func TestListRootPaths(t *testing.T) {
	// Reset cache
	cachedRootPaths = nil

	roots := ListRootPaths()
	if len(roots) == 0 {
		t.Error("ListRootPaths() returned empty slice")
	}

	if runtime.GOOS != "windows" {
		// On Unix, should return ["/"]
		if len(roots) != 1 || roots[0] != "/" {
			t.Errorf("ListRootPaths() on Unix = %v, want [\"/\"]", roots)
		}
	} else {
		// On Windows, should return at least one drive
		for _, r := range roots {
			if len(r) < 3 || r[1] != ':' || r[2] != '\\' {
				t.Errorf("ListRootPaths() on Windows returned invalid drive %q", r)
			}
		}
	}

	// Verify caching: second call returns same slice
	roots2 := ListRootPaths()
	if len(roots) != len(roots2) {
		t.Errorf("ListRootPaths() returned different lengths on second call: %d vs %d", len(roots), len(roots2))
	}
}

// TestIsPathUnderAnyRoot pins the SEMANTICS of root containment, not a security
// boundary. On Unix the app's root is "/" (ListRootPaths), so with that value
// the predicate is equivalent to "is this an absolute path" — which is correct
// for what it is used for (authenticated file-browser endpoints that are meant
// to reach the whole machine).
//
// It must therefore never be the sole guard on an UNauthenticated endpoint: a
// root of "/" would confine nothing. The public share endpoints capture their
// own confinement directory at share-creation time and compare against that
// instead (see handler.serveShareLocal / resolveShareRoot).
func TestIsPathUnderAnyRoot(t *testing.T) {
	tests := []struct {
		name     string
		absPath  string
		roots    []string
		expected bool
	}{
		{
			name:     "path under root",
			absPath:  "/home/user/project",
			roots:    []string{"/"},
			expected: true,
		},
		{
			name:     "path equals root",
			absPath:  "/",
			roots:    []string{"/"},
			expected: true,
		},
		{
			name:     "real existing path under root",
			absPath:  "/tmp",
			roots:    []string{"/"},
			expected: true,
		},
		{
			name:     "empty roots",
			absPath:  "/home/user",
			roots:    []string{},
			expected: false,
		},
		{
			name:     "path under second root (non-existent paths, lexical match)",
			absPath:  "/fake/root2/subdir",
			roots:    []string{"/fake/root1", "/fake/root2"},
			expected: true,
		},
		{
			name:     "path not under any of multiple roots (non-existent, lexical)",
			absPath:  "/opt/something",
			roots:    []string{"/fake/root1", "/fake/root2"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPathUnderAnyRoot(tt.absPath, tt.roots)
			if result != tt.expected {
				t.Errorf("IsPathUnderAnyRoot(%q, %v) = %v, want %v", tt.absPath, tt.roots, result, tt.expected)
			}
		})
	}
}

func TestIsPathUnderAnyRoot_MultipleRoots(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()
	if r, err := filepath.EvalSymlinks(tmpDir1); err == nil {
		tmpDir1 = r
	}
	if r, err := filepath.EvalSymlinks(tmpDir2); err == nil {
		tmpDir2 = r
	}

	roots := []string{tmpDir1, tmpDir2}

	t.Run("path under first root", func(t *testing.T) {
		result := IsPathUnderAnyRoot(filepath.Join(tmpDir1, "subdir"), roots)
		if !result {
			t.Error("expected path under first root to return true")
		}
	})

	t.Run("path under second root", func(t *testing.T) {
		result := IsPathUnderAnyRoot(filepath.Join(tmpDir2, "subdir"), roots)
		if !result {
			t.Error("expected path under second root to return true")
		}
	})

	t.Run("path outside all roots", func(t *testing.T) {
		result := IsPathUnderAnyRoot("/completely/outside/path", roots)
		if result {
			t.Error("expected path outside all roots to return false")
		}
	})
}

func TestIsPathUnderRoot_DeeplyNestedNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = resolved
	}

	// Non-existent deeply nested path under an existing root
	result := isPathUnderRoot(filepath.Join(tmpDir, "a", "b", "c", "d", "nonexistent"), tmpDir)
	if !result {
		t.Error("expected deeply nested non-existent path under root to return true")
	}
}

func TestIsPathUnderRoot(t *testing.T) {
	tmpDir := t.TempDir()
	// Resolve symlinks so tests work on macOS where /var → /private/var
	if resolved, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = resolved
	}

	t.Run("existing path under existing root", func(t *testing.T) {
		result := isPathUnderRoot(filepath.Join(tmpDir, "subdir"), tmpDir)
		if !result {
			t.Error("expected path under root to return true")
		}
	})

	t.Run("path equals root", func(t *testing.T) {
		result := isPathUnderRoot(tmpDir, tmpDir)
		if !result {
			t.Error("expected path equal to root to return true")
		}
	})

	t.Run("path outside root", func(t *testing.T) {
		result := isPathUnderRoot("/etc/passwd", tmpDir)
		if result {
			t.Error("expected path outside root to return false")
		}
	})

	t.Run("non-existent path under existing root", func(t *testing.T) {
		result := isPathUnderRoot(filepath.Join(tmpDir, "nonexistent", "deep", "path"), tmpDir)
		if !result {
			t.Error("expected non-existent path under root to return true (resolved via parent)")
		}
	})

	t.Run("non-existent root falls back to lexical", func(t *testing.T) {
		result := isPathUnderRoot("/fake/root/sub", "/fake/root")
		if !result {
			t.Error("expected lexical match for non-existent root")
		}
	})

	t.Run("root path / matches subpaths", func(t *testing.T) {
		result := isPathUnderRoot("/home/user", "/")
		if !result {
			t.Error("expected / to match /home/user")
		}
	})

	t.Run("root path / matches itself", func(t *testing.T) {
		result := isPathUnderRoot("/", "/")
		if !result {
			t.Error("expected / to match /")
		}
	})

	t.Run("different temp dir roots", func(t *testing.T) {
		tmpDir2 := t.TempDir()
		result := isPathUnderRoot(tmpDir2, tmpDir)
		if result {
			t.Error("expected different temp dirs to not match")
		}
	})
}

func TestResolveExistingPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Resolve symlinks for macOS compatibility
	if resolved, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = resolved
	}

	t.Run("existing path returns itself", func(t *testing.T) {
		result := resolveExistingPath(tmpDir, "/")
		if result == "" {
			t.Error("expected non-empty result for existing path")
		}
	})

	t.Run("non-existent path resolves via parent", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "a", "b", "c")
		result := resolveExistingPath(nonExistent, tmpDir)
		if result == "" {
			t.Error("expected parent resolution for non-existent path under root")
		}
	})

	t.Run("path outside root returns empty", func(t *testing.T) {
		result := resolveExistingPath("/fake/outside/path", "/real")
		if result != "" {
			t.Errorf("expected empty result for path outside root, got %q", result)
		}
	})
}

func TestClaudeProjectDir(t *testing.T) {
	dir := ClaudeProjectDir("/home/user/project")
	if dir == "" {
		t.Error("ClaudeProjectDir() returned empty string")
	}
	// Should contain "projects" segment
	if len(dir) < 8 {
		t.Errorf("ClaudeProjectDir() = %q, too short", dir)
	}
}
