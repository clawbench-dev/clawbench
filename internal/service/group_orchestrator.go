package service

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"

	"clawbench/internal/ai"
	"clawbench/internal/grouprouting"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// group_orchestrator.go drives a group chat's main loop: host speaks, routes to
// named members, they speak in order, repeat until the host ends the discussion,
// the round cap is reached, or a human interjects. See
// docs/plans/2026-10-04-ai-group-chat-design.md §5.

// groupMemberTurn describes one member's (or the host's) turn.
type groupMemberTurn struct {
	MemberRowID string
	Prompt      string
	IsHost      bool
}

// groupMemberResult is the outcome of one member turn.
//
// CancelReason is separate from Err because a user cancel leaves Err empty
// (run_turn.go sets TurnResult.CancelReason, not Err). Treating "Err empty" as
// "processed successfully" would advance the cursor past context the turn never
// consumed (decision #69).
type groupMemberResult struct {
	Err          string
	CancelReason string
}

// groupTurnRunner runs one member's turn. Production wires it to runTurn with a
// TurnSpec whose SessionID is the member row and TimelineSessionID the group.
// It is a field on the orchestrator so tests can inject a scripted runner.
type groupTurnRunner func(ctx context.Context, groupID string, turn groupMemberTurn) groupMemberResult

// GroupOrchestrator runs one group turn.
type GroupOrchestrator struct {
	groupID string
	project string
	// runTurn is injectable for tests; nil means the production runTurn path.
	runTurn groupTurnRunner
	// fallbackIdx is the round-robin cursor for the parse-failure fallback, so
	// consecutive failures address DIFFERENT members instead of the same first
	// one every time (decision #56).
	fallbackIdx int
	// parseFailures counts consecutive unparseable host routings. Reset by a
	// usable decision; reaching maxConsecutiveParseFailures ends the turn.
	parseFailures int
	// hostFailures counts consecutive host turn FAILURES (backend error /
	// timeout, distinct from an unparseable route). A single failure falls back
	// to round-robin; reaching maxConsecutiveHostFailures ends the turn
	// (design §6: "主持人跑挂 → 回退轮转继续；再失败终止群回合"). Reset by a
	// successful host turn.
	hostFailures int
	// commandInjection is the rendered /cb-* prompt template for THIS turn (the
	// drained message). It is prepended to the FIRST speaker's turn only, so
	// that speaker executes the built-in command once and its spoken result
	// enters the timeline for the others to discuss. Empty = the message was
	// not a built-in command (or no renderer is wired).
	commandInjection string
	// userMessage is the drained user message's literal text. In free mode it
	// carries the initial @-mentions that seed the relay queue; host mode
	// ignores it (humans never route in host mode, decision #47).
	userMessage string
}

// maxConsecutiveParseFailures is how many unparseable host routings in a row
// end the discussion early (decision #56). Two is enough to tell "the host
// fumbled once" from "the host cannot route at all".
const maxConsecutiveParseFailures = 2

// maxConsecutiveHostFailures is how many host turn FAILURES in a row end the
// discussion (design §6). Two: the first falls back to round-robin, the second
// means the host is down — continuing would burn the whole round budget.
const maxConsecutiveHostFailures = 2

// NewGroupOrchestrator builds an orchestrator for a group session.
func NewGroupOrchestrator(groupID string) *GroupOrchestrator {
	return &GroupOrchestrator{
		groupID: groupID,
		project: GetSessionProjectPathAnyPath(groupID),
	}
}

// GetSessionProjectPathAnyPath is a small helper so the orchestrator can resolve
// the project path (empty if the session is gone).
func GetSessionProjectPathAnyPath(sessionID string) string {
	p, _ := GetSessionProjectPathAny(sessionID)
	return p
}

// RunGroupTurnDrain runs one group turn for a message the drain loop already
// materialized, and returns a DrainResult the loop uses to decide whether to
// continue.
//
// It is the SINGLE production entry point for a group turn (decision #45/#58):
// the HTTP handler and the IM path both drive it through RunGroupDrainLoop. It
// deliberately does NOT persist/announce the user message (ClaimNextAndMaterialize
// already wrote the chat_history row and emitted user_message) and does NOT emit
// a terminal event — RunDrainLoop decides when the whole queue is drained and
// sends "done" exactly once, then.
//
// There is no standalone counterpart: an earlier RunGroupTurn entry owned its
// own running state and terminal event, but once both the handler and the IM
// path moved onto this drain entry it became unreachable — and with it the
// terminal responsibilities it carried (see runLoop). It was deleted rather
// than left as a second, silently-dead path (review C1 / N3).
func RunGroupTurnDrain(ctx context.Context, groupID string, msgID int64, row QueuedRow, runner groupTurnRunner) DrainResult {
	o := NewGroupOrchestrator(groupID)
	if runner != nil {
		o.runTurn = runner
	} else if groupDrainRunnerOverride != nil {
		// Test-only: lets a test exercise the REAL production chain with only
		// the backend stubbed (see groupDrainRunnerOverride).
		o.runTurn = groupDrainRunnerOverride
	}
	// A built-in /cb-* command rides in the FIRST speaker's prompt only: that
	// speaker runs the command and reports the result as its speech, and the
	// others discuss it. It is NOT injected into the timeline content (the
	// bubble keeps the user's literal text) and NOT injected to members (the
	// API contract is noise to them, decision #9).
	o.commandInjection = renderGroupCommandForHost(row.Content, o.project, groupID)
	// The user's message carries the free-mode initial targets (@-mentions).
	// Host mode ignores them (decision #47: humans never route in host mode).
	o.userMessage = row.Content
	// NOTE: no running-state management here. The caller (the HTTP handler via
	// TryClaimSessionRun, or the drain loop) already owns the runner and the
	// running flag for the whole queued run. Registering a second, per-message
	// runner would let this function clear the running flag when ONE message
	// finishes, while later queued messages are still to come.
	//
	// ctx is the claim's context, so CancelSession still reaches every member
	// turn (their TurnSpec.Ctx derives from it).
	return o.runLoop(ctx, groupID)
}

