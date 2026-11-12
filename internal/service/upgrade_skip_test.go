package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadSkippedUpgradeVersion / WriteSkippedUpgradeVersion ---

func TestSkippedUpgradeVersion_RoundTrip(t *testing.T) {
	withTempDataDir(t)

	stored, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", stored)
	assert.Equal(t, "1.2.3", ReadSkippedUpgradeVersion())
}

// The registry answers without a leading "v" while some callers spell it with
// one; both spellings must land on the same record, otherwise a version the
// user skipped keeps looking unskipped.
func TestSkippedUpgradeVersion_NormalizesSpelling(t *testing.T) {
	withTempDataDir(t)

	stored, err := WriteSkippedUpgradeVersion("  v1.2.3 \n")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", stored)
	assert.Equal(t, "1.2.3", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_EmptyMeansNothingSkipped(t *testing.T) {
	withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)

	stored, err := WriteSkippedUpgradeVersion("")
	require.NoError(t, err)
	assert.Equal(t, "", stored)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_ClearWhenNothingRecorded(t *testing.T) {
	withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("")
	require.NoError(t, err, "clearing an absent record is the requested state, not a failure")
}

func TestSkippedUpgradeVersion_ReadWithoutRecord(t *testing.T) {
	withTempDataDir(t)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_RejectsInvalidInput(t *testing.T) {
	withTempDataDir(t)

	cases := map[string]string{
		"too long":          strings.Repeat("9", maxSkippedVersionLen+1),
		"newline injection": "1.2.3\n1.2.4",
		"embedded CR":       "1.2\r.3",
		"embedded NUL":      "1.2.3\x00",
		"embedded vtab":     "1.2\x0b3",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := WriteSkippedUpgradeVersion(input)
			assert.ErrorIs(t, err, ErrSkippedVersionInvalid)
			assert.Equal(t, "", ReadSkippedUpgradeVersion(), "a rejected write must not leave a record")
		})
	}
}

func TestSkippedUpgradeVersion_AcceptsMaxLength(t *testing.T) {
	withTempDataDir(t)

	longest := strings.Repeat("9", maxSkippedVersionLen)
	_, err := WriteSkippedUpgradeVersion(longest)
	require.NoError(t, err)
	assert.Equal(t, longest, ReadSkippedUpgradeVersion())
}

// The record lives in the data directory next to the binary path and the cookie
// token, so it must not be world-readable.
func TestSkippedUpgradeVersion_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on Windows")
	}
	dir := withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, upgradeSkipFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Without a data directory there is nowhere durable to put the choice, so the
// write reports failure instead of pretending it was remembered. Reads report
// "nothing skipped" — one extra prompt beats failing the check.
func TestSkippedUpgradeVersion_NoDataDir(t *testing.T) {
	orig := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = orig })

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	assert.Error(t, err)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}
