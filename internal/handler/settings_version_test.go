package handler

import (
	"regexp"
	"testing"

	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
)

func TestGetBuildVersion_WithLdflagsReleaseVersion(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// Simulate release: ldflags-injected version like "v1.0.0"
	version.Version = "v1.0.0"
	result := getBuildVersion()

	// Should return the version string as-is (no SHA appending)
	assert.Equal(t, "v1.0.0", result)
}

func TestGetBuildVersion_WithLdflagsDevVersion(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// Simulate dev build: ldflags-injected version with build time suffix
	version.Version = "v0.30.0-33-ga636beb-05211030"
	result := getBuildVersion()

	// Should return the full string as-is
	assert.Equal(t, "v0.30.0-33-ga636beb-05211030", result)
}

func TestGetBuildVersion_WithLdflagsShortSHA(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// When no git tags exist, git describe returns just the short SHA
	version.Version = "abc1234"
	result := getBuildVersion()

	assert.Equal(t, "abc1234", result)
}

func TestGetBuildVersion_WithoutLdflagsVersion(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// No ldflags — should fall back to VCS short SHA from debug.ReadBuildInfo()
	version.Version = ""
	result := getBuildVersion()

	// In a git repo, result should be the short SHA (7 hex chars)
	assert.NotEmpty(t, result)
	if len(result) >= 7 {
		matched, _ := regexp.MatchString(`^[0-9a-f]{7}`, result)
		assert.True(t, matched, "expected result to start with 7-char hex SHA, got %q", result)
	}
}

func TestGetBuildVersion_LdflagsVersionTakesPrecedence(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// When version is set via ldflags, it should be returned as-is
	// without any additional VCS info appending
	version.Version = "v2.0.0"
	result := getBuildVersion()
	assert.Equal(t, "v2.0.0", result)
}

func TestGetBuildVersion_NoVCSFallbackToDev(t *testing.T) {
	// Verify that when called, it never returns an empty string
	original := version.Version
	defer func() { version.Version = original }()

	version.Version = ""
	result := getBuildVersion()
	assert.NotEmpty(t, result, "getBuildVersion should never return empty string")
}

func TestGetBuildVersion_DevStringFallback(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// Simulate the "dev" fallback when not in a git repo
	version.Version = "dev"
	result := getBuildVersion()
	assert.Equal(t, "dev", result)
}

func TestGetBuildVersion_BuildTimeStringPreserved(t *testing.T) {
	original := version.Version
	defer func() { version.Version = original }()

	// The build time suffix is injected by build.sh in dev builds
	// and should be preserved exactly as-is
	version.Version = "v0.30.0-33-ga636beb-05211030"
	result := getBuildVersion()
	assert.Contains(t, result, "05211030")
	assert.Equal(t, "v0.30.0-33-ga636beb-05211030", result)
}