// renderGroupCommandForHost returns the rendered /cb-* template for a message,
// or "" when the message is not a built-in command (the renderer returns the
// input unchanged) or no renderer is wired. A render error is logged and
// treated as "no injection" — the turn must still run, with the user's literal
// text visible in the injected context.
func renderGroupCommandForHost(rawMsg, projectPath, groupID string) string {
	if rawMsg == "" || renderGroupCommandFn == nil {
		return ""
	}
	injected, err := renderGroupCommandFn(rawMsg, projectPath, groupID)
	if err != nil {
		slog.Warn("group: /cb-* render failed, running the turn without injection",
			"group", groupID, "err", err)
		return ""
	}
	if injected == rawMsg {
		return "" // not a built-in command
	}
	return injected
}

// runLoop executes one group turn and performs its per-turn finalization,
// returning the outcome. It emits NO terminal event and does NOT touch the
// session's running state — the drain loop (RunGroupDrainLoop) owns both, so a
// run spanning several queued messages sends "done" exactly once.
//
// It dispatches on the group's mode (design §13.1): host mode runs the
// host-routes-members loop, free mode runs the mention-relay loop. Both are
// thin wrappers over drainSpeakers (the shared queue kernel); the finalization
// is identical for both.
//
// The finalization runs on EVERY exit path (cancel, abort, end, round cap,
// error), which is why it lives here rather than in each branch:
//
//   - finalizeGroupOrphans closes a streaming row left open by a Finalize that
//     itself failed. Without it a reload after the turn renders a phantom
//     streaming bubble that never ends (decision #71).
//   - triggerChatSummarization summarizes the discussion ONCE. Member turns
//     deliberately skip summarization (their Finalize writes to the GROUP
//     timeline, so running it per member would summarize every member's reply —
//     decision #55), which makes this the only place it happens. It is also the
//     only caller of triggerChatRecommendation, so skipping it here means a
//     group never produces a recommendation chip.
//
// Summarizing on the CANCEL path too is deliberate and matches single chat: its
// Finalize still finalizes the partial reply (FinalizeCancelledStreamingMessage
// returns a non-zero id), so `if msgID > 0` still calls triggerChatSummarization.
// (Verified against TestSessionExecutor_Finalize_UserCancelLeavesCompletedAtNull.)
//
// The summarizer is synchronous and DB-only — its sole LLM call (the
// recommendation) is already detached into a goroutine — so it runs after the
// rounds return but before the caller can send "done", matching single chat
// where Finalize summarizes before the terminal event.
func (o *GroupOrchestrator) runLoop(groupCtx context.Context, groupID string) DrainResult {
	var res DrainResult
	if GetGroupMode(groupID) == GroupModeFree {
		res = o.runFreeLoop(groupCtx, groupID)
	} else {
		res = o.runRounds(groupCtx, groupID)
	}
	finalizeGroupOrphans(groupID)
	triggerChatSummarization(context.Background(), groupID)
	return res
}

// groupTurnSetup is the shared per-turn scaffolding both modes need: the
// roster, the name/lookup maps, the (possibly test-injected) runner, and the
// member rows registered active so the ACP idle sweep leaves them alone.
type groupTurnSetup struct {
	members   []GroupMember
	names     map[string]string
	byName    map[string]GroupMember
	runner    groupTurnRunner
	activeIDs []string
}

// prepareTurn loads the roster and wires the per-turn scaffolding shared by
// host and free mode. The caller MUST `defer clearGroupMembersActive(s.activeIDs)`
// so the ACP idle-sweep protection is released on every exit path (including
// panics, decision #73). Orphaned streaming rows from a previous crashed turn
// are cleaned before new ones are written (decision #71).
func (o *GroupOrchestrator) prepareTurn(groupID string) (groupTurnSetup, error) {
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return groupTurnSetup{}, fmt.Errorf("list group members: %w", err)
	}
	activeIDs := make([]string, 0, len(members))
	for _, m := range members {
		activeIDs = append(activeIDs, m.ID)
	}
	markGroupMembersActive(activeIDs)
	runner := o.runTurn
	if runner == nil {
		runner = o.defaultRunner
	}
	finalizeGroupOrphans(groupID)
	_, byName := memberLookups(members)
	return groupTurnSetup{
		members:   members,
		names:     memberNameMap(members),
		byName:    byName,
		runner:    runner,
		activeIDs: activeIDs,
	}, nil
}

// runSpeakerTurn executes one member's turn end-to-end: it injects the pending
// private notes addressed to this member, builds the prompt (prepend + context
// + system prompt), runs the turn, advances the member's cursor on success, and
// clears the delivered notes on a clean turn. BOTH modes route their member
// turns through here, so the cursor rule (decision #69) and the
// deliver-on-success note rule cannot drift between them.
//
// instruction is the host's public directive (host mode) or "" (free mode);
// prepend is a one-shot block ahead of everything (the /cb-* command injection);
// sysPrompt is the mode's member system prompt.
func (o *GroupOrchestrator) runSpeakerTurn(ctx context.Context, s groupTurnSetup, t GroupMember, instruction, prepend, sysPrompt string) groupMemberResult {
	pending := pendingBccForTarget(o.groupID, s.names[t.ID])
	bcc := joinPendingBcc(pending)
	cursor := GetMemberCursor(t.ID)
	prompt := groupInjectionTextOrEmpty(o.groupID, t.ID, cursor, s.names, instruction, bcc) + sysPrompt
	if prepend != "" {
		prompt = prepend + "\n\n" + prompt
	}
	preH := GroupTimelineHighWater(o.groupID)
	res := s.runner(ctx, o.groupID, groupMemberTurn{MemberRowID: t.ID, Prompt: prompt})
	if res.CancelReason != "" {
		// The user stopped the turn mid-speech. Delivered notes are NOT cleared
		// (the turn consumed nothing) — they re-send (decision #69).
		return res
	}
	advanceCursorOnSuccess(t.ID, preH, res)
	// Deliver-on-success only: an errored turn keeps its notes.
	if res.Err == "" {
		ids := make([]int64, 0, len(pending))
		for _, p := range pending {
			ids = append(ids, p.ID)
		}
		deletePendingBcc(ids)
	}
	return res
}

