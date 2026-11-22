package ai

import "sync"

// Orphan detection fallback: the command-line shapes that identify an ACP agent
// process.
//
// CleanupOrphans normally recognizes a ClawBench child by the
// CLAWBENCH_CHILD=1 environment marker it injects at spawn. That marker is
// enough for every child we started — but not for a child whose environment we
// cannot read, and not for one spawned by a previous build that predates the
// marker. For those, the command line is the only evidence left.
//
// The shapes are per-backend facts, so each backend declares its own in its
// BackendPlugin.OrphanCmdline instead of being listed here: this file used to
// hold a three-row table naming codebuddy, claude and codex, which meant a new
// backend's leftovers were never cleaned up (a silently-skipped cleanup, not a
// visible failure) and that adding one required editing a package it does not
// belong to.

// OrphanCmdlinePattern identifies one backend's ACP process on the command
// line. Both substrings must be present for a match: the binary alone is not
// enough (a shell running `claude --version` is not our child), and the argument
// alone is not either (many tools take `--acp`).
type OrphanCmdlinePattern struct {
	// Binary is a substring of the executable name or argv[0].
	Binary string
	// Arg is a substring of the argument list.
	Arg string
}

// Registered orphan patterns, populated by internal/ai/backends at init.
//
// A registry rather than a read of the backend plugins, for the same reason the
// other per-backend maps exist: CleanupOrphans runs at startup from `main`
// before the web server is up, and it must not depend on the plugin packages
// having been imported by whoever calls it.
var (
	orphanPatternsMu sync.RWMutex
	orphanPatterns   []OrphanCmdlinePattern
)

// RegisterOrphanCmdline records a backend's orphan-recognition pattern. Called
// by the backend registry for every plugin that declares one. Idempotent per
// (binary, arg) pair: re-registering the same pattern is a no-op rather than a
// duplicate.
func RegisterOrphanCmdline(binary, arg string) {
	if binary == "" || arg == "" {
		return
	}
	orphanPatternsMu.Lock()
	defer orphanPatternsMu.Unlock()
	for _, p := range orphanPatterns {
		if p.Binary == binary && p.Arg == arg {
			return
		}
	}
	orphanPatterns = append(orphanPatterns, OrphanCmdlinePattern{Binary: binary, Arg: arg})
}

// orphanCmdlinePatterns returns a snapshot for matching. A copy, so the caller
// never holds the lock while walking it.
func orphanCmdlinePatterns() []OrphanCmdlinePattern {
	orphanPatternsMu.RLock()
	defer orphanPatternsMu.RUnlock()
	if len(orphanPatterns) == 0 {
		return nil
	}
	out := make([]OrphanCmdlinePattern, len(orphanPatterns))
	copy(out, orphanPatterns)
	return out
}
