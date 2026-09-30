package skill

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/model"
)

// gitTimeout bounds a single clone/fetch. A hung remote must not wedge the
// sync worker.
const gitTimeout = 2 * time.Minute

// maxRepoBytes is a post-clone size cap. A skills repository is text; anything
// this large is either a mistake or hostile, and scanning it would waste time.
const maxRepoBytes = 200 << 20 // 200 MiB

// EnsureRepo makes sure the local checkout for repo exists and is up to date.
// It clones when the directory is missing and pulls otherwise. Returns the
// checkout directory.
//
// A failed pull leaves the previous checkout untouched, so the last good
// revision keeps serving.
func EnsureRepo(repo model.SkillRepo) (string, error) {
	dir := RepoDir(repo.Slug)
	if dir == "" {
		return "", fmt.Errorf("skill: repo has no usable slug (url=%q)", repo.URL)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, PullRepo(repo)
	}
	return dir, cloneRepo(repo, dir)
}

// cloneRepo clones into a sibling temp directory and renames it into place, so
// a failed or partial clone never appears as a valid checkout.
func cloneRepo(repo model.SkillRepo, dir string) error {
	root := filepath.Join(model.DataDir, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("skill: create skills root: %w", err)
	}
	tmp, err := os.MkdirTemp(root, ".tmp-clone-")
	if err != nil {
		return fmt.Errorf("skill: create temp clone dir: %w", err)
	}
	// Remove the placeholder so git can create the target itself.
	_ = os.Remove(tmp)
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := runGit("", "clone", "--depth", "1", authURL(repo), tmp); err != nil {
		return fmt.Errorf("skill: clone %s: %w", repo.URL, err)
	}
	if err := checkRepoSize(tmp); err != nil {
		return err
	}
	// Swap the new checkout into place. The old directory is moved aside first
	// and only deleted once the rename succeeded, so a failed rename cannot
	// destroy a working checkout.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("skill: prepare repo parent: %w", err)
	}
	backup := dir + ".old"
	_ = os.RemoveAll(backup)
	hadPrevious := false
	if _, statErr := os.Stat(dir); statErr == nil {
		if err := os.Rename(dir, backup); err != nil {
			return fmt.Errorf("skill: move aside old checkout: %w", err)
		}
		hadPrevious = true
	}
	if err := os.Rename(tmp, dir); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, dir) // restore
		}
		return fmt.Errorf("skill: install clone: %w", err)
	}
	_ = os.RemoveAll(backup)
	slog.Info("skill: cloned repo", "url", repo.URL, "dir", dir)
	return nil
}

// PullRepo fetches the latest revision and hard-resets to it. The reset only
// runs after a successful fetch, so a network failure cannot corrupt the
// working tree.
func PullRepo(repo model.SkillRepo) error {
	dir := RepoDir(repo.Slug)
	if dir == "" {
		return fmt.Errorf("skill: repo has no usable slug (url=%q)", repo.URL)
	}
	if err := runGit(dir, "fetch", "--depth", "1", authURL(repo), "HEAD"); err != nil {
		return fmt.Errorf("skill: fetch %s: %w", repo.URL, err)
	}
	if err := runGit(dir, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("skill: reset %s: %w", repo.URL, err)
	}
	if err := checkRepoSize(dir); err != nil {
		return err
	}
	return nil
}

// authURL injects a token into an http(s) remote URL. Non-http URLs (ssh://,
// git@host:path) are returned unchanged and rely on the host's own SSH agent.
//
// The token is never written to .git/config — it lives only in this argv.
func authURL(repo model.SkillRepo) string {
	raw := repo.URL
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	token := repo.Token
	if token == "" {
		if u, err := url.Parse(raw); err == nil {
			token = model.ConfigInstance.ForgeToken(u.Host)
		}
	}
	if token == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// Build via url.URL so an "@" or ":" inside the token cannot break parsing.
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

// runGit executes a git command, optionally inside dir.
func runGit(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Never block on an interactive credential prompt — fail instead.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git %s timed out after %s", args[0], gitTimeout)
		}
		return fmt.Errorf("git %s: %w: %s", args[0], err, truncateOutput(string(out)))
	}
	return nil
}

// truncateOutput bounds captured git stderr so a chatty failure does not fill
// the log line or the API response.
func truncateOutput(s string) string {
	s = strings.TrimSpace(s)
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// checkRepoSize rejects a checkout that exceeds maxRepoBytes.
//
// A walk error on an individual entry is skipped: the size check is a safety
// net, and an unreadable file should not fail an otherwise good sync. A walk
// error from the callback itself (the size cap) is reported.
func checkRepoSize(dir string) error {
	var total int64
	walkErr := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Debug("skill sync: skipping unreadable entry during size check", "error", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}
		if total > maxRepoBytes {
			return errRepoTooLarge
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("skill: repository exceeds %d MiB: %w", maxRepoBytes>>20, walkErr)
	}
	return nil
}

var errRepoTooLarge = fmt.Errorf("repository too large")