// handUserBack ends the current turn because the floor was handed to the human
// user: it clears the user's pending-note bookkeeping (the user reads notes in
// the UI card, never through an injected prompt), leaves a system line so the
// timeline shows why the turn stopped, and returns the (empty) result. Shared
// by both modes (host: the host named User; free: a speaker @-ed User).
func (o *GroupOrchestrator) handUserBack(groupID string) DrainResult {
	deletePendingBccForTarget(groupID, groupUserTarget)
	writeGroupSystemMessage(o.project, groupID,
		i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn"))
	return DrainResult{}
}

// speakerTurn is one queued speaker plus the mode-specific prompt fragments
// that ride with it. Host mode attaches the host's public directive and the
// "member" system prompt; free mode attaches only the free-mode system prompt.
type speakerTurn struct {
	member      GroupMember
	instruction string
	prepend     string
	sysPrompt   string
}

// drainSpeakers is the ONE turn kernel both modes share. It runs queued speakers
// FIFO; when the queue empties it calls refill to obtain more — host mode runs
// the host turn there and returns its routed targets, free mode returns "done".
// A clean turn's output is offered to onSpoke so the mode can extend the queue
// (free mode appends the speaker's @-mentions; host mode passes nil, since its
// members cannot route).
//
// The kernel owns the three rules that must NOT drift between modes:
//   - groupCtx cancellation ⇒ a user-cancel result (checked before every
//     speaker AND before every refill, so a stop during the host turn is seen);
//   - naming the human user ⇒ hand the floor back and end the turn;
//   - a cancelled speaker turn ⇒ stop the WHOLE turn (the rest of the queue is
//     dropped; nothing runs after a stop).
func (o *GroupOrchestrator) drainSpeakers(
	ctx context.Context,
	s groupTurnSetup,
	queue []speakerTurn,
	refill func(ctx context.Context) ([]speakerTurn, bool, DrainResult),
	onSpoke func(ctx context.Context, sp speakerTurn, queue []speakerTurn) []speakerTurn,
) DrainResult {
	for {
		if ctx.Err() != nil {
			return DrainResult{CancelReason: cancelReasonUser}
		}
		if len(queue) == 0 {
			more, ok, res := refill(ctx)
			// ok=false ends the turn; an ok=true-but-empty refill would spin, so
			// treat it as "nothing more" and return the (zero) result.
			if !ok || len(more) == 0 {
				return res
			}
			queue = more
			continue
		}
		sp := queue[0]
		queue = queue[1:]
		if sp.member.ID == groupUserTargetID {
			return o.handUserBack(o.groupID)
		}
		r := o.runSpeakerTurn(ctx, s, sp.member, sp.instruction, sp.prepend, sp.sysPrompt)
		if r.CancelReason != "" {
			return DrainResult{CancelReason: r.CancelReason}
		}
		if r.Err == "" && onSpoke != nil {
			queue = onSpoke(ctx, sp, queue)
		}
	}
}

// runRounds is the host/member decision loop, driven by drainSpeakers: the
// refill closure runs one host turn and returns its routed members (or a
// round-robin fallback pick), and terminates the turn on the end signal, the
// round cap, or repeated host/parse failures.
func (o *GroupOrchestrator) runRounds(ctx context.Context, groupID string) DrainResult {
	hostMemberID := GetGroupHostMember(groupID)
	if hostMemberID == "" {
		return DrainResult{Err: fmt.Sprintf("group %s has no host member", groupID)}
	}
	s, err := o.prepareTurn(groupID)
	if err != nil {
		return DrainResult{Err: err.Error()}
	}
	defer clearGroupMembersActive(s.activeIDs)
	maxRounds := GetGroupMaxRounds(groupID)

	round := 0
	refill := func(ctx context.Context) ([]speakerTurn, bool, DrainResult) {
		if round >= maxRounds {
			// Round cap reached: run the host once more purely for the summary.
			o.summarize(ctx, s.runner, hostMemberID, s.names, s.members)
			return nil, false, DrainResult{}
		}
		round++
		// Host speaks and routes.
		hostCursor := GetMemberCursor(hostMemberID)
		hostPrompt := o.hostPrompt(hostMemberID, hostCursor, s.names, s.members)
		// Prepend the rendered /cb-* template to the FIRST host turn only
		// (decision #79 revision): the host executes the built-in command once
		// and its spoken result enters the timeline for the members to discuss.
		// Injecting on every round would re-run the command each round.
		if round == 1 && o.commandInjection != "" {
			hostPrompt = o.commandInjection + "\n\n" + hostPrompt
		}
		hostPreH := GroupTimelineHighWater(groupID)
		hostRes := s.runner(ctx, groupID, groupMemberTurn{MemberRowID: hostMemberID, Prompt: hostPrompt, IsHost: true})
		if hostRes.CancelReason != "" {
			// The user stopped the turn while the host was speaking. Do NOT fall
			// back to a member turn — that would keep running after a stop.
			return nil, false, DrainResult{CancelReason: hostRes.CancelReason}
		}
		if hostRes.Err != "" {
			slog.Warn("group: host turn failed", "group", groupID, "err", hostRes.Err)
			// Design §6: fall back to round-robin once; a SECOND consecutive
			// host failure means the host is down, so terminate the turn rather
			// than burn the whole round budget (maxRounds host turns).
			o.hostFailures++
			if o.hostFailures >= maxConsecutiveHostFailures {
				slog.Warn("group: aborting after consecutive host failures",
					"group", groupID, "failures", o.hostFailures)
				o.summarize(ctx, s.runner, hostMemberID, s.names, s.members)
				return nil, false, DrainResult{Err: "host turn failed repeatedly"}
			}
			fb, ok := o.pickFallbackMember(hostMemberID, s.members)
			if !ok {
				return nil, false, DrainResult{Err: "host turn failed and no member could take over"}
			}
			return []speakerTurn{{member: fb}}, true, DrainResult{}
		}
		// A successful host turn clears the failure streak.
		o.hostFailures = 0
		advanceCursorOnSuccess(hostMemberID, hostPreH, hostRes)

		route := grouprouting.Parse(o.lastMemberOutput(groupID, hostMemberID))
		if route.End {
			// The end-signal turn already contains the summary (B2 prompt).
			return nil, false, DrainResult{}
		}

		targets := resolveSpeakerTargets(route, s.members, hostMemberID)
		if len(targets) == 0 {
			// Parse failed or no valid speakers: fall back to round-robin.
			//
			// Two consecutive failures mean the host is not producing usable
			// routing at all — continuing would burn the whole round budget on
			// a broken loop. Abort and finalize (decision #56).
			o.parseFailures++
			if o.parseFailures >= maxConsecutiveParseFailures {
				slog.Warn("group: aborting after consecutive parse failures",
					"group", groupID, "failures", o.parseFailures)
				o.summarize(ctx, s.runner, hostMemberID, s.names, s.members)
				return nil, false, DrainResult{}
			}
			fb, ok := o.pickFallbackMember(hostMemberID, s.members)
			if !ok {
				return nil, false, DrainResult{}
			}
			return []speakerTurn{{member: fb}}, true, DrainResult{}
		}
		// A usable routing decision resets the failure streak.
		o.parseFailures = 0

		// Persist this round's private notes BEFORE dispatching anyone. A note
		// addressed to a participant who is not named this round stays in the
		// table and is delivered with that participant's next turn, so the host
		// can "hand everyone a word, then call on one player first" (the
		// Who-Is-The-Spy setup). Delivery deletes the row; a failed/cancelled
		// turn keeps it for the next attempt (decision #69 semantics).
		storeRouteNotes(groupID, route, hostMemberID, s.byName)

		// Members speak in order; each sees the previous speakers' output.
		out := make([]speakerTurn, 0, len(targets))
		for _, t := range targets {
			out = append(out, speakerTurn{
				member:      t,
				instruction: route.Instruction,
				sysPrompt:   BuildMemberSystemPrompt(memberRoster(s.members), s.names[t.ID]),
			})
		}
		return out, true, DrainResult{}
	}

	return o.drainSpeakers(ctx, s, nil, refill, nil)
}

// runFreeLoop is the free-chat mode's main loop (design §13.3). There is no
// host: the user's message seeds a FIFO queue of speakers (the @-mentioned
// members, or — when the message mentions nobody — every active member in
// roster order), and each speaker's own @-mentions append to the queue's tail
// (via drainSpeakers' onSpoke). The relay runs until the queue drains (nobody
// is @-ed any more), a member hands the floor back to the human ("User"), or
// the user stops the turn.
//
// There is deliberately NO round cap and NO loop detection (decision #F13): the
// user asked for unlimited relay, stopped by hand. A@B,B@A therefore burns
// tokens until the user presses stop — a known, accepted risk.
func (o *GroupOrchestrator) runFreeLoop(ctx context.Context, groupID string) DrainResult {
	s, err := o.prepareTurn(groupID)
	if err != nil {
		return DrainResult{Err: err.Error()}
	}
	defer clearGroupMembersActive(s.activeIDs)

	// Seed the queue from the user's message: the @-mentioned members, in the
	// order mentioned; or every active member in roster order when nobody is
	// mentioned.
	targets := o.freeInitialTargets(s.members)
	if len(targets) == 0 {
		return DrainResult{}
	}
	queue := make([]speakerTurn, 0, len(targets))
	for i, t := range targets {
		st := speakerTurn{
			member:    t,
			sysPrompt: BuildFreeMemberSystemPrompt(memberRoster(s.members), s.names[t.ID]),
		}
		// commandInjection runs on the FIRST speaker only (design §13.3 #F16).
		if i == 0 {
			st.prepend = o.commandInjection
		}
		queue = append(queue, st)
	}

	// The queue is self-extending: free mode has no router, so an empty queue
	// means the relay is over.
	refill := func(context.Context) ([]speakerTurn, bool, DrainResult) {
		return nil, false, DrainResult{}
	}
	onSpoke := func(_ context.Context, sp speakerTurn, q []speakerTurn) []speakerTurn {
		o.storeFreeSpeakerNotes(groupID, sp.member.ID, s.byName)
		return o.appendFreeTargets(q, groupID, sp.member.ID, s.members, s.names)
	}
	return o.drainSpeakers(ctx, s, queue, refill, onSpoke)
}

// pickFallbackMember picks the next round-robin fallback speaker, skipping the
// host and left members, and advances o.fallbackIdx. Returns false when no
// eligible member exists.
//
// The pick ROTATES via o.fallbackIdx (decision #56): when the host's routing is
// repeatedly unparseable, always choosing the first member would turn the
// discussion into that member's monologue. The index advances past whatever was
// picked and wraps.
func (o *GroupOrchestrator) pickFallbackMember(hostMemberID string, members []GroupMember) (GroupMember, bool) {
	eligible := make([]GroupMember, 0, len(members))
	for _, m := range members {
		if m.Left || m.ID == hostMemberID {
			continue
		}
		eligible = append(eligible, m)
	}
	if len(eligible) == 0 {
		return GroupMember{}, false
	}
	if o.fallbackIdx >= len(eligible) {
		o.fallbackIdx = 0
	}
	m := eligible[o.fallbackIdx]
	o.fallbackIdx = (o.fallbackIdx + 1) % len(eligible)
	return m, true
}

// storeFreeSpeakerNotes persists the private notes a free-mode speaker emitted,
// so each note is delivered on its target's next turn (the group_pending_bcc
// contract host mode uses). It re-parses the speaker's own output (free mode
// has no pre-parsed route) and delegates the validation/storage to
// storeRouteNotes.
func (o *GroupOrchestrator) storeFreeSpeakerNotes(groupID, speakerID string, byName map[string]GroupMember) {
	output := o.lastMemberOutput(groupID, speakerID)
	if output == "" {
		return
	}
	storeRouteNotes(groupID, grouprouting.Parse(output), speakerID, byName)
}

// storeRouteNotes persists a parsed route's private notes, validating each
// target against the roster: an unknown or left target is dropped (it could
// never be delivered — storing it would be silent loss plus unbounded growth of
// group_pending_bcc), and a self-directed note is dropped (it would be
// delivered on the speaker's own next turn). Both modes store notes through
// here. The note is keyed by the RESOLVED member name, which is exactly what
// the delivery lookup (pendingBccForTarget(names[t.ID])) uses.
func storeRouteNotes(groupID string, route grouprouting.Result, speakerID string, byName map[string]GroupMember) {
	for _, e := range route.Bcc {
		for _, target := range e.Targets {
			m, ok := byName[strings.TrimSpace(target)]
			if !ok || m.ID == speakerID {
				continue
			}
			addPendingBcc(groupID, m.Name, e.Content)
		}
	}
}

// freeInitialTargets resolves the free-mode queue seed from the user message:
// the @-mentioned members in the order mentioned (de-duplicated), or every
// active member in roster order when the message mentions nobody. A mention
// target is matched by member ROW ID first (the frontend sends ids), then by
// display name (a user who typed the tag by hand).
//
// Left members are excluded. An unknown name is dropped silently at this stage
// (the "unknown name" system notice is only for AGENT-emitted mentions — see
// appendFreeTargets — because a user's typo is not a discussion event).
func (o *GroupOrchestrator) freeInitialTargets(members []GroupMember) []GroupMember {
	mentioned := resolveSpeakerTargets(grouprouting.Parse(o.userMessage), members, "")
	if len(mentioned) == 0 {
		out := make([]GroupMember, 0, len(members))
		for _, m := range members {
			if !m.Left {
				out = append(out, m)
			}
		}
		return out
	}
	return mentioned
}

// appendFreeTargets parses the just-finished speaker's output for @-mentions and
// appends the valid, not-already-queued targets to the queue's tail. Returns the
// (possibly extended) queue.
//
// Dedup rule (decision #F9): only targets already WAITING in the queue are
// skipped. A member that has already spoken may be @-ed again and will speak
// again — this is what makes unlimited relay possible (A@B, B@A loops).
//
// Invalid targets (decision #F7): a self-mention and a left-member mention are
// dropped silently (model slips not worth surfacing); an unknown NAME emits a
// visible system notice (it usually means a member was removed/renamed, or the
// model hallucinated — the user should know).
func (o *GroupOrchestrator) appendFreeTargets(queue []speakerTurn, groupID, speakerID string, members []GroupMember, names map[string]string) []speakerTurn {
	output := o.lastMemberOutput(groupID, speakerID)
	if output == "" {
		return queue
	}
	route := grouprouting.Parse(output)
	if !route.Found {
		return queue
	}

	byID, byName := memberLookups(members)
	waiting := map[string]bool{}
	for _, st := range queue {
		waiting[st.member.ID] = true
	}

	var unknown []string
	for _, tgt := range route.Speakers {
		m, ok := byID[strings.TrimSpace(tgt)]
		if !ok {
			m, ok = byName[strings.TrimSpace(tgt)]
		}
		if !ok {
			unknown = append(unknown, tgt)
			continue
		}
		if m.ID == speakerID {
			continue // self-mention: drop silently
		}
		if m.Left && m.ID != groupUserTargetID {
			continue // left member: drop silently
		}
		if waiting[m.ID] {
			continue // already queued: do not duplicate
		}
		waiting[m.ID] = true
		queue = append(queue, speakerTurn{
			member:    m,
			sysPrompt: BuildFreeMemberSystemPrompt(memberRoster(members), names[m.ID]),
		})
	}
	if len(unknown) > 0 {
		// Surface once, aggregated, so a hallucinated name is visible without
		// flooding the timeline.
		speakerName := names[speakerID]
		if speakerName == "" {
			speakerName = "成员"
		}
		writeGroupSystemMessage(o.project, groupID,
			fmt.Sprintf("%s @ 了一个不存在的成员：%s（已忽略）", speakerName, strings.Join(unknown, "、")))
	}
	return queue
}

// resolveSpeakerTargets maps a parsed route's public targets to member rows, in
// the order named, de-duplicated. A target is matched by member ROW id first
// (a user's @-mention carries the row id) then by display name (an agent writes
// the name). Unknown/left targets are dropped, and so is excludeID — the host
// must not route to itself (which would turn every "member turn" into another
// host turn, the reported "only the host ever speaks" defect), and a free-mode
// speaker must not @-queue itself. The human user is resolvable under its
// reserved name (and, for the frontend, its sentinel id).
func resolveSpeakerTargets(route grouprouting.Result, members []GroupMember, excludeID string) []GroupMember {
	if !route.Found {
		return nil
	}
	byID, byName := memberLookups(members)
	out := make([]GroupMember, 0, len(route.Speakers))
	seen := map[string]bool{}
	for _, tgt := range route.Speakers {
		t := strings.TrimSpace(tgt)
		m, ok := byID[t]
		if !ok {
			m, ok = byName[t]
		}
		if !ok || seen[m.ID] || m.ID == excludeID {
			continue
		}
		if m.Left && m.ID != groupUserTargetID {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

// lastMemberOutput returns a specific member's (or the host's) most recent
// assistant speech on the group timeline, used to parse that speaker's routing
// decision / @-mentions. There is no separate host variant: the host is just a
// member row.
func (o *GroupOrchestrator) lastMemberOutput(groupID, memberID string) string {
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].AgentID == memberID && msgs[i].Role == roleAssistant {
			return ExtractPlainText(msgs[i].Content)
		}
	}
	return ""
}

// summarize runs the host once more with the summary-only prompt. The host's
// routing tags in this turn are ignored (never parsed).
func (o *GroupOrchestrator) summarize(ctx context.Context, runner groupTurnRunner, hostMemberID string, names map[string]string, members []GroupMember) {
	cursor := GetMemberCursor(hostMemberID)
	prompt := groupInjectionTextOrEmpty(o.groupID, hostMemberID, cursor, names, "", "") + BuildHostSummaryPrompt(activeMemberNamesExcept(members, hostMemberID))
	preH := GroupTimelineHighWater(o.groupID)
	res := runner(ctx, o.groupID, groupMemberTurn{MemberRowID: hostMemberID, Prompt: prompt, IsHost: true})
	advanceCursorOnSuccess(hostMemberID, preH, res)
}

// advanceCursorOnSuccess records a member's seen_cursor after a turn, applying
// BOTH rules of decision #69 / design §5.1:
//
//  1. Advance ONLY on a turn that actually processed its input — i.e. no error
//     AND no user cancel. A cancelled turn was fed the context but produced
//     nothing, so advancing past it would make `(oldCursor, preH]` permanently
//     unreachable for that member (it silently answers off-topic next round).
//     Cancel is NOT an error (run_turn.go puts it in CancelReason, leaving Err
//     empty), so both must be checked.
//  2. Advance to the PRE-speech high-water `preH`, not the current one. A
//     synchronous HTTP write can land a row (e.g. a membership system event)
//     WHILE this member is speaking; that row was never injected to it, so
//     advancing past it would swallow it. Using preH means the next round still
//     injects it. When nothing raced, preH is exactly the member's own reply id
//     and the reply is filtered by the author==self rule — so the common case is
//     unchanged.
func advanceCursorOnSuccess(memberID string, preH int64, res groupMemberResult) {
	if res.Err != "" {
		slog.Warn("group: turn failed, cursor not advanced",
			"member", memberID, "err", res.Err)
		return
	}
	if res.CancelReason != "" {
		// The user stopped this turn. It processed nothing, so the context it
		// was handed must be re-injected next round (decision #69).
		slog.Info("group: turn cancelled, cursor not advanced",
			"member", memberID, "reason", res.CancelReason)
		return
	}
	SetMemberCursor(memberID, preH)
}

// hostPrompt builds the host's turn prompt: the incremental group context plus
// the host instruction (routing rules). The selectable list EXCLUDES the host
// itself, so the model cannot name itself (which caused the self-route loop).
func (o *GroupOrchestrator) hostPrompt(hostMemberID string, cursor int64, names map[string]string, members []GroupMember) string {
	ctxText := groupInjectionTextOrEmpty(o.groupID, hostMemberID, cursor, names, "", "")
	return ctxText + BuildHostSystemPrompt(activeMemberNamesExcept(members, hostMemberID))
}

// buildMemberTurnSpec assembles the TurnSpec for one member (or host) turn.
//
// Extracted from defaultRunner so the wiring can be unit-tested without running
// a real backend: the load-bearing invariants are that the CONNECTION session
// is the member row (SessionID) while the TIMELINE is the group (so output
// lands on the group timeline and is attributed to the member via SpeakerID).
func (o *GroupOrchestrator) buildMemberTurnSpec(ctx context.Context, groupID string, turn groupMemberTurn) TurnSpec {
	project := o.project
	agentID := ResolveAgentID(turn.MemberRowID, "")
	backend := groupBackend(turn.MemberRowID)
	req := BuildChatRequest(turn.Prompt, turn.MemberRowID, project, backend, agentID, "", "", "", "", resolveFileDir(project), false)
	// Fix the chat_history-derived resume signals for a member row.
	applyMemberResumeOverrides(&req, GetExternalSessionID(turn.MemberRowID), resolveIsACP(agentID, ""))
	return TurnSpec{
		Ctx:               ctx,
		Mode:              ModeInteractive,
		ProjectPath:       project,
		BackendName:       backend,
		SessionID:         turn.MemberRowID,
		TimelineSessionID: groupID,
		SpeakerID:         turn.MemberRowID,
		// Member turns write to the GROUP timeline, so the executor must NOT
		// summarize per member (N LLM calls per round). The orchestrator
		// summarizes once at the end of the turn (runLoop → triggerChatSummarization).
		SuppressSummarization: true,
		AgentID:               agentID,
		ChatReq:               req,
		FileDir:               resolveFileDir(project),
		DrainOnFinalize:       true,
	}
}

// defaultRunner runs a member turn through the production runTurn path.
//
// After the turn returns it emits a per-member "stream_finalize" on the group
// timeline. This is what lets the frontend close THIS member's streaming bubble
// before the next member's stream_start arrives: without it the first member's
// placeholder stays streaming, so the next member's stream_start finds an
// existing streaming message and never opens a new bubble — the reported
// "other agents don't stream at all until you switch sessions". The whole-group
// "done" (emitted once by the drain loop's markDoneAndSendFinal) is a different
// event: it also clears loading and ends the run, so it cannot be used per member.
func (o *GroupOrchestrator) defaultRunner(ctx context.Context, groupID string, turn groupMemberTurn) groupMemberResult {
	res := runTurn(o.buildMemberTurnSpec(ctx, groupID, turn))
	emitGroupMemberFinalize(groupID, res.MsgID)
	// No extra failure handling is needed here (decision #51): every path that
	// sets res.Err already persisted an attributed warning block.
	//   - early failures (backend create / stream start) go through
	//     TurnSpec.failTurn, which writes one with SpeakerID = the member row;
	//   - a timeout appends a warning in the executor's content assembly
	//     (session_executor.go:1526) before Finalize persists it.
	// The frontend then shows WHICH member failed, and the cursor fix (#69)
	// makes sure the member is re-fed that context next round.
	//
	// CancelReason is carried through separately from Err: runTurn reports a
	// user cancel there (leaving Err empty), and the orchestrator must NOT treat
	// a cancelled turn as "processed successfully" (decision #69).
	return groupMemberResult{Err: res.Err, CancelReason: res.CancelReason}
}

// emitGroupMemberFinalize tells the group's subscribers that one member turn
// has ended, so the frontend finalizes that member's streaming bubble. msgID is
// the member row's streaming message id (0 if the turn never started). It is a
// var seam so tests can observe the contract without a WS hub.
var emitGroupMemberFinalize = func(groupID string, msgID int64) {
	ws.EmitToSession(groupID, ai.StreamEvent{
		Type:         "stream_finalize",
		StreamFinish: &ai.StreamFinishData{MessageID: msgID},
	})
}

// --- small helpers ---

func groupBackend(sessionID string) string {
	if info := GetSessionFullInfo(sessionID); info != nil {
		return info.Backend
	}
	return ""
}

func memberNameMap(members []GroupMember) map[string]string {
	names := make(map[string]string, len(members))
	for _, m := range members {
		names[m.ID] = m.Name
	}
	return names
}

// memberLookups builds the two maps a mention target is resolved through: by
// member ROW id (the frontend's user mentions carry ids) and by display name
// (an agent writes names). Left members are excluded from the name map (they
// cannot speak), and the human user is added under its reserved name.
func memberLookups(members []GroupMember) (byID, byName map[string]GroupMember) {
	byID = make(map[string]GroupMember, len(members))
	byName = make(map[string]GroupMember, len(members)+1)
	for _, m := range members {
		byID[m.ID] = m
		if !m.Left {
			byName[strings.TrimSpace(m.Name)] = m
		}
	}
	byName[groupUserTarget] = GroupMember{ID: groupUserTargetID, Name: groupUserTarget}
	return byID, byName
}

// activeMemberNamesExcept returns the members the host may route to, with the
// metadata that makes routing sensible (decision #39/#41): every member EXCEPT
// the host itself, each carrying its specialty and whether it has left.
//
// Left members are INCLUDED (marked, not dropped): their past speech is still
// on the timeline, so omitting them would make the host see speech from a name
// it cannot address. Marking them is what stops the host from routing to
// someone who is gone (which would silently fall back to round-robin).
//
// The host must never be offered as a routing target (see resolveSpeakerTargets).
//
// The human user is appended as a routable participant (the reserved name
// "User"): naming the user ends the round and waits for their reply, so the
// host needs to see them in the list to be able to call on them at all.
func activeMemberNamesExcept(members []GroupMember, hostMemberID string) []HostMemberInfo {
	out := make([]HostMemberInfo, 0, len(members)+1)
	for _, m := range members {
		if m.ID == hostMemberID {
			continue
		}
		out = append(out, HostMemberInfo{
			Name:      m.Name,
			Specialty: GetAgentSpecialty(m.AgentID),
			Left:      m.Left,
		})
	}
	out = append(out, HostMemberInfo{Name: groupUserTarget})
	return out
}

// memberRoster converts the group's members into the HostMemberInfo shape used
// by the prompt builders (name + specialty + left marker).
func memberRoster(members []GroupMember) []HostMemberInfo {
	out := make([]HostMemberInfo, 0, len(members))
	for _, m := range members {
		out = append(out, HostMemberInfo{
			Name:      m.Name,
			Specialty: GetAgentSpecialty(m.AgentID),
			Left:      m.Left,
		})
	}
	return out
}

// groupInjectionTextOrEmpty is the injectable seam over groupInjectionText used
// by the orchestrator. bcc is the host's private note for THIS member (already
// filtered by name); pass "" when there is none.
func groupInjectionTextOrEmpty(groupID, self string, cursor int64, names map[string]string, instruction, bcc string) string {
	text, err := groupInjectionText(groupID, self, cursor, names, instruction, bcc)
	if err != nil {
		slog.Warn("group: injection load failed", "group", groupID, "err", err)
		return ""
	}
	return text
}

// joinPendingBcc renders the accumulated private notes for one target into the
// single block injected as that member's private directive, or "" when there
// are none. Notes reach here through group_pending_bcc (see addPendingBcc), so
// a note from an earlier round — or one addressed to a member this round did
// not name — is delivered with the member's next turn.
func joinPendingBcc(pending []pendingBcc) string {
	if len(pending) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pending))
	for _, p := range pending {
		parts = append(parts, p.Content)
	}
	return strings.Join(parts, "\n\n")
}

