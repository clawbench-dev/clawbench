package codex

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodexModelRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"gpt-5.5", true},
		{"gpt-5.4", true},
		{"gpt-5.4-mini", true},
		{"o3", true},
		{"o4-mini", true},
		{"gpt-4", false},         // single version segment
		{"gpt-4.1", true},        // matches gpt-\d+\.\d+
		{"o3-mini", true},        // matches o[34](-mini)?
		{"o4", true},             // matches o[34]
		{"gpt-3.5-turbo", false}, // "turbo" is not "-mini", regex only allows -mini suffix
		{"claude-sonnet-4", false},
		{"model-x", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, codexModelRe.MatchString(tt.input))
		})
	}
}

func TestCodexModelOrder(t *testing.T) {
	assert.Equal(t, 0, codexModelOrder["gpt-5.5"], "gpt-5.5 should come first")
	assert.Equal(t, 1, codexModelOrder["gpt-5.4"])
	assert.Equal(t, 2, codexModelOrder["gpt-5.4-mini"])
	assert.Equal(t, 3, codexModelOrder["o3"])
	assert.Equal(t, 4, codexModelOrder["o4-mini"])
}

func TestCodexTargetTriple(t *testing.T) {
	triple := codexTargetTriple()

	switch runtime.GOOS {
	case "linux", "android":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-unknown-linux-musl", triple)
		case "arm64":
			assert.Equal(t, "aarch64-unknown-linux-musl", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-apple-darwin", triple)
		case "arm64":
			assert.Equal(t, "aarch64-apple-darwin", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-pc-windows-msvc", triple)
		case "arm64":
			assert.Equal(t, "aarch64-pc-windows-msvc", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	default:
		assert.Empty(t, triple, "unsupported OS should return empty")
	}
}

func TestCodexDefaultModels_Structure(t *testing.T) {
	assert.NotEmpty(t, codexDefaultModels)

	defaultCount := 0
	for _, m := range codexDefaultModels {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be default")
}

func TestCodexDefaultModels_FirstIsDefault(t *testing.T) {
	assert.NotEmpty(t, codexDefaultModels)
	assert.True(t, codexDefaultModels[0].Default)
	assert.Equal(t, "gpt-5.5", codexDefaultModels[0].ID)
}

func TestDiscoverCodexModels_NoCLI(t *testing.T) {
	// When codex CLI is not installed, all strategies return nil.
	models := DiscoverCodexModels()
	// Result depends on installation; just verify no panic
	_ = models
}

func TestDiscoverCodexModelsDefaults_NoCLI(t *testing.T) {
	// When codex is not on PATH, defaults should return nil
	models := discoverCodexModelsDefaults()
	_ = models // may be nil if not installed, just verify no panic
}
