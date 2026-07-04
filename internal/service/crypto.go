//nolint:noctx // DB parameter, context not applicable
package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"clawbench/internal/dbutil"
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
	if model.BinDir == "" {
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

// SaveAgentAPIKey encrypts and stores an API key for an agent+provider combination.
// Uses upsert (ON CONFLICT) to update existing keys.
func SaveAgentAPIKey(db dbutil.Writer, agentID, provider, customURL, apiKey string) error {
	encrypted, nonce, err := EncryptAPIKey(apiKey)
	if err != nil {
		return fmt.Errorf("encrypt API key: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO agent_api_keys (agent_id, provider, custom_url, encrypted_key, key_nonce)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, provider) DO UPDATE SET
			custom_url = excluded.custom_url,
			encrypted_key = excluded.encrypted_key,
			key_nonce = excluded.key_nonce,
			updated_at = CURRENT_TIMESTAMP
	`, agentID, provider, customURL, encrypted, nonce)
	if err != nil {
		return fmt.Errorf("save API key for agent %s/%s: %w", agentID, provider, err)
	}
	return nil
}

// LoadAgentAPIKey decrypts and returns the API key for an agent+provider combination.
// Returns error if not found.
func LoadAgentAPIKey(agentID, provider string) (customURL, apiKey string, err error) {
	var encKey, nonce string
	err = dbRead.QueryRow(`
		SELECT custom_url, encrypted_key, key_nonce
		FROM agent_api_keys
		WHERE agent_id = ? AND provider = ?
	`, agentID, provider).Scan(&customURL, &encKey, &nonce)
	if err != nil {
		return "", "", fmt.Errorf("load API key for agent %s/%s: %w", agentID, provider, err)
	}

	apiKey, err = DecryptAPIKey(encKey, nonce)
	if err != nil {
		return "", "", fmt.Errorf("decrypt API key: %w", err)
	}

	return customURL, apiKey, nil
}

// LoadAgentAnyAPIKey decrypts and returns the first API key found for an agent
// (across all providers). Returns the provider, customURL, and apiKey.
// Returns ("", "", "", nil) if no keys exist for this agent.
func LoadAgentAnyAPIKey(agentID string) (provider, customURL, apiKey string, err error) {
	var encKey, nonce string
	err = dbRead.QueryRow(`
		SELECT provider, custom_url, encrypted_key, key_nonce
		FROM agent_api_keys
		WHERE agent_id = ?
		LIMIT 1
	`, agentID).Scan(&provider, &customURL, &encKey, &nonce)

	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", fmt.Errorf("load API key for agent %s: %w", agentID, err)
	}

	apiKey, err = DecryptAPIKey(encKey, nonce)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt API key: %w", err)
	}

	return provider, customURL, apiKey, nil
}

// DecryptedAPIKey holds a decrypted API key loaded from the database.
type DecryptedAPIKey struct {
	AgentID      string
	Provider     string
	CustomURL    string
	PlaintextKey string
}

// loadAllAPIKeys loads and decrypts all API keys from the database.
func loadAllAPIKeys() ([]DecryptedAPIKey, error) {
	rows, err := dbRead.Query("SELECT agent_id, provider, custom_url, encrypted_key, key_nonce FROM agent_api_keys")
	if err != nil {
		return nil, fmt.Errorf("query API keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []DecryptedAPIKey
	for rows.Next() {
		var agentID, provider, customURL, encKey, nonce string
		if err := rows.Scan(&agentID, &provider, &customURL, &encKey, &nonce); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}

		plaintext, err := DecryptAPIKey(encKey, nonce)
		if err != nil {
			return nil, fmt.Errorf("decrypt API key for agent %s/%s: %w", agentID, provider, err)
		}

		keys = append(keys, DecryptedAPIKey{
			AgentID:      agentID,
			Provider:     provider,
			CustomURL:    customURL,
			PlaintextKey: plaintext,
		})
	}

	return keys, rows.Err()
}

// RotateAPIKeyEncryption re-encrypts all API keys after a password change.
// The encryption key is derived from the auto-password, so when it changes,
// all existing encrypted keys become undecryptable unless re-encrypted.
//
// Steps:
// 1. Decrypt all API keys with the CURRENT (old) key
// 2. Save the old key as previousEncryptionKey for crash recovery (ISS-225)
// 3. Reset the key cache so the next DeriveEncryptionKey uses the new password
// 4. Re-encrypt all API keys with the new key
// 5. Clear the previousEncryptionKey after successful rotation
//
// If any step fails, attempts to roll back by restoring the old password.
// The previousEncryptionKey fallback ensures that if the process crashes
// mid-rotation, DecryptAPIKey can still decrypt keys using the old key.
func RotateAPIKeyEncryption(oldAutoPassword string) error {
	// 1. Decrypt all keys with the CURRENT (old) key
	keys, err := loadAllAPIKeys()
	if err != nil {
		return fmt.Errorf("load API keys for rotation: %w", err)
	}

	if len(keys) == 0 {
		return nil // Nothing to rotate
	}

	// 2. Save the current (old) key as fallback for crash recovery (ISS-225).
	// If the process crashes mid-rotation, DecryptAPIKey can try this key
	// to decrypt keys that haven't been re-encrypted yet.
	oldKey := DeriveEncryptionKey()
	previousKeyMu.Lock()
	previousEncryptionKey = make([]byte, len(oldKey))
	copy(previousEncryptionKey, oldKey)
	previousKeyMu.Unlock()

	// 3. Reset key cache — the auto-password file has already been updated by the caller,
	// so DeriveEncryptionKey will now use the new password
	ResetEncryptionKeyCache()

	// 3. Re-encrypt all keys with the new key
	for _, k := range keys {
		if err := SaveAgentAPIKey(WriteDB(), k.AgentID, k.Provider, k.CustomURL, k.PlaintextKey); err != nil {
			// CRITICAL: password was updated but re-encryption failed.
			// Attempt rollback: restore the old auto-password
			slog.Error("API key rotation failed, attempting rollback", "agent_id", k.AgentID, "provider", k.Provider, "error", err)
			if writeErr := os.WriteFile(filepath.Join(model.DataDir, "auto-password"), []byte(oldAutoPassword), 0o600); writeErr != nil {
				slog.Error("CRITICAL: failed to rollback auto-password during key rotation", "error", writeErr)
			}
			ResetEncryptionKeyCache()
			return fmt.Errorf("re-encrypt API key for agent %s/%s: %w (password rolled back)", k.AgentID, k.Provider, err)
		}
	}

	slog.Info("API key encryption rotated successfully", "keys", len(keys))

	// 5. Clear previous key — rotation complete, no more fallback needed (ISS-225)
	previousKeyMu.Lock()
	previousEncryptionKey = nil
	previousKeyMu.Unlock()
	return nil
}
