package service

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDirs creates a temp dir, sets BinDir and DataDir, and returns a cleanup function.
func setupTestDirs(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() { model.BinDir = origBinDir; model.DataDir = origDataDir })
}

func TestDeriveFallbackKey(t *testing.T) {
	key := deriveFallbackKey()
	assert.Len(t, key, 32, "fallback key should be 32 bytes")

	// Should be deterministic
	key2 := deriveFallbackKey()
	assert.Equal(t, key, key2, "fallback key should be deterministic")
}

func TestReadAutoPassword_FileExists(t *testing.T) {
	setupTestDirs(t)
	dataDir := model.DataDir

	// Write auto-password file
	err := os.MkdirAll(dataDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("test-password-123"), 0o600)
	require.NoError(t, err)

	password := readAutoPassword()
	assert.Equal(t, "test-password-123", password)
}

func TestReadAutoPassword_NoFile(t *testing.T) {
	setupTestDirs(t)

	password := readAutoPassword()
	assert.Equal(t, "", password, "should return empty string when file doesn't exist")
}

func TestReadAutoPassword_EmptyBinDir(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = ""
	defer func() { model.DataDir = origDataDir }()

	password := readAutoPassword()
	assert.Equal(t, "", password, "should return empty string when DataDir is empty")
}

func TestDeriveKeyFromPassword_WithPassword(t *testing.T) {
	setupTestDirs(t)
	dataDir := model.DataDir

	// Write auto-password file
	err := os.MkdirAll(dataDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("my-secret-password"), 0o600)
	require.NoError(t, err)

	ResetEncryptionKeyCache()
	key := DeriveEncryptionKey()
	assert.Len(t, key, 32)
}

func TestDeriveKeyFromPassword_NoPassword(t *testing.T) {
	setupTestDirs(t)

	// No auto-password file — HKDF will use empty string, not fallback
	ResetEncryptionKeyCache()
	key := DeriveEncryptionKey()
	assert.Len(t, key, 32)

	// Should be deterministic
	ResetEncryptionKeyCache()
	key2 := DeriveEncryptionKey()
	assert.Equal(t, key, key2, "key derivation should be deterministic even without password")
}

func TestInitInMemoryDB_Success(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Verify agents table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agents'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "agents table should exist")
}

func TestDeriveKeyFromPassword_FallbackKey(t *testing.T) {
	// Test that deriveKeyFromPassword produces a valid key when DataDir is empty
	// (HKDF with empty password should succeed, not hit the fallback path)
	origDataDir := model.DataDir
	model.DataDir = ""
	defer func() { model.DataDir = origDataDir }()

	ResetEncryptionKeyCache()
	key := deriveKeyFromPassword()
	assert.Len(t, key, 32, "derived key should be 32 bytes")
}

func TestDeriveEncryptionKey_ConcurrentAccess(t *testing.T) {
	ResetEncryptionKeyCache()

	// Call DeriveEncryptionKey from multiple goroutines to test thread safety
	done := make(chan []byte, 5)
	for range 5 {
		go func() {
			done <- DeriveEncryptionKey()
		}()
	}

	keys := make([][]byte, 0, 5)
	for range 5 {
		keys = append(keys, <-done)
	}

	// All goroutines should get the same key
	for i := 1; i < len(keys); i++ {
		assert.Equal(t, keys[0], keys[i], "concurrent DeriveEncryptionKey calls should return the same key")
	}
}
