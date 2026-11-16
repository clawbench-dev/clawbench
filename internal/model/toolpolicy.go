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

// DefaultToolPolicyBudgets are the limits applied when a policy leaves a
// budget at zero. A built-in agent runs a model loop with no user watching the
// intermediate steps, so an unbounded loop is never the safe default.
const (
	DefaultMaxRounds      = 24
	DefaultMaxToolCalls   = 60
	DefaultBashTimeoutSec = 120
	// MaxBashTimeoutSec caps the per-command timeout a policy may request.
	MaxBashTimeoutSec = 3600
)

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

// ValidateToolPolicy rejects a policy this build cannot honour.
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
