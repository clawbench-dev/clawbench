package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tool capabilities granted to a built-in agent.
//
// Capabilities are deliberately coarser than tool names. A per-tool allowlist
// ("enable read, disable bash") reads as a safety boundary but is not one: bash
// alone can write files (`cat > f`, `sed -i`, `git checkout`), so an agent with
// bash but without fs.write is not read-only — it is read-only by promise. The
// capability set states the truth, and the tool list is derived from it.
const (
	// CapFSRead allows reading files inside the working directory.
	CapFSRead = "fs.read"
	// CapFSWrite allows creating and modifying files inside the working
	// directory. Implied by CapExec, which can do the same thing.
	CapFSWrite = "fs.write"
	// CapExec allows spawning a shell. This is the widest grant: it implies
	// CapFSWrite and CapFSRead, and nothing short of an OS sandbox narrows it
	// afterwards.
	CapExec = "exec"
)

// toolCapabilityOrder is the canonical display/serialization order: least
// privilege first, so a policy's JSON reads as an increasing grant.
var toolCapabilityOrder = []string{CapFSRead, CapFSWrite, CapExec}

// Modes the built-in agent offers in the session picker.
//
// A mode NARROWS the agent's tool grant; it never widens it. The grant in the
// agent settings is the ceiling, and a session can ask for less (a read-only
// pass over a repository) but not for more. Making the mode authoritative
// instead would turn a single click in the chat drawer into a permission
// escalation past the switch the user deliberately set.
const (
	// ModeRead allows only fs.read.
	ModeRead = "read"
	// ModeWrite allows fs.read + fs.write.
	ModeWrite = "write"
	// ModeFull applies no extra narrowing — the agent's own grant is the limit.
	ModeFull = "full"
)

// ToolMode is one selectable mode: the id stored/compared, and the label the
// client renders. The label is English because every other agent's mode names
// come from the agent itself and are English too; a Chinese-only label here
// would be the odd one out.
type ToolMode struct {
	ID   string
	Name string
}

var builtinModes = []ToolMode{
	{ID: ModeRead, Name: "Read only"},
	{ID: ModeWrite, Name: "Read & write"},
	{ID: ModeFull, Name: "Full access"},
}

// BuiltinModes returns the mode list for the built-in agent, least privilege
// first. The slice is a copy: callers register it into the AI capability
// registry, and a shared backing array would let one caller's append corrupt
// another's view.
func BuiltinModes() []ToolMode {
	out := make([]ToolMode, len(builtinModes))
	copy(out, builtinModes)
	return out
}

// ToolPolicy is an agent's tool grant.
//
// The zero value grants NOTHING: a freshly created agent can chat but cannot
// touch the filesystem or spawn a process. That direction is deliberate — a
// forgotten policy must fail closed, and every existing backend defaults to
// full access, so an agent that quietly inherited "everything" would be the
// one failure mode users never notice.
type ToolPolicy struct {
	// Allow lists the granted capabilities (see CapFSRead/CapFSWrite/CapExec).
	Allow []string `json:"allow"`
	// MaxRounds bounds model round-trips per turn (0 = DefaultMaxRounds).
	MaxRounds int `json:"maxRounds,omitempty"`
	// MaxToolCalls bounds tool invocations per turn (0 = DefaultMaxToolCalls).
	MaxToolCalls int `json:"maxToolCalls,omitempty"`
	// BashTimeoutSec bounds one shell command (0 = DefaultBashTimeoutSec).
	BashTimeoutSec int `json:"bashTimeoutSec,omitempty"`
	// RestrictToWorkDir confines the file tools to the working directory.
	//
	// It is phrased as the RESTRICTION rather than as the permission so the
	// zero value means "not confined", which is the behavior this field was
	// added with (the switch is on for every agent that never touched it).
	// Storing the permission instead would make the zero value "confined" and
	// silently change what existing agents can do on upgrade.
	//
	// false (default) — the file tools may reach anywhere the process can:
	// absolute paths, `..` outside the project, symlinks pointing out. That is
	// what the user asked for by default: the project directory is a starting
	// point, not a fence, and an agent told "no" spends rounds hunting for a
	// way around it.
	// true — every path must resolve inside the working directory; anything
	// else is refused with an explanation. This is the sandbox: use it when
	// the agent runs on a machine with files the user does not want read.
	//
	// Exec is not affected either way — a shell can read the whole disk
	// whatever this says, which is why the setting is described in the UI as
	// covering the file tools only.
	RestrictToWorkDir bool `json:"restrictToWorkDir,omitempty"`
}

