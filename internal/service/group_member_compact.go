package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// group_member_compact.go lets the user compact ONE group member's context.
//
// Why this is not a normal turn: a group turn writes to the GROUP timeline
// (TimelineSessionID), so running "/compact" through runTurn would persist a
// user bubble ("/compact") and an assistant reply on the group timeline, where
// every member would then read them as ordinary discussion. Compaction is a
// command addressed to one agent's own context — it must produce no
// CONVERSATIONAL output on the timeline.
//
// It DOES write two role='system' rows (started / finished), because the
// compaction silently rewrites what that member remembers: the discussion needs
// a marker explaining why its answers may suddenly lose earlier detail. System
// rows are the established channel for exactly this (membership changes use
// them too) — they are rendered as a centered line, never as speech, and they
// ARE injected to members as "[系统] …" so nobody mistakes the reset for the
// member contradicting itself.
//
// So this path drives the ACP backend directly: build the member's request,
// send "/compact", consume the events (persisting the context-state patches
// onto the MEMBER row, which is where a member's usage lives), and never emit
// anything to the WS hub.
//
// ACP only: the command is an ACP slash command. A CLI-transport agent has no
// command channel — sending the text would reach the model as prose.

// Errors the handler maps to HTTP statuses.
var (
	// ErrGroupRunning is returned when the group has a turn in flight. A member
	// connection is busy during its own turn, and compaction mid-turn would
	// interleave two prompts on one ACP session.
	ErrGroupRunning = errors.New("group turn is running")
	// ErrGroupMemberNotFound is returned when memberID is not a member of
	// groupID (wrong group, or a non-member row id). A DISABLED member still
	// qualifies: muting only takes it off the schedule, its context window is
	// exactly what the user may want to compact while it is disabled.
	ErrGroupMemberNotFound = errors.New("group member not found")
	// ErrCompactNeedsACP is returned for a CLI-transport member, which has no
	// slash-command channel.
	ErrCompactNeedsACP = errors.New("member does not use ACP transport")
	// ErrMemberCompacting is returned when the same member is already being
	// compacted (double click / two clients).
	ErrMemberCompacting = errors.New("member compaction already in progress")
)

// compactTimeout bounds one compaction. Compaction is a model round-trip over
// the whole context; a large window can take a minute. The cap exists so a
// wedged agent cannot hold the HTTP request (and the in-flight slot) forever.
const compactTimeout = 3 * time.Minute

// compactingMembers maps a member row id to the group that owns it, for every
// compaction currently in flight.
//
// A plain map (not the session runner registry): a member row must never appear
// in GetRunningSessionIDs, which feeds the user-visible session list (decision
// #73). The ACP idle sweep is kept away separately, via markGroupMembersActive.
var (
	compactingMu      sync.Mutex
	compactingMembers = map[string]string{}
)

// IsGroupMemberCompacting reports whether memberID has a compaction in flight.
//
// The chat handler consults this before claiming a group run: compaction drives
// the member's ACP connection directly, so a group turn that starts meanwhile
// would send a SECOND prompt on the same ACP session. Three things break at
// once: the agent answers "Session is busy" (so the user's message fails with
// an error bubble), RegisterSession OVERWRITES the compaction's stream channel
// (its events are silently dropped), and promptCancel is overwritten too —
// leaving one of the two prompts uncancellable when the process dies. Sending
// during a compaction therefore has to queue, exactly like sending during a
// running group turn.
func IsGroupMemberCompacting(memberID string) bool {
	compactingMu.Lock()
	defer compactingMu.Unlock()
	_, busy := compactingMembers[memberID]
	return busy
}

// GroupHasCompactingMember reports whether ANY member of the group is
// compacting. Read from the in-flight map, so it costs no query on the send
// path.
func GroupHasCompactingMember(groupID string) bool {
	if groupID == "" {
		return false
	}
	compactingMu.Lock()
	defer compactingMu.Unlock()
	for _, g := range compactingMembers {
		if g == groupID {
			return true
		}
	}
	return false
}

