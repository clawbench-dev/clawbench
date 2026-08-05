package service_test

import (
	"encoding/base64"
	"fmt"
	"testing"

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