// ForMode narrows the grant to what the mode allows.
//
// The empty string means "no mode chosen" and leaves the policy untouched, so a
// turn from a caller that never had a mode (a background task, an older client)
// runs with exactly the grant the agent was configured with.
//
// An UNKNOWN non-empty mode narrows to read-only rather than passing through:
// mode ids are backend-specific, and a leftover ACP mode ("code", "ask") must
// not silently mean "everything". Failing closed here is the only direction
// that cannot hand out more access than the user asked for.
func (p ToolPolicy) ForMode(mode string) ToolPolicy {
	allowed := map[string]bool{}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModeFull:
		return p
	case ModeRead:
		allowed[CapFSRead] = true
	case ModeWrite:
		allowed[CapFSRead] = true
		allowed[CapFSWrite] = true
	default:
		allowed[CapFSRead] = true
	}

	out := p
	out.Allow = []string{}
	for _, c := range toolCapabilityOrder {
		// Intersection: the mode can only remove capabilities, never add one
		// the agent was not granted.
		if allowed[c] && p.Has(c) {
			out.Allow = append(out.Allow, c)
		}
	}
	return out.Normalized()
}

// DefaultMode reports the narrowest mode that still expresses the grant.
//
// Used as the selected mode for a session that has never picked one, so the
// drawer highlights the grant the agent will actually run instead of an empty
// selection. An empty grant reports ModeFull: every mode is equivalent there,
// and "full" is the one that promises no extra narrowing rather than
// advertising a read permission the agent does not have.
func (p ToolPolicy) DefaultMode() string {
	switch {
	case p.Has(CapExec):
		return ModeFull
	case p.Has(CapFSWrite):
		return ModeWrite
	case p.Has(CapFSRead):
		return ModeRead
	default:
		return ModeFull
	}
}

// DefaultToolPolicyBudgets are the limits applied when a policy leaves a
// budget at zero. A built-in agent runs a model loop with no user watching the
// intermediate steps, so an unbounded loop is never the safe default.
//
// The numbers are sized for real repository work, not for a demo. A "find the
// settings panel and add an option" task measured 24+ model rounds and 40+
// tool calls before the first edit — the old 24/60 pair cut such a turn off
// mid-exploration, and the agent then ended with no answer at all. Both are
// per-turn and configurable per agent (settings → tool permissions).
const (
	DefaultMaxRounds      = 100
	DefaultMaxToolCalls   = 300
	DefaultBashTimeoutSec = 120
	// MaxBashTimeoutSec caps the per-command timeout a policy may request.
	MaxBashTimeoutSec = 3600
)

// ConfinesToWorkDir reports whether file paths are limited to the working
// directory. The negation lives here so callers never have to remember which
// way the field points.
func (p ToolPolicy) ConfinesToWorkDir() bool {
	return p.RestrictToWorkDir
}