// CompactGroupMember asks one member's agent to compact its own context and
// returns the member's usage AFTER the compaction (nil when the agent reported
// none).
//
// Guards, in order:
//   - the group must be idle (a member connection is busy during a group turn);
//   - the member must be a member of that group (a DISABLED one qualifies: it is
//     disabled, but its context window is exactly what may need compacting);
//   - the member must run over ACP (a CLI agent has no command channel);
//   - the member must not already be compacting.
func CompactGroupMember(ctx context.Context, groupID, memberID string) (*ai.UsageState, error) {
	member, err := memberRowInGroup(groupID, memberID)
	if err != nil {
		return nil, err
	}
	agentID := member.AgentID
	transport := GetSessionTransport(memberID)
	if !resolveIsACP(agentID, transport) {
		return nil, ErrCompactNeedsACP
	}
	if !beginCompaction(groupID, memberID) {
		return nil, ErrMemberCompacting
	}
	defer func() {
		endCompaction(memberID)
		// The send path queues while a compaction is in flight (see
		// IsGroupMemberCompacting), so anything the user typed meanwhile is
		// sitting in queued_messages. Wake the consumer now that the member's
		// connection is free — otherwise those messages wait for the reaper's
		// next sweep. Done AFTER releasing the slot so the woken runner's own
		// claim does not race the flag.
		wakeQueuedAfterCompaction(groupID)
	}()

	// Protect the connection from the ACP idle sweep for the duration, using the
	// same registry a group turn uses (member rows are deliberately absent from
	// the session runner registry).
	markGroupMembersActive([]string{memberID})
	defer clearGroupMembersActive([]string{memberID})

	projectPath := GetSessionProjectPathAnyPath(groupID)
	// Capture the window BEFORE the compaction so the finished row can report the
	// real effect ("32% → 3%"). Without a before/after pair the marker says a
	// reset happened but not whether it helped.
	beforeUsage := currentMemberUsage(memberID)

	// Tell the group the compaction is happening. Written BEFORE the agent round
	// trip: that takes tens of seconds, and a silent gap is exactly what makes a
	// later context reset look like the member losing its thread.
	writeCompactionSystemMessage(projectPath, groupID, member.Name, member.AgentID, "GroupMemberCompactionStarted", nil)

	runErr, err := runMemberCompact(ctx, groupID, memberID, agentID, transport)
	if err != nil {
		// Setup failed (backend create / stream start): nothing was compacted.
		// Close the pair so the timeline does not keep claiming a compaction that
		// never happened.
		writeCompactionSystemMessage(projectPath, groupID, member.Name, member.AgentID, "GroupMemberCompactionFailed", nil)
		return nil, err
	}
	if runErr != nil {
		writeCompactionSystemMessage(projectPath, groupID, member.Name, member.AgentID, "GroupMemberCompactionFailed", nil)
		return nil, runErr
	}
	// Flag unconditionally as well: the user asked for a compaction, and several
	// agents never announce it on the wire (mirrors handler/chat.go's
	// MarkSessionCompacted for a user-issued /compact).
	MarkSessionCompacted(memberID)

	// Leave the connection reclaimable exactly like a completed chat turn does,
	// instead of pinning it until the process exits.
	ai.GetACPConnManager().MarkIdle(memberID)

	afterUsage := currentMemberUsage(memberID)
	writeCompactionSystemMessage(projectPath, groupID, member.Name, member.AgentID, "GroupMemberCompactionDone",
		map[string]any{
			"Before": usagePercentLabel(beforeUsage),
			"After":  usagePercentLabel(afterUsage),
		})

	return afterUsage, nil
}

// usagePercentLabel renders a usage snapshot as "32%", or "—" when the agent
// never reported a window. Used in the compaction system row, where a missing
// value must read as "unknown" rather than "0%".
func usagePercentLabel(usage *ai.UsageState) string {
	if usage == nil || usage.Size <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", int(math.Round(float64(usage.Used)/float64(usage.Size)*100)))
}

