package service_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptAPIKey_RoundTrip(t *testing.T) {
	testKeys := []string{
		"sk-1234567890abcdef",
		"sk-cp-s_zVlSNstte7xON5i9aF85cVPX1UCSiKuVt-5vmPFEGZG8sCu-09AdEWQgHG7FgkOFC1xsLtS-wwHTgM_RZFo7u6F1VB0A06sSi7zSuw-_jfT6656fnWSJo",
		"AIzaSyB1234567890",
		"",
	}

	for _, key := range testKeys {
		t.Run("key_len_"+fmt.Sprintf("%d", len(key)), func(t *testing.T) {
			encrypted, nonce, err := service.EncryptAPIKey(key)
			require.NoError(t, err)

			// Encrypted text should differ from plaintext
			if key != "" {
				assert.NotEqual(t, key, encrypted)
			}

			// Decrypt should recover the original
			decrypted, err := service.DecryptAPIKey(encrypted, nonce)
			require.NoError(t, err)
			assert.Equal(t, key, decrypted)
		})
	}
}

func TestEncryptAPIKey_DifferentNonces(t *testing.T) {
	key := "sk-test-key-123"

	encrypted1, nonce1, err := service.EncryptAPIKey(key)
	require.NoError(t, err)

	encrypted2, nonce2, err := service.EncryptAPIKey(key)
	require.NoError(t, err)

	// Same key encrypted twice should produce different nonces and ciphertexts
	assert.NotEqual(t, nonce1, nonce2, "nonces should be different")
	assert.NotEqual(t, encrypted1, encrypted2, "ciphertexts should be different")

	// Both should decrypt correctly
	dec1, err := service.DecryptAPIKey(encrypted1, nonce1)
	require.NoError(t, err)
	assert.Equal(t, key, dec1)

	dec2, err := service.DecryptAPIKey(encrypted2, nonce2)
	require.NoError(t, err)
	assert.Equal(t, key, dec2)
}

func TestDecryptAPIKey_InvalidCiphertext(t *testing.T) {
	_, err := service.DecryptAPIKey("not-valid-base64!!!", "also-not-valid!!!")
	assert.Error(t, err)
}

func TestDecryptAPIKey_WrongNonce(t *testing.T) {
	key := "sk-test-key"
	encrypted, _, err := service.EncryptAPIKey(key)
	require.NoError(t, err)

	// Encrypt something else to get a different nonce
	_, wrongNonce, err := service.EncryptAPIKey("other-key")
	require.NoError(t, err)

	// Decrypting with wrong nonce should fail
	_, err = service.DecryptAPIKey(encrypted, wrongNonce)
	assert.Error(t, err)
}

func TestDeriveEncryptionKey_Deterministic(t *testing.T) {
	service.ResetEncryptionKeyCache()
	key1 := service.DeriveEncryptionKey()
	service.ResetEncryptionKeyCache()
	key2 := service.DeriveEncryptionKey()

	// Same environment should produce the same key
	assert.Equal(t, key1, key2)
}

func TestDeriveEncryptionKey_Length(t *testing.T) {
	service.ResetEncryptionKeyCache()
	key := service.DeriveEncryptionKey()

	// AES-256 key should be 32 bytes
	assert.Len(t, key, 32)
}

func TestSaveAndLoadAgentAPIKey(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert agent first
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Save API key
	err = service.SaveAgentAPIKey(db, "pi", "openai", "https://api.openai.com", "sk-test-key-12345")
	require.NoError(t, err)

	// Load API key
	customURL, apiKey, err := service.LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com", customURL)
	assert.Equal(t, "sk-test-key-12345", apiKey)
}

func TestSaveAndLoadAgentAPIKey_NoCustomURL(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Save without custom URL
	err = service.SaveAgentAPIKey(db, "pi", "anthropic", "", "sk-ant-test-key")
	require.NoError(t, err)

	customURL, apiKey, err := service.LoadAgentAPIKey("pi", "anthropic")
	require.NoError(t, err)
	assert.Equal(t, "", customURL)
	assert.Equal(t, "sk-ant-test-key", apiKey)
}

func TestLoadAgentAPIKey_NotFound(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	_, _, err = service.LoadAgentAPIKey("pi", "nonexistent-provider")
	assert.Error(t, err)
}

func TestSaveAgentAPIKey_Upsert(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Save first time
	err = service.SaveAgentAPIKey(db, "pi", "openai", "", "sk-old-key")
	require.NoError(t, err)

	// Upsert with new key
	err = service.SaveAgentAPIKey(db, "pi", "openai", "", "sk-new-key")
	require.NoError(t, err)

	// Should have the new key
	_, apiKey, err := service.LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-new-key", apiKey)

	// Should still be only one record
	var count int
	db.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = 'pi' AND provider = 'openai'").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestRotateAPIKeyEncryption_NoKeys(t *testing.T) {
	_ = setupTestDBForAgents(t)

	// No API keys stored — rotation should succeed with no-op
	err := service.RotateAPIKeyEncryption("old-password")
	assert.NoError(t, err)
}