// finalizeGroupOrphans closes any streaming=1 rows left on the group timeline
// by a group turn whose Finalize failed, so they do not surface as phantom
// streaming bubbles on reload (decision #71). Uses the group session as the
// orphan-finalize target. Called at the end of every group turn (runLoop) —
// both after the previous crashed turn (a stale row) and after this one.
func finalizeGroupOrphans(groupID string) {
	finalizeOrphanedStreamingMessages(groupID, "interrupt")
}

// --- ACP idle-sweep protection for group members (decision #73) ---

// groupActiveMemberIDs tracks the member rows a running group turn is using.
//
// Why it exists: an ACP connection is keyed by the MEMBER row id, but a group
// turn only registers the GROUP row as running. The idle sweep asks
// IsSessionRunning(memberID), gets false, and kills a member's connection in
// the middle of a live discussion — forcing a respawn (seconds, per member)
// exactly when the debate needs it.
//
// It is deliberately a SEPARATE registry from the session runner registry:
// GetRunningSessionIDs feeds the session list and the "project has a running
// session, refuse to delete" guard, and member rows are hidden from users. A
// member row must never appear there (decision #73).
var (
	groupActiveMembersMu sync.RWMutex
	groupActiveMembers   = map[string]int{} // member row id -> refcount
)

// markGroupMembersActive registers the member rows a group turn is about to use.
// Idempotent per id via refcount, so overlapping registrations are safe.
func markGroupMembersActive(memberIDs []string) {
	groupActiveMembersMu.Lock()
	defer groupActiveMembersMu.Unlock()
	for _, id := range memberIDs {
		if id == "" {
			continue
		}
		groupActiveMembers[id]++
	}
}

