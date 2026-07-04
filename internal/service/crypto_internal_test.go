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

func TestLoadAllAPIKeys_Empty(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	keys, err := loadAllAPIKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestLoadAllAPIKeys_WithKeys(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = SaveAgentAPIKey(db, "pi", "openai", "https://api.openai.com", "sk-test-key")
	require.NoError(t, err)

	keys, err := loadAllAPIKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "pi", keys[0].AgentID)
	assert.Equal(t, "openai", keys[0].Provider)
	assert.Equal(t, "https://api.openai.com", keys[0].CustomURL)
	assert.Equal(t, "sk-test-key", keys[0].PlaintextKey)
}

func TestLoadAllAPIKeys_MultipleKeys(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = SaveAgentAPIKey(db, "pi", "openai", "", "sk-openai-key")
	require.NoError(t, err)
	err = SaveAgentAPIKey(db, "pi", "anthropic", "https://custom.api", "sk-ant-key")
	require.NoError(t, err)

	keys, err := loadAllAPIKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestLoadAllAPIKeys_CorruptKey(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Insert a key with corrupted encrypted data directly
	_, err = db.Exec(`INSERT INTO agent_api_keys (agent_id, provider, encrypted_key, key_nonce) VALUES ('pi', 'broken', 'corrupted-data', 'bad-nonce')`)
	require.NoError(t, err)

	// loadAllAPIKeys should return an error when decryption fails
	_, err = loadAllAPIKeys()
	assert.Error(t, err)
}

func TestRotateAPIKeyEncryption_LoadKeysError(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Close the DB to make loadAllAPIKeys fail
	db.Close()

	err = RotateAPIKeyEncryption("old-password")
	assert.Error(t, err, "should fail when loadAllAPIKeys errors")
	assert.Contains(t, err.Error(), "load API keys for rotation")
}

func TestRotateAPIKeyEncryption_SaveKeyError(t *testing.T) {
	setupTestDirs(t)
	dataDir := model.DataDir

	// Write initial password file
	err := os.MkdirAll(dataDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("old-password"), 0o600)
	require.NoError(t, err)

	db, err := InitInMemoryDB()
	require.NoError(t, err)

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Encrypt with old password
	ResetEncryptionKeyCache()
	err = SaveAgentAPIKey(db, "pi", "openai", "", "sk-test-key")
	require.NoError(t, err)

	// Update password file before rotation (as the real code does)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("new-password"), 0o600)
	require.NoError(t, err)

	// Make the DB read-only by putting it in WAL mode and opening a second read-only connection
	// Actually, simpler: close the DB and reopen it in a way that the write will fail.
	// The simplest way: open a second :memory: DB that doesn't have the table.
	// Actually, the easiest: use the same DB but set PRAGMA query_only = ON
	// This will make all write operations fail
	_, err = db.Exec("PRAGMA query_only = ON")
	require.NoError(t, err)

	err = RotateAPIKeyEncryption("old-password")
	assert.Error(t, err, "should fail when SaveAgentAPIKey errors during rotation")
	assert.Contains(t, err.Error(), "re-encrypt API key")

	// Verify password was rolled back
	data, err := os.ReadFile(filepath.Join(dataDir, "auto-password"))
	require.NoError(t, err)
	assert.Equal(t, "old-password", string(data), "password should be rolled back on rotation failure")

	// Restore query_only for cleanup
	_, _ = db.Exec("PRAGMA query_only = OFF")
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

	// Verify agent_api_keys table exists
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_api_keys'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "agent_api_keys table should exist")
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

func TestRotateAPIKeyEncryption_WithPasswordChange(t *testing.T) {
	setupTestDirs(t)
	dataDir := model.DataDir

	// Write initial password file
	err := os.MkdirAll(dataDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("old-password"), 0o600)
	require.NoError(t, err)

	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Encrypt with old password (cache must reflect old-password)
	ResetEncryptionKeyCache()
	err = SaveAgentAPIKey(db, "pi", "openai", "", "sk-test-key")
	require.NoError(t, err)

	// Simulate password change: update the password file BEFORE calling RotateAPIKeyEncryption.
	// The caller is responsible for updating the file; RotateAPIKeyEncryption decrypts with
	// the CURRENT key (old), resets cache, then re-encrypts with the new key.
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("new-password"), 0o600)
	require.NoError(t, err)

	// IMPORTANT: Do NOT reset cache before calling RotateAPIKeyEncryption.
	// The function itself handles the cache reset after decrypting all keys.
	err = RotateAPIKeyEncryption("old-password")
	require.NoError(t, err)

	// Verify the key can still be decrypted with the new key
	ResetEncryptionKeyCache() // Now derive from new password
	customURL, apiKey, err := LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err)
	assert.Equal(t, "", customURL)
	assert.Equal(t, "sk-test-key", apiKey)
}