// writeCompactionSystemMessage appends one role='system' row about a member's
// context compaction to the GROUP timeline.
//
// Same channel as the membership rows (writeGroupSystemMessage): centered line
// in the UI, persisted once, broadcast after commit, and injected to members as
// "[系统] …" on their next turn. That last part is the point — the member's
// memory really did shrink, and the discussion needs to know why its earlier
// detail stopped being available.
//
// Localized at WRITE time (model.Language), matching every other group system
// row: the text is persisted and injected, so a key would reach members as an
// unreadable identifier.
func writeCompactionSystemMessage(projectPath, groupID, memberName, agentID, messageID string, extra map[string]any) {
	data := map[string]any{"Name": memberName + memberSpecialtySuffix(agentID)}
	for k, v := range extra {
		data[k] = v
	}
	writeGroupSystemMessage(projectPath, groupID,
		i18n.T(i18n.LocalizerForLocale(model.Language), messageID, data))
}

// wakeQueuedAfterCompaction starts the consumer for messages that were queued
// while a member was compacting.
//
// It does NOT pre-check the queue: the send path queues FIRST and only then
// releases/never takes the runner claim, so a snapshot taken here can legitimately
// see an empty queue while a row is about to land (the two are separate
// statements, not one transaction). With the pre-check that race left the
// message stranded until the reaper's next sweep — up to ~30s of a queue bar
// that does not move. EnsureConsumer already handles "nothing to run" (it
// dequeues, finds no row and releases the claim), so the emptiness check was
// both redundant and harmful.
func wakeQueuedAfterCompaction(groupID string) {
	if groupID == "" {
		return
	}
	EnsureConsumer(groupID)
}

// memberRowInGroup resolves memberID within groupID, rejecting an empty id and a
// member of another group. A disabled member is accepted on purpose: it is still a
// member with its own context window. The group must also be idle: a group turn
// owns every member connection, so a second prompt on one of them would
// interleave with the running turn.
func memberRowInGroup(groupID, memberID string) (*GroupMember, error) {
	if groupID == "" || memberID == "" {
		return nil, ErrGroupMemberNotFound
	}
	if IsSessionRunning(groupID) {
		return nil, ErrGroupRunning
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].ID != memberID {
			continue
		}
		// The member row IS the agent binding: its agent_id is the real agent
		// (ListGroupMembers reads it from that row), so use it directly rather
		// than ResolveAgentID — which falls back to the DEFAULT agent for an
		// empty id and would compact the wrong agent's context.
		if members[i].AgentID == "" {
			return nil, ErrGroupMemberNotFound
		}
		return &members[i], nil
	}
	return nil, ErrGroupMemberNotFound
}

// beginCompaction claims the member's in-flight slot, recording its group so
// the send path can tell whether THIS group is blocked. Returns false when a
// compaction is already running for the member (double click / two clients).
func beginCompaction(groupID, memberID string) bool {
	compactingMu.Lock()
	defer compactingMu.Unlock()
	if _, busy := compactingMembers[memberID]; busy {
		return false
	}
	compactingMembers[memberID] = groupID
	return true
}

func endCompaction(memberID string) {
	compactingMu.Lock()
	delete(compactingMembers, memberID)
	compactingMu.Unlock()
}

// newBackendForAgent is a seam for tests: the real call would spawn an ACP
// subprocess, which a unit test cannot do. Production always uses the ai
// factory (same pattern as injectMidTurn in midturn.go).
var newBackendForAgent = ai.NewBackendForAgentWithTransport

