package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Variables that a shell sets for its own bookkeeping. Exporting them into an
// agent process is pointless (they describe the throwaway shell, not the user's
// session) and a stale PWD/OLDPWD actively confuses tools that trust them.
var shellInternalEnvNames = map[string]bool{
	"_":              true,
	"SHLVL":          true,
	"PWD":            true,
	"OLDPWD":         true,
	"PS1":            true,
	"PS2":            true,
	"PS4":            true,
	"PROMPT_COMMAND": true,
}

// shellEnvMarker separates shell startup noise from the environment dump.
// Startup files are free to print banners to stdout; without a marker a banner
// and the first variable would land in the same record and the variable would
// be silently dropped.
const shellEnvMarker = "__CLAWBENCH_SHELL_ENV_MARKER_9c1f__"

// shellEnvTimeout bounds how long a user's startup files may take. Some
// .bashrc files run `nvm use`, `conda activate` or an `ssh-add` prompt; the
// query is best-effort, so a slow shell must not stall the caller.
var shellEnvTimeout = 5 * time.Second

// shellEnvTTL is how long a captured snapshot is reused. Every AI turn spawns a
// CLI process, so querying the shell per spawn would add its startup cost to
// every reply.
const shellEnvTTL = 5 * time.Minute

// shellEnvRetryDelay is how soon a failed background refresh is retried while a
// previous snapshot is still usable.
const shellEnvRetryDelay = 30 * time.Second

var (
	shellEnvMu         sync.Mutex
	shellEnvCache      []string
	shellEnvAt         time.Time
	shellEnvReady      bool
	shellEnvRefreshing bool
)

// LoginShellEnv returns the environment exported by the user's shell startup
// files (.bashrc / .zshrc / .profile / …) as "KEY=VALUE" entries.
//
// ClawBench is usually started from a context that never sourced those files —
// a systemd unit, a desktop launcher, or a non-interactive shell — so the
// server's own environment lacks anything the user exports there (AK/SK, API
// keys, …). Agent processes spawned with only os.Environ() then behave as if
// the user had never logged in (issue #619).
//
// The first call blocks until the shell answers; afterwards a stale snapshot is
// returned as-is and refreshed in the background, because the caller may hold a
// connection lock that user code must not stall.
//
// The returned slice is shared with the cache and must not be mutated.
func LoginShellEnv() []string {
	if IsWindows() {
		return nil
	}

	shellEnvMu.Lock()
	defer shellEnvMu.Unlock()

	if !shellEnvReady {
		shellEnvCache = readLoginShellEnv()
		shellEnvAt = time.Now()
		shellEnvReady = true
		return shellEnvCache
	}

	if time.Since(shellEnvAt) >= shellEnvTTL && !shellEnvRefreshing {
		shellEnvRefreshing = true
		go func() {
			defer func() {
				shellEnvMu.Lock()
				shellEnvRefreshing = false
				shellEnvMu.Unlock()
			}()

			env := readLoginShellEnv()

			shellEnvMu.Lock()
			defer shellEnvMu.Unlock()
			switch {
			case len(env) > 0:
				shellEnvCache = env
				shellEnvAt = time.Now()
			case len(shellEnvCache) > 0:
				// A transient failure (hung shell, unmounted home) must not throw
				// away a working snapshot; retry soon instead of a full TTL.
				shellEnvAt = time.Now().Add(-shellEnvTTL + shellEnvRetryDelay)
			default:
				shellEnvAt = time.Now()
			}
		}()
	}
	return shellEnvCache
}

// MergeLoginShellEnv appends the entries of LoginShellEnv that are not already
// present in env. Existing entries always win: ClawBench's own environment is
// the authority for everything it set deliberately, and this only fills gaps.
func MergeLoginShellEnv(env []string) []string {
	extra := LoginShellEnv()
	if len(extra) == 0 {
		return env
	}

	seen := make(map[string]bool, len(env)+len(extra))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			seen[kv[:i]] = true
		}
	}

	added := 0
	for _, kv := range extra {
		i := strings.IndexByte(kv, '=')
		if i <= 0 || seen[kv[:i]] {
			continue
		}
		env = append(env, kv)
		seen[kv[:i]] = true
		added++
	}
	if added > 0 {
		slog.Debug("inherited environment from user shell startup files", slog.Int("added", added))
	}
	return env
}