// clearGroupMembersActive unregisters member rows when the group turn ends.
func clearGroupMembersActive(memberIDs []string) {
	groupActiveMembersMu.Lock()
	defer groupActiveMembersMu.Unlock()
	for _, id := range memberIDs {
		if id == "" {
			continue
		}
		if n := groupActiveMembers[id]; n <= 1 {
			delete(groupActiveMembers, id)
		} else {
			groupActiveMembers[id] = n - 1
		}
	}
}

// IsSessionRunningForSweep reports whether the ACP idle sweep must treat
// sessionID as busy. It is IsSessionRunning OR "this row is a member of a group
// turn that is running right now".
//
// The split matters: the sweep must see members as busy (their connections are
// keyed by member row id), while every user-facing consumer keeps using
// IsSessionRunning / GetRunningSessionIDs and therefore never sees the hidden
// member rows (decision #73).
func IsSessionRunningForSweep(sessionID string) bool {
	if IsSessionRunning(sessionID) {
		return true
	}
	groupActiveMembersMu.RLock()
	defer groupActiveMembersMu.RUnlock()
	return groupActiveMembers[sessionID] > 0
}

// emitGroupSystemMessage broadcasts a persisted role='system' timeline row to
// the group's subscribers so a membership change appears immediately (decisions
// #40/#43). It is a seam (var) so tests can observe without the WS hub. msgID
// is the chat_history row id; the frontend dedups on
// it, so a duplicate delivery cannot render the row twice.
var emitGroupSystemMessage = func(groupID string, msgID int64, text string) {
	ws.EmitToSession(groupID, ai.StreamEvent{
		Type: eventTypeSystemMessage,
		SystemMessage: &ai.SystemMessageData{
			MessageID: msgID,
			Content:   text,
		},
	})
}