// Normalized returns a copy with unknown capabilities dropped, the implied ones
// added, and the order made canonical.
//
// Implication is closed here rather than validated-and-rejected because the two
// shapes it fixes ("exec without write", "write without read") are not typos a
// user can be talked out of — they are what a UI produces when someone ticks
// "run commands" and leaves "write files" off. Rejecting the request would make
// the switch feel broken; silently keeping it would make the UI lie about what
// the agent can do. Closing the set means the stored policy is always the
// truth, and the frontend renders the closure so the user sees what they got.
//
// Allow is always a non-nil slice: it is serialized to JSON and read by the
// client, where a null would become `undefined` and break `new Set(allow)`.
func (p ToolPolicy) Normalized() ToolPolicy {
	have := make(map[string]bool, len(p.Allow))
	for _, c := range p.Allow {
		have[strings.TrimSpace(c)] = true
	}
	if have[CapExec] {
		have[CapFSWrite] = true
	}
	if have[CapFSWrite] {
		have[CapFSRead] = true
	}

	out := ToolPolicy{
		Allow:          []string{},
		MaxRounds:      p.MaxRounds,
		MaxToolCalls:   p.MaxToolCalls,
		BashTimeoutSec: p.BashTimeoutSec,
		// Carried over, not defaulted: Normalized runs on every save and every
		// load, so dropping it here would silently reset the switch to "off"
		// the first time a user touched any other field on this panel.
		RestrictToWorkDir: p.RestrictToWorkDir,
	}
	for _, c := range toolCapabilityOrder {
		if have[c] {
			out.Allow = append(out.Allow, c)
		}
	}
	if out.MaxRounds < 0 {
		out.MaxRounds = 0
	}
	if out.MaxToolCalls < 0 {
		out.MaxToolCalls = 0
	}
	if out.BashTimeoutSec < 0 {
		out.BashTimeoutSec = 0
	}
	if out.BashTimeoutSec > MaxBashTimeoutSec {
		out.BashTimeoutSec = MaxBashTimeoutSec
	}
	return out
}

// Has reports whether the policy grants capability c.
func (p ToolPolicy) Has(c string) bool {
	for _, a := range p.Allow {
		if a == c {
			return true
		}
	}
	return false
}

// Rounds returns the effective round budget.
func (p ToolPolicy) Rounds() int {
	if p.MaxRounds > 0 {
		return p.MaxRounds
	}
	return DefaultMaxRounds
}

// ToolCalls returns the effective tool-call budget.
func (p ToolPolicy) ToolCalls() int {
	if p.MaxToolCalls > 0 {
		return p.MaxToolCalls
	}
	return DefaultMaxToolCalls
}

// CommandTimeout returns the effective per-command timeout.
func (p ToolPolicy) CommandTimeout() int {
	if p.BashTimeoutSec > 0 {
		return p.BashTimeoutSec
	}
	return DefaultBashTimeoutSec
}

// ValidateToolPolicy rejects a policy this build cannot honor.
//
// Only the budget range is enforced: capability names are closed by
// Normalized() instead, because an unknown name there means "a UI offered a
// switch this build does not have", which must not brick the agent.
func ValidateToolPolicy(p ToolPolicy) error {
	if p.MaxRounds < 0 {
		return fmt.Errorf("tool policy: maxRounds must not be negative")
	}
	if p.MaxToolCalls < 0 {
		return fmt.Errorf("tool policy: maxToolCalls must not be negative")
	}
	if p.BashTimeoutSec < 0 || p.BashTimeoutSec > MaxBashTimeoutSec {
		return fmt.Errorf("tool policy: bashTimeoutSec must be between 0 and %d", MaxBashTimeoutSec)
	}
	return nil
}

// EncodeToolPolicy renders a policy for storage, normalized so a row never
// stores a shape Normalized() would have to repair on read.
func EncodeToolPolicy(p ToolPolicy) (string, error) {
	p = p.Normalized()
	if p.Allow == nil {
		p.Allow = []string{}
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode tool policy: %w", err)
	}
	return string(encoded), nil
}

// DecodeToolPolicy parses a stored policy. An unreadable value degrades to the
// zero policy (no grants) rather than failing the load: refusing to start over
// one bad row is worse than an agent that needs its permissions re-ticked.
func DecodeToolPolicy(raw string) ToolPolicy {
	if raw == "" || raw == "null" {
		// Normalized (not the bare zero value) so Allow is an empty slice, not
		// nil: this value is serialized straight to the client, where null
		// becomes undefined and breaks `new Set(allow)`.
		return ToolPolicy{}.Normalized()
	}
	var p ToolPolicy
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ToolPolicy{}.Normalized()
	}
	return p.Normalized()
}
