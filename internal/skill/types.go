// Package skill implements a backend-agnostic skill discovery and aggregation
// framework.
//
// A "skill" is a directory containing a SKILL.md file with YAML frontmatter
// (name + description), the convention shared by CodeBuddy, Claude Code,
// Qoder, Codex and others. ClawBench scans several kinds of skill sources and
// makes every discovered skill visible to every agent, so a skill installed
// for one agent can be used by the others.
//
// Import rule: this package may import internal/model but MUST NOT import
// internal/ai or internal/ai/backends — internal/ai imports this package to
// turn skills into slash commands, so the reverse edge would be a cycle.
package skill

import (
	"strings"
	"sync"
)

// SourceKind classifies where a skill entry came from.
//
// The numeric values ARE the dedup priority (lower wins) — see
// Registry.InjectedFor. Do not reorder without updating the dedup logic and
// its tests.
type SourceKind int

const (
	// SourceOwnNative is the skill directory owned by the agent being served.
	SourceOwnNative SourceKind = 0
	// SourceUserDir is a directory the user configured explicitly
	// (config skills.dir).
	SourceUserDir SourceKind = 1
	// SourceGit is a skill repository cloned from a remote git URL
	// (config skills.repos).
	SourceGit SourceKind = 2
	// SourceOtherNative is another agent's native skill directory.
	SourceOtherNative SourceKind = 3
)

// String returns a stable identifier used by the HTTP API and tests.
func (k SourceKind) String() string {
	switch k {
	case SourceOwnNative:
		return "own"
	case SourceUserDir:
		return "user"
	case SourceGit:
		return "git"
	case SourceOtherNative:
		return "other"
	default:
		return "unknown"
	}
}

// Source describes where a scan came from.
type Source struct {
	Kind SourceKind
	// AgentID is the agent(s) whose spec declares this directory, for display
	// only — own-ness is decided per agent in InjectedFor, not from this field.
	// A directory read by several agents (the shared ".agents/skills") carries
	// them joined, because the directory is scanned once and its content does
	// not depend on who is asking.
	AgentID string
	// Label is a human-readable origin ("codebuddy native", "user dir",
	// "repo:team-skills") shown in the UI.
	Label string
	// Dir is the absolute directory that was scanned.
	Dir string
	// Shared marks the cross-tool shared directory (".agents/skills"). It is a
	// property of the DIRECTORY, not of the observer: the same skills are "own"
	// for a backend that reads that directory and "other" for one that does not,
	// but they are always generic/shared skills. Consumers use this to label
	// them by what they are rather than by which agent happened to declare it.
	Shared bool
}

// Key returns the registry map key for this source. It is stable across scans
// of the same directory so results can be replaced wholesale.
//
// Native sources are keyed by directory alone, NOT by agent: the same directory
// can be declared by several agents, and it must still be one entry (both to
// scan it once and to avoid listing its skills once per agent).
func (s Source) Key() string {
	switch s.Kind {
	case SourceOwnNative, SourceOtherNative:
		return "native:" + s.Dir
	default:
		return s.Kind.String() + ":" + s.Dir
	}
}

// Skill is one discovered skill.
type Skill struct {
	Name        string
	Description string
	// Path is the absolute path to the SKILL.md file. It is emitted in the
	// injected system-prompt table because an agent may not own the skill's
	// directory and needs the path to read it.
	Path string
	// NameMismatch is true when the frontmatter name disagrees with the skill's
	// directory name (the spec requires them to match). Such a skill is still
	// injected — the table carries an explicit Path, so it remains readable —
	// but it must NOT be advertised as a slash command: an agent resolves a
	// skill by directory, so the frontmatter name would be a dead entry.
	NameMismatch bool
	Source       Source
}

// canonicalName normalizes a skill name for cross-source identity comparison:
// lowercased with a single leading "/" stripped. Producers disagree on the
// slash prefix (CodeBuddy's ACP strips it, a frontmatter name may carry it),
// and comparing raw names would let one skill win twice.
func canonicalName(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
}

// Registry holds the aggregated scan results and the per-agent injection cache.
type Registry struct {
	mu sync.RWMutex
	// bySource maps Source.Key() to the skills found in that source.
	bySource map[string][]Skill
	// injected caches InjectedFor results per agent ID. Cleared by Invalidate.
	injected map[string][]Skill
}

var (
	globalRegistry     *Registry
	globalRegistryOnce sync.Once
)

// Global returns the process-wide registry singleton.
func Global() *Registry {
	globalRegistryOnce.Do(func() {
		globalRegistry = &Registry{
			bySource: make(map[string][]Skill),
			injected: make(map[string][]Skill),
		}
	})
	return globalRegistry
}