// RunGroupDrainLoop drives a group turn for `first` (an already-materialized
// user message) and then drains any queued messages through the SAME loop
// single chat uses, so queueing semantics, the terminal event and the push
// contract are identical (decisions #45/#58/#72).
//
// The drain loop — not the orchestrator — owns the terminal event: the group
// turn returns a DrainResult and RunDrainLoop sends "done" exactly once when
// the whole queue is empty. RunGroupTurnDrain deliberately emits nothing
// terminal, so this is the only place a group run ends.
//
// runCtx is the claim's context (from TryClaimSessionRun or the enqueue path),
// so CancelSession reaches every member turn. The caller is responsible for
// FinishSessionRun; this function does not touch the running flag, because the
// drain loop's MarkDoneAndSendFinal owns that transition.
//
// A panic barrier wraps the whole run: a group turn runs N members × maxRounds
// turns through runTurn, and a panic anywhere in that chain (a nil deref, a
// backend SDK panic its own recover does not cover) would otherwise unwind the
// caller's goroutine and — because it is an uncaught panic in a goroutine —
// terminate the whole process, dropping every session, task and WS connection.
// Single chat has the same protection (handler/chat.go's AI goroutine,
// handleSessionPanic). This is a net, not a substitute for fixing panics.
func RunGroupDrainLoop(runCtx context.Context, groupID, projectPath string, firstMsgID int64, firstText string, firstFiles []model.FileEntry) {
	defer recoverGroupDrainPanic(groupID, projectPath)

	markDoneAndSendFinal := func(event ai.StreamEvent) {
		// Clear running BEFORE the terminal event so a loadHistory triggered by
		// "done" cannot see running=true and reconnect in a loop.
		SetSessionRunning(groupID, false, true)
		ws.EmitToSession(groupID, event)
		if event.Type == "done" {
			if !EmitSessionPushNotification(groupID, statusCompleted) {
				return
			}
			EmitSessionEventWSOnly(groupID, "completed", false)
		}
	}

	first := runGroupTurnDrainFn(runCtx, groupID, firstMsgID,
		QueuedRow{SessionID: groupID, Content: firstText, Files: firstFiles}, nil)

	RunDrainLoop(DrainConfig{
		SessionID:   groupID,
		ProjectPath: projectPath,
		BackendName: GetSessionBackend(groupID),
		ExecuteRunWithMessage: func(msgID int64, row QueuedRow) DrainResult {
			return runGroupTurnDrainFn(runCtx, groupID, msgID, row, nil)
		},
		// Each intermediate answer in a multi-message group drain is its own
		// completed turn: notify now, not once at the very end (decision #72).
		OnTurnAnswered: func() {
			EmitTurnAnsweredNotification(groupID)
		},
		MarkDoneAndSendFinal: markDoneAndSendFinal,
	}, first)
}