// runMemberCompact sends "/compact" to the member's agent and consumes the
// stream. It returns a user-facing error (runErr) separately from a setup
// failure (err) so the caller can tell "the agent refused" from "we could not
// start it".
func runMemberCompact(ctx context.Context, groupID, memberID, agentID, transport string) (runErr, err error) {
	projectPath := GetSessionProjectPathAnyPath(groupID)
	backend := groupBackend(memberID)
	req := BuildChatRequest("/compact", memberID, projectPath, backend, agentID, "", "", "", "", resolveFileDir(projectPath), false)
	// A member row carries no chat_history of its own (all speech lands on the
	// group timeline), so the usual resume signals must come from its external
	// session id — exactly what a member turn does. Without this the command
	// would open a BRAND NEW agent session and compact an empty context.
	applyMemberResumeOverrides(&req, GetExternalSessionID(memberID), true)

	be, err := newBackendForAgent(backend, agentID, transport)
	if err != nil {
		return nil, fmt.Errorf("create backend: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, compactTimeout)
	defer cancel()

	eventCh, err := be.ExecuteStream(runCtx, req)
	if err != nil {
		return nil, fmt.Errorf("start compaction: %w", err)
	}
	// The producer closes the channel; drain anything left so it cannot block on
	// a full channel after we stop reading.
	defer func() {
		for range eventCh {
		}
	}()

	for ev := range eventCh {
		if e := consumeCompactEvent(memberID, ev); e != nil && runErr == nil {
			runErr = e
		}
	}
	return runErr, nil
}

// consumeCompactEvent applies one event from a compaction stream. It returns a
// non-nil error only for a failure the user should see.
//
// Every state event is persisted onto the MEMBER row (never the group): that
// row's context_state is the member's own context state, which is what the
// member list and detail panel read back.
func consumeCompactEvent(memberID string, ev ai.StreamEvent) error {
	switch ev.Type {
	case "usage_update", "mode_update", "thinking_effort_update":
		PersistContextStateFromEvent(memberID, ev)
	case "session_capture":
		if ev.Content != "" && GetExternalSessionID(memberID) == "" {
			if uerr := UpdateExternalSessionID(memberID, ev.Content); uerr != nil {
				slog.Warn("group compact: persist external session id failed", "member", memberID, "err", uerr)
			}
		}
	case "compact_detected":
		MarkSessionCompacted(memberID)
	case eventTypeError:
		return errors.New(ev.Error)
	case blockTypeWarning:
		// A failed compaction arrives as a WARNING, not an error (the agent
		// streams no content for it). Only ReasonRefused used to be treated as a
		// failure, so every other failure — request_failed, agent_no_run,
		// backend_exit, auth_required — was reported as SUCCESS: the timeline said
		// "compacted (32% → 32%)" and the member row was marked compacted, while
		// the context had not changed at all. The user then believes the member
		// shrank its context and cannot explain the later overflow.
		//
		// The list is an allow-list of NON-failures, so a new failure reason is
		// surfaced by default rather than silently swallowed.
		if !compactWarningIsBenign(ev.Reason) {
			msg := ev.Content
			if msg == "" {
				msg = ev.Reason
			}
			return errors.New(msg)
		}
	}
	return nil
}

// compactWarningIsBenign reports whether a warning can accompany a SUCCESSFUL
// compaction.
//
// Only informational notices belong here. Everything else — a refused request,
// a failed turn, an unhealthy agent, a sign-in gate — means the context was not
// rewritten, and reporting success would tell the user their context shrank
// when it did not.
func compactWarningIsBenign(reason string) bool {
	switch reason {
	case ai.ReasonEmpty:
		// "The model answered with nothing" — for /compact that is a legitimate
		// outcome (the agent may report nothing at all), and the usage numbers in
		// the completion row show whether anything actually changed.
		return true
	default:
		return false
	}
}

// currentMemberUsage returns the member's usage, preferring the live ACP cache
// (post-compaction, fresher than the last 500ms flush) and falling back to the
// persisted context_state. Returns nil when neither has data.
func currentMemberUsage(memberID string) *ai.UsageState {
	if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(memberID); s.Usage != nil {
		// Persist the live value so the roster keeps showing it after the
		// connection is reclaimed.
		if b, err := json.Marshal(s.Usage); err == nil {
			PatchContextStateMerge(memberID, map[string]string{"usage": string(b)})
		}
		return s.Usage
	}
	if state := GetContextState(memberID); state != nil {
		return state.Usage
	}
	return nil
}
