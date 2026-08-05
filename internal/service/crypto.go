//nolint:noctx // DB parameter, context not applicable
package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"clawbench/internal/model"

	"golang.org/x/crypto/hkdf"
)

// encryptionKeyCache caches the derived encryption key to avoid re-reading
// the auto-password file on every encrypt/decrypt operation.
var (
	encryptionKeyCache []byte
	encryptionKeyOnce  sync.Once
	encryptionKeyMu    sync.RWMutex // protects cache for rotation

	// previousEncryptionKey caches the key from before a rotation.
	// Used by DecryptAPIKey as fallback when the current key fails to decrypt,
	// which can happen if the process crashed mid-rotation (ISS-225).
	previousEncryptionKey []byte
	previousKeyMu         sync.RWMutex
)

// DeriveEncryptionKey derives a 32-byte AES-256 key from the ClawBench auto-password
// using HKDF-SHA256. The auto-password is the same secret used for web UI authentication,
// so the encryption is only as strong as the login password.
// Thread-safe: uses sync.Once for initial derivation and RWMutex for rotation.
func DeriveEncryptionKey() []byte {
	encryptionKeyMu.RLock()
	if encryptionKeyCache != nil {
		defer encryptionKeyMu.RUnlock()
		return encryptionKeyCache
	}
	encryptionKeyMu.RUnlock()

	encryptionKeyOnce.Do(func() {
		key := deriveKeyFromPassword()
		encryptionKeyMu.Lock()
		encryptionKeyCache = key
		encryptionKeyMu.Unlock()
	})

	encryptionKeyMu.RLock()
	defer encryptionKeyMu.RUnlock()
	return encryptionKeyCache
}

// deriveKeyFromPassword reads the auto-password and derives an AES-256 key via HKDF-SHA256.
func deriveKeyFromPassword() []byte {
	// Read auto-password
	salt := []byte("clawbench-salt")
	password := readAutoPassword()

	// Derive key via HKDF-SHA256
	hkdfReader := hkdf.New(sha256.New, []byte(password), salt, []byte("clawbench-agent-api-key"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		// Fallback: use a fixed key (dev mode, no password set)
		slog.Warn("HKDF key derivation failed, using fallback key", "error", err)
		key = deriveFallbackKey()
	}
	return key
}

// readAutoPassword reads the auto-password from .clawbench/auto-password.
// Returns empty string if not found.
func readAutoPassword() string {
	if model.DataDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(model.DataDir, "auto-password"))
	if err != nil {
		return ""
	}
	return string(data)
}

// deriveFallbackKey produces a deterministic key for dev mode (no password).
// This is acceptable because dev mode implies localhost-only access.
func deriveFallbackKey() []byte {
	h := sha256.New()
	h.Write([]byte("clawbench-dev-fallback-key"))
	return h.Sum(nil)
}

// EncryptAPIKey encrypts a plaintext API key using AES-256-GCM.
// Returns base64-encoded ciphertext and nonce.
func EncryptAPIKey(plaintext string) (encrypted, nonce string, err error) {
	key := DeriveEncryptionKey()

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("create GCM: %w", err)
	}

	// Generate random nonce
	nonceBytes := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt and seal
	ciphertext := aesGCM.Seal(nil, nonceBytes, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(nonceBytes),
		nil
}

// DecryptAPIKey decrypts a base64-encoded ciphertext using AES-256-GCM.
// If decryption with the current key fails, falls back to the previous key
// (from before a password change rotation) to handle mid-rotation crashes (ISS-225).
func DecryptAPIKey(encrypted, nonce string) (string, error) {
	key := DeriveEncryptionKey()

	plaintext, err := decryptWithKey(key, encrypted, nonce)
	if err == nil {
		return plaintext, nil
	}

	// Fallback: try the previous encryption key (from before rotation)
	previousKeyMu.RLock()
	prevKey := previousEncryptionKey
	previousKeyMu.RUnlock()

	if prevKey != nil {
		plaintext, prevErr := decryptWithKey(prevKey, encrypted, nonce)
		if prevErr == nil {
			slog.Warn("decrypted API key with previous (pre-rotation) key — rotation may be incomplete (ISS-225)")
			return plaintext, nil
		}
	}

	return "", fmt.Errorf("decrypt: %w", err)
}

// decryptWithKey attempts AES-256-GCM decryption with a specific key.
func decryptWithKey(key []byte, encrypted, nonce string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	if len(nonceBytes) != aesGCM.NonceSize() {
		return "", fmt.Errorf("invalid nonce size: got %d, want %d", len(nonceBytes), aesGCM.NonceSize())
	}

	plaintext, err := aesGCM.Open(nil, nonceBytes, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// ResetEncryptionKeyCache clears the cached encryption key and resets the once guard.
// Used during API key rotation (password change) and in tests.
func ResetEncryptionKeyCache() {
	encryptionKeyMu.Lock()
	encryptionKeyCache = nil
	encryptionKeyOnce = sync.Once{}
	encryptionKeyMu.Unlock()
}
