package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"clawbench/internal/model"
)

// upgradeSkipFileName is the record of the version the user asked not to be
// prompted about again, kept under the data directory.
//
// Why on the server instead of the browser: the "skip this version" choice used
// to live only in the client's localStorage, which is scoped to one origin on
// one device. The same person opening the app from a second device (or from the
// LAN address instead of the public hostname — a different origin, hence a
// different localStorage) was asked again, and a WebView that loses its storage
// on teardown asked on every launch. The data directory is the one place that
// outlives all of that, and it is already where this server keeps its other
// small pieces of durable state (cookie-token, self-path).
const upgradeSkipFileName = "upgrade-skip"

// maxSkippedVersionLen bounds what we accept. A version string is short; an
// unbounded one would let a caller stuff arbitrary content into the data
// directory through an authenticated endpoint.
const maxSkippedVersionLen = 64

// ErrSkippedVersionInvalid is returned when the version to record is not a
// plausible version string (too long, or containing control characters).
var ErrSkippedVersionInvalid = errors.New("invalid version to skip")

// normalizeSkippedVersion trims surrounding space and a single leading "v".
// The comparison that matters happens in the client, where the prompt shows the
// registry's own spelling; normalizing here keeps "v1.2.3" and "1.2.3" from
// becoming two different records.
func normalizeSkippedVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// ReadSkippedUpgradeVersion returns the version the user chose to skip, or ""
// when nothing is recorded. A read failure is reported as "nothing skipped":
// the worst case is one extra prompt, which is better than failing the check.
func ReadSkippedUpgradeVersion() string {
	if model.DataDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(model.DataDir, upgradeSkipFileName))
	if err != nil {
		return ""
	}
	return normalizeSkippedVersion(string(data))
}

// WriteSkippedUpgradeVersion records version as skipped and returns the stored
// (normalized) value. An empty version clears the record.
//
// Unlike the best-effort records next to it, a failure here is returned to the
// caller: the whole point of the endpoint is durability, so a client that gets
// an error keeps relying on its own localStorage instead of believing the
// choice was remembered.
func WriteSkippedUpgradeVersion(version string) (string, error) {
	normalized := normalizeSkippedVersion(version)
	if normalized == "" {
		return "", clearSkippedUpgradeVersion()
	}
	if len(normalized) > maxSkippedVersionLen {
		return "", ErrSkippedVersionInvalid
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", ErrSkippedVersionInvalid
		}
	}
	if model.DataDir == "" {
		return "", errors.New("data directory is not configured")
	}
	if err := os.MkdirAll(model.DataDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create data dir: %w", err)
	}
	path := filepath.Join(model.DataDir, upgradeSkipFileName)
	if err := os.WriteFile(path, []byte(normalized), 0o600); err != nil {
		return "", fmt.Errorf("failed to write upgrade skip: %w", err)
	}
	return normalized, nil
}

// clearSkippedUpgradeVersion removes the record. A missing file is not an
// error — "nothing is skipped" is the state the caller asked for.
func clearSkippedUpgradeVersion() error {
	if model.DataDir == "" {
		return nil
	}
	err := os.Remove(filepath.Join(model.DataDir, upgradeSkipFileName))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear upgrade skip: %w", err)
	}
	return nil
}