// runGroupTurnDrainFn is the injectable indirection over RunGroupTurnDrain so
// tests can drive RunGroupDrainLoop (and its push contract, decision #72)
// without running a real orchestrator/backend.
//
// NOTE: replacing this wholesale bypasses the production orchestration path, so
// the tests that use it do NOT cover "the real RunGroupTurnDrain runs". That is
// covered separately via groupDrainRunnerOverride below.
var runGroupTurnDrainFn = RunGroupTurnDrain

// SetRunGroupTurnDrainForTest swaps the group-turn-runner seam and returns the
// previous one. Pass nil to restore the default.
func SetRunGroupTurnDrainForTest(fn func(context.Context, string, int64, QueuedRow, groupTurnRunner) DrainResult) func(context.Context, string, int64, QueuedRow, groupTurnRunner) DrainResult {
	prev := runGroupTurnDrainFn
	if fn == nil {
		runGroupTurnDrainFn = RunGroupTurnDrain
	} else {
		runGroupTurnDrainFn = fn
	}
	return prev
}

// groupDrainRunnerOverride, when non-nil, replaces the per-member runner that
// RunGroupTurnDrain uses. It exists so a test can drive the REAL production
// chain (RunGroupDrainLoop → RunGroupTurnDrain → runLoop → member turns) with
// only the backend stubbed, instead of replacing the whole orchestrator via
// runGroupTurnDrainFn. Nil in production (member turns go through defaultRunner).
var groupDrainRunnerOverride groupTurnRunner