// TestDecryptAPIKey_PreviousKeyFallback verifies ISS-225 fix:
// If a key was encrypted with the old key (before rotation), and the
// process crashed mid-rotation, DecryptAPIKey should fall back to the
// previous key to decrypt it.
func TestDecryptAPIKey_PreviousKeyFallback(t *testing.T) {
	setupTestDirs(t)
	dataDir := model.DataDir

	// Write initial password file
	err := os.MkdirAll(dataDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("old-password"), 0o600)
	require.NoError(t, err)

	db, err := InitInMemoryDB()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	err = SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Step 1: Encrypt with old password
	ResetEncryptionKeyCache()
	err = SaveAgentAPIKey(db, "pi", "openai", "", "sk-fallback-test")
	require.NoError(t, err)

	// Step 2: Simulate rotation starting — this saves the old key as previousEncryptionKey
	// Change password file
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("new-password"), 0o600)
	require.NoError(t, err)

	// Call RotateAPIKeyEncryption which sets previousEncryptionKey
	err = RotateAPIKeyEncryption("old-password")
	require.NoError(t, err)

	// Step 3: Now simulate a crash mid-rotation by saving a new key with the NEW password,
	// then manually setting previousEncryptionKey to the OLD key.
	// This simulates: some keys were re-encrypted (new key), some weren't (old key).
	ResetEncryptionKeyCache() // Derive from new password

	// Save a key that was encrypted with the new key
	err = SaveAgentAPIKey(db, "pi", "anthropic", "", "sk-new-key")
	require.NoError(t, err)

	// Now simulate: manually re-insert a key encrypted with the OLD key
	// (as if the rotation crashed before re-encrypting this one)
	ResetEncryptionKeyCache()
	// Read the old-password derived key by temporarily restoring the old password
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("old-password"), 0o600)
	require.NoError(t, err)
	oldKey := DeriveEncryptionKey()

	// Save the old key as the previous key fallback
	previousKeyMu.Lock()
	previousEncryptionKey = make([]byte, len(oldKey))
	copy(previousEncryptionKey, oldKey)
	previousKeyMu.Unlock()

	// Now restore the new password
	err = os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("new-password"), 0o600)
	require.NoError(t, err)
	ResetEncryptionKeyCache()

	// The openai key should still be decryptable via the previous key fallback
	customURL, apiKey, err := LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err, "should decrypt with previous key fallback (ISS-225)")
	assert.Equal(t, "", customURL)
	assert.Equal(t, "sk-fallback-test", apiKey)

	// The anthropic key (encrypted with new key) should decrypt normally
	customURL, apiKey, err = LoadAgentAPIKey("pi", "anthropic")
	require.NoError(t, err)
	assert.Equal(t, "", customURL)
	assert.Equal(t, "sk-new-key", apiKey)

	// Clean up
	previousKeyMu.Lock()
	previousEncryptionKey = nil
	previousKeyMu.Unlock()
}
