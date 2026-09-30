package model

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// maxSkillSlugLen bounds the human-readable part of a derived slug. The hash
// suffix is appended after truncation so it always survives.
const maxSkillSlugLen = 64

// SkillSlug derives a filesystem-safe, collision-resistant directory name for a
// remote skill repository URL.
//
// The readable prefix is the URL's last path segment (the repository name) with
// every character outside [a-z0-9._-] replaced by "-". A short hash of the full
// URL is appended so two different repositories whose names sanitize to the
// same prefix (e.g. "team/skills" and "org/skills") still get distinct
// directories. The result never contains a path separator or "..", so it is
// safe to join under the skills root.
func SkillSlug(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(url), "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	name := trimmed
	if i := strings.LastIndexAny(name, "/:"); i >= 0 {
		name = name[i+1:]
	}

	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	prefix := strings.Trim(b.String(), "-.")
	if len(prefix) > maxSkillSlugLen {
		prefix = prefix[:maxSkillSlugLen]
	}
	if prefix == "" {
		prefix = "repo"
	}

	sum := sha256.Sum256([]byte(trimmed))
	return prefix + "-" + hex.EncodeToString(sum[:4])
}

// SkillsConfig configures the cross-agent skill discovery framework.
//
// Skills are directories containing SKILL.md. ClawBench scans the native
// directory of every installed agent, plus the user's own directory and any
// cloned git repositories, and makes all of them visible to every agent (see
// internal/skill). Which directory a skill came from only affects dedup
// priority, not visibility.
type SkillsConfig struct {
	// Enabled is the master switch. When false, no skills section is injected
	// into any system prompt (scanning and the UI listing still work).
	Enabled bool `yaml:"enabled"`
	// Dirs are the user's own skill directories, scanned in order. Each is an
	// absolute path; a skill found in more than one is deduplicated by name
	// (all of them share SourceUserDir priority, so the first in this list
	// wins).
	//
	// An empty list resolves to DefaultSkillsDir(). The legacy single-directory
	// key (`dir`) is migrated into this list on load — see main.go.
	Dirs []string `yaml:"dirs"`
	// LegacyDir is the pre-multi-directory key, kept ONLY so a config written
	// by an older build still parses. Never read at runtime: main.go moves a
	// non-empty value into Dirs on load, and the next save drops the key.
	LegacyDir string `yaml:"dir,omitempty"`
	// RefreshHours is the periodic git pull interval. 0 disables periodic
	// refresh (the startup pull still runs).
	RefreshHours int `yaml:"refresh_hours"`
	// Repos are the remote git repositories to clone and scan.
	Repos []SkillRepo `yaml:"repos"`
	// LastError / LastSyncAt are server-owned sync state, mirroring
	// BingWallpaperConfig — a failed sync keeps the previous checkout and
	// records why.
	LastError  string `yaml:"last_error"`
	LastSyncAt int64  `yaml:"last_sync_at"`
}

// SkillRepo is one remote git skill source.
type SkillRepo struct {
	URL string `yaml:"url"`
	// Token authenticates against a private repository over http(s). It is
	// optional: when empty, the per-host forge credential is used instead.
	// Never rendered back to the client.
	Token string `yaml:"token,omitempty"`
	// Slug is the local checkout directory name under {DataDir}/skills.
	// Derived from URL when the user did not set one.
	Slug string `yaml:"slug"`
	// LastError records the most recent sync failure for this repo only.
	LastError string `yaml:"last_error,omitempty"`
}

// DefaultSkillsDir returns the default user skill directory:
// <DataDir>/skills-user. It is deliberately distinct from <DataDir>/skills,
// which holds cloned repositories (managed by the server, not the user).
// Returns "" when DataDir is unset.
func DefaultSkillsDir() string {
	if DataDir == "" {
		return ""
	}
	return filepath.Join(DataDir, "skills-user")
}

// ResolveSkillsDirs returns the user's skill directories, falling back to the
// default when none are configured. Call this at scan time rather than reading
// Skills.Dirs directly, so an explicitly-cleared list resolves to the default.
//
// Blank entries are dropped: an empty string would otherwise resolve to the
// scan root itself and turn a whole project tree into skills.
func (c *Config) ResolveSkillsDirs() []string {
	var out []string
	for _, d := range c.Skills.Dirs {
		if d != "" {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		if def := DefaultSkillsDir(); def != "" {
			return []string{def}
		}
	}
	return out
}