// resetLoginShellEnvCache clears the cached snapshot. Test-only.
func resetLoginShellEnvCache() {
	shellEnvMu.Lock()
	defer shellEnvMu.Unlock()
	shellEnvCache = nil
	shellEnvAt = time.Time{}
	shellEnvReady = false
}

func readLoginShellEnv() []string {
	shell := ResolveLoginShell()
	if shell == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellEnvTimeout)
	defer cancel()

	// Two passes, because no single invocation covers both families of files:
	// an interactive shell reads ~/.bashrc / ~/.zshrc, while a login shell reads
	// /etc/profile and ~/.profile / ~/.bash_profile / ~/.zprofile. bash
	// deliberately does NOT read ~/.bashrc in login mode. The interactive pass
	// runs first so that a slow profile chain (nvm, conda, a network home) cannot
	// starve the .bashrc read this whole mechanism exists for; the two share one
	// deadline, and on conflicts the interactive values win — the order a
	// terminal applies them.
	passEnv := make(map[string]map[string]string, 2)
	for _, mode := range []string{"-ic", "-ilc"} {
		if ctx.Err() != nil {
			break
		}
		out, err := runShellEnv(ctx, shell, mode, true)
		if err != nil || !bytes.Contains(out, []byte{0}) {
			// env(1) without -0 support: retry newline-separated. A timeout must
			// not be retried — the context is already expired and the second
			// attempt would only add another full wait.
			if ctx.Err() != nil {
				break
			}
			out, err = runShellEnv(ctx, shell, mode, false)
			if err != nil {
				slog.Warn("could not read user shell environment",
					slog.String("shell", shell), slog.String("mode", mode), slog.String("error", err.Error()))
				continue
			}
		}
		pass := make(map[string]string, 64)
		for _, kv := range parseShellEnv(out) {
			name, value, _ := strings.Cut(kv, "=")
			pass[name] = value
		}
		passEnv[mode] = pass
	}

	merged := make(map[string]string, 64)
	for _, mode := range []string{"-ilc", "-ic"} {
		for name, value := range passEnv[mode] {
			merged[name] = value
		}
	}

	if len(merged) == 0 {
		slog.Warn("no environment could be read from the user shell; agent processes keep the server environment",
			slog.String("shell", shell))
		return nil
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+merged[name])
	}

	slog.Info("read user shell environment", slog.String("shell", shell), slog.Int("vars", len(env)))
	return env
}

func runShellEnv(ctx context.Context, shell, mode string, nulSeparated bool) ([]byte, error) {
	cmdline := "command printf '%s\\n' " + shellEnvMarker + "; command env"
	if nulSeparated {
		cmdline += " -0"
	}

	cmd := exec.CommandContext(ctx, shell, mode, cmdline)
	// Startup files may print banners or prompts; keep that noise away from the
	// caller and never let a shell block waiting for input.
	cmd.Stdin = nil
	// A startup file that leaves a daemon behind would keep the stdout pipe
	// open after the shell exits; do not wait on it.
	cmd.WaitDelay = time.Second
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	// An interactive shell needs a TERM entry; a service-started server often
	// has none, and zsh then fails before running the startup files.
	if os.Getenv("TERM") == "" {
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Output already read stays valid when only the leftover pipe held Wait
		// open — the environment dump itself completed.
		if errors.Is(err, exec.ErrWaitDelay) && stdout.Len() > 0 {
			return stdout.Bytes(), nil
		}
		return nil, fmt.Errorf("%s %s %q: %w: %s", shell, mode, cmdline, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// parseShellEnv accepts the marked NUL- or newline-separated `env` output and
// keeps only well-formed, non-internal assignments. Anything else (prompt text,
// banners, shell error output) is dropped rather than guessed at.
func parseShellEnv(out []byte) []string {
	idx := bytes.Index(out, []byte(shellEnvMarker))
	if idx < 0 {
		return nil
	}
	body := bytes.TrimLeft(out[idx+len(shellEnvMarker):], "\r\n")

	sep := "\n"
	if bytes.IndexByte(body, 0) >= 0 {
		sep = "\x00"
	}

	env := make([]string, 0, 64)
	for _, entry := range strings.Split(string(body), sep) {
		entry = strings.TrimRight(entry, "\r")
		i := strings.IndexByte(entry, '=')
		if i <= 0 {
			continue
		}
		name := entry[:i]
		if !isEnvName(name) || shellInternalEnvNames[name] {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func isEnvName(name string) bool {
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return name != ""
}
