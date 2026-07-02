package model

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeConfig_IsChatSummaryEnabled_Nil(t *testing.T) {
	cfg := SummarizeConfig{}
	// ChatSummary is nil — should default to true
	assert.True(t, cfg.IsChatSummaryEnabled())
}

func TestSummarizeConfig_IsChatSummaryEnabled_True(t *testing.T) {
	val := true
	cfg := SummarizeConfig{ChatSummary: &val}
	assert.True(t, cfg.IsChatSummaryEnabled())
}

func TestSummarizeConfig_IsChatSummaryEnabled_False(t *testing.T) {
	val := false
	cfg := SummarizeConfig{ChatSummary: &val}
	assert.False(t, cfg.IsChatSummaryEnabled())
}

func TestIsSHA256Password_Plaintext(t *testing.T) {
	assert.False(t, IsSHA256Password("my-password"))
}

func TestIsSHA256Password_SHA256Prefix(t *testing.T) {
	assert.True(t, IsSHA256Password("sha256:abc123"))
}

func TestIsSHA256Password_Empty(t *testing.T) {
	assert.False(t, IsSHA256Password(""))
}

func TestParseSHA256Hash_Plaintext(t *testing.T) {
	assert.Equal(t, "", ParseSHA256Hash("my-password"))
}

func TestParseSHA256Hash_ValidSHA256(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	result := ParseSHA256Hash("sha256:" + hash)
	assert.Equal(t, hash, result)
}

func TestParseSHA256Hash_InvalidTooShort(t *testing.T) {
	result := ParseSHA256Hash("sha256:abc")
	assert.Equal(t, "", result)
}

func TestParseSHA256Hash_InvalidTooLong(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789extra"
	result := ParseSHA256Hash("sha256:" + hash)
	assert.Equal(t, "", result) // 64+5=69 chars, not 64
}

func TestApplyDefaults_SHA256PasswordRemovesAutoPasswordFile(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := BinDir
	origDataDir := DataDir
	BinDir = tmpDir
	DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { BinDir = origBinDir; DataDir = origDataDir }()

	// Create a stale auto-password file
	autoFile := filepath.Join(DataDir, "auto-password")
	require.NoError(t, os.MkdirAll(filepath.Dir(autoFile), 0o755))
	require.NoError(t, os.WriteFile(autoFile, []byte("old-auto-password"), 0o600))

	// Create a SHA-256 password
	hash := sha256.Sum256([]byte("test-password" + "clawbench-salt"))
	sha256Password := "sha256:" + hex.EncodeToString(hash[:])

	cfg := &Config{Password: sha256Password, Port: 30000}
	ApplyDefaults(cfg, nil)

	// Auto-password file should be removed for SHA-256 stored password
	_, err := os.Stat(autoFile)
	assert.True(t, os.IsNotExist(err), "auto-password file should be removed when SHA-256 password is set")
}

// --- ISS-117/131/183: GenerateRandomToken, PersistCookieToken, LoadCookieToken ---

func TestGenerateRandomToken_Length(t *testing.T) {
	token := GenerateRandomToken(32)
	// 32 bytes = 64 hex chars
	assert.Len(t, token, 64)
}

func TestGenerateRandomToken_Uniqueness(t *testing.T) {
	token1 := GenerateRandomToken(32)
	token2 := GenerateRandomToken(32)
	assert.NotEqual(t, token1, token2, "two random tokens should differ")
}

func TestGenerateRandomToken_HexChars(t *testing.T) {
	token := GenerateRandomToken(16)
	for _, c := range token {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"token should only contain hex chars, got: %c", c)
	}
}

func TestPersistAndLoadCookieToken(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := BinDir
	origDataDir := DataDir
	BinDir = tmpDir
	DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { BinDir = origBinDir; DataDir = origDataDir }()

	token := GenerateRandomToken(32)
	PersistCookieToken(token)

	loaded := LoadCookieToken()
	assert.Equal(t, token, loaded)
}

func TestLoadCookieToken_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := BinDir
	origDataDir := DataDir
	BinDir = tmpDir
	DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { BinDir = origBinDir; DataDir = origDataDir }()

	// No file exists yet
	loaded := LoadCookieToken()
	assert.Equal(t, "", loaded)
}

func TestLoadCookieToken_EmptyBinDir(t *testing.T) {
	origDataDir := DataDir
	DataDir = ""
	defer func() { DataDir = origDataDir }()

	loaded := LoadCookieToken()
	assert.Equal(t, "", loaded, "empty DataDir should return empty token")
}

func TestPersistCookieToken_EmptyBinDir(t *testing.T) {
	origDataDir := DataDir
	DataDir = ""
	defer func() { DataDir = origDataDir }()

	// Should not panic
	PersistCookieToken("some-token")
}

func TestPersistCookieToken_WriteFailure(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := BinDir
	origDataDir := DataDir
	BinDir = tmpDir
	DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { BinDir = origBinDir; DataDir = origDataDir }()

	// Create .clawbench as a read-only file (not a directory) to cause WriteFile to fail
	clawbenchDir := DataDir
	require.NoError(t, os.WriteFile(clawbenchDir, []byte("not-a-dir"), 0o600))

	// Should not panic — error is silently ignored
	PersistCookieToken("some-token")
}

func TestScopedCookieName_DefaultPort(t *testing.T) {
	orig := ServerPort
	defer func() { ServerPort = orig }()

	ServerPort = 0
	assert.Equal(t, "clawbench_session", ScopedCookieName("clawbench_session"))

	ServerPort = 20000
	assert.Equal(t, "clawbench_session", ScopedCookieName("clawbench_session"))
}

func TestScopedCookieName_NonDefaultPort(t *testing.T) {
	orig := ServerPort
	defer func() { ServerPort = orig }()

	ServerPort = 20300
	assert.Equal(t, "cb20300_clawbench_session", ScopedCookieName("clawbench_session"))
	assert.Equal(t, "cb20300_clawbench_project", ScopedCookieName("clawbench_project"))
	assert.Equal(t, "cb20300_chat_session_id", ScopedCookieName("chat_session_id"))
	assert.Equal(t, "cb20300_clawbench-locale", ScopedCookieName("clawbench-locale"))
}