// SetGroupDrainRunnerForTest swaps the per-member runner override and returns
// the previous one. Pass nil to restore the default (real runTurn).
func SetGroupDrainRunnerForTest(fn groupTurnRunner) groupTurnRunner {
	prev := groupDrainRunnerOverride
	groupDrainRunnerOverride = fn
	return prev
}

// renderGroupCommandFn renders a ClawBench built-in command's (/cb-*) prompt
// template for a group's HOST turn. It is injected by main.go from the handler
// package, which owns the OpenAPI-rendered templates, because the orchestrator
// (service) cannot import handler (that would be a cycle). Same pattern as
// SetPersistBingStateFn / SetSessionRunningChecker.
//
// Contract: for a non-command input it returns the input unchanged, and the
// caller treats "unchanged" as "no injection". Nil disables injection entirely.
var renderGroupCommandFn func(rawMsg, projectPath, sessionID string) (string, error)

// SetRenderGroupCommandFn wires the /cb-* renderer for group host prompts and
// returns the previous one. Pass nil to disable. Called once from main.go;
// tests use the return value to restore.
func SetRenderGroupCommandFn(fn func(rawMsg, projectPath, sessionID string) (string, error)) func(rawMsg, projectPath, sessionID string) (string, error) {
	prev := renderGroupCommandFn
	renderGroupCommandFn = fn
	return prev
}

// recoverGroupDrainPanic is the panic barrier for a group drain run (see
// RunGroupDrainLoop). It is a named function so tests can drive it directly by
// panicking inside a deferred call. Mirrors handleSessionPanic's terminal
// cleanup: clear the runner, surface an error, notify, and close any streaming
// row left open so the group does not reload into a phantom bubble.
func recoverGroupDrainPanic(groupID, projectPath string) {
	if r := recover(); r != nil {
		slog.Error("group drain loop panicked",
			slog.String("session", groupID),
			slog.Any("panic", r),
			slog.String("stack", string(debug.Stack())))
		FinishSessionRun(groupID)
		emitDrainEvent(groupID, ai.StreamEvent{
			Type:   eventTypeError,
			Error:  "AI internal error, please retry",
			Reason: ai.ReasonPanic,
		})
		EmitSessionPushNotification(groupID, statusCancelled)
		// Close the orphan streaming row WITHOUT a backend scope. A member's
		// streaming row is written with the MEMBER's backend
		// (session_executor.go's CreateStreamingMessageWithAgent uses
		// cfg.BackendName), while FinalizeStreamingMessage filters on backend —
		// so passing the group row's backend (the host's) silently matches
		// nothing in a heterogeneous group. finalizeGroupOrphans uses
		// finalizeOrphanedStreamingMessages, which has no backend predicate.
		finalizeGroupOrphans(groupID)
	}
}