func TestLoadAgentAnyAPIKey_Found(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = service.SaveAgentAPIKey(db, "pi", "openai", "https://api.openai.com", "sk-test-any-key")
	require.NoError(t, err)

	provider, customURL, apiKey, err := service.LoadAgentAnyAPIKey("pi")
	require.NoError(t, err)
	assert.Equal(t, "openai", provider)
	assert.Equal(t, "https://api.openai.com", customURL)
	assert.Equal(t, "sk-test-any-key", apiKey)
}

func TestLoadAgentAnyAPIKey_NotFound(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// No API keys stored for this agent
	provider, customURL, apiKey, err := service.LoadAgentAnyAPIKey("pi")
	require.NoError(t, err)
	assert.Equal(t, "", provider)
	assert.Equal(t, "", customURL)
	assert.Equal(t, "", apiKey)
}

func TestLoadAgentAnyAPIKey_MultipleProviders(t *testing.T) {
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = service.SaveAgentAPIKey(db, "pi", "openai", "", "sk-openai-key")
	require.NoError(t, err)
	err = service.SaveAgentAPIKey(db, "pi", "anthropic", "", "sk-ant-key")
	require.NoError(t, err)

	// Should return one of the providers (LIMIT 1)
	provider, _, apiKey, err := service.LoadAgentAnyAPIKey("pi")
	require.NoError(t, err)
	assert.NotEmpty(t, provider)
	assert.NotEmpty(t, apiKey)
}

func TestDecryptAPIKey_InvalidNonceSize(t *testing.T) {
	// Create a valid encrypted value, then try decrypting with a wrong-size nonce
	key := "sk-test-key"
	encrypted, _, err := service.EncryptAPIKey(key)
	require.NoError(t, err)

	// Create a nonce of wrong size (1 byte instead of 12)
	invalidNonce := "AA=="
	_, err = service.DecryptAPIKey(encrypted, invalidNonce)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid nonce size")
}

func TestDecryptAPIKey_InvalidNonceBase64(t *testing.T) {
	// Valid base64 ciphertext but invalid nonce
	_, err := service.DecryptAPIKey("dGVzdA==", "!!!invalid!!!")
	assert.Error(t, err)
}

func TestSaveAgentAPIKey_EncryptError(t *testing.T) {
	// SaveAgentAPIKey calls EncryptAPIKey internally which should work in normal cases.
	// This test just verifies the happy path with a non-empty key.
	db := setupTestDBForAgents(t)

	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = service.SaveAgentAPIKey(db, "pi", "openai", "", "valid-api-key")
	require.NoError(t, err)
}

func TestDeriveEncryptionKey_Cached(t *testing.T) {
	service.ResetEncryptionKeyCache()
	key1 := service.DeriveEncryptionKey()
	key2 := service.DeriveEncryptionKey() // Should return cached value
	assert.Equal(t, key1, key2)
	assert.Len(t, key2, 32)
}

func TestDecryptAPIKey_TamperedCiphertext(t *testing.T) {
	key := "sk-test-key"
	encrypted, nonce, err := service.EncryptAPIKey(key)
	require.NoError(t, err)

	// Tamper with the ciphertext (flip some bits)
	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	if len(decoded) > 0 {
		decoded[0] ^= 0xFF
	}
	tampered := base64.StdEncoding.EncodeToString(decoded)

	_, err = service.DecryptAPIKey(tampered, nonce)
	assert.Error(t, err)
}

func TestRotateAPIKeyEncryption_WithKeys(t *testing.T) {
	db := setupTestDBForAgents(t)
	service.ResetEncryptionKeyCache()

	// Create agent and save API keys
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	err = service.SaveAgentAPIKey(db, "pi", "openai", "", "sk-test-key-1")
	require.NoError(t, err)

	err = service.SaveAgentAPIKey(db, "pi", "anthropic", "https://custom.api", "sk-ant-key-2")
	require.NoError(t, err)

	// Verify keys can be decrypted before rotation
	customURL, apiKey, err := service.LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err)
	assert.Equal(t, "", customURL)
	assert.Equal(t, "sk-test-key-1", apiKey)

	customURL, apiKey, err = service.LoadAgentAPIKey("pi", "anthropic")
	require.NoError(t, err)
	assert.Equal(t, "https://custom.api", customURL)
	assert.Equal(t, "sk-ant-key-2", apiKey)

	// Simulate password change by resetting the encryption key cache
	// (In production, the auto-password file would have changed before calling RotateAPIKeyEncryption)
	service.ResetEncryptionKeyCache()

	// Rotate — since the auto-password hasn't actually changed in this test env,
	// DeriveEncryptionKey will return the same key. This test validates the
	// round-trip works: decrypt → reset cache → re-encrypt → decrypt.
	err = service.RotateAPIKeyEncryption("old-password")
	require.NoError(t, err)

	// Verify keys can still be decrypted after rotation
	_, apiKey, err = service.LoadAgentAPIKey("pi", "openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key-1", apiKey)

	customURL, apiKey, err = service.LoadAgentAPIKey("pi", "anthropic")
	require.NoError(t, err)
	assert.Equal(t, "https://custom.api", customURL)
	assert.Equal(t, "sk-ant-key-2", apiKey)
}
