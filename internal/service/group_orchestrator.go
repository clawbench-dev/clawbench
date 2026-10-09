package service

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

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

// groupMemberTurn describes one member's (or the host's) turn. There is no
// IsHost flag: the host is just a member row, and every consumer distinguishes
// it by comparing MemberRowID to the group's host pointer.
type groupMemberTurn struct {
	MemberRowID string
	Prompt      string
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
	// firstTurnDone is set once the command injection has been consumed, so the
	// /cb-* template reaches exactly ONE speaker per turn — whichever mode
	// speaks first (host mode: the host; free mode: the first queued member).
	// runSpeakerTurn owns this so neither mode has to manage the "first turn"
	// bookkeeping itself. A fresh orchestrator is built per turn, so it starts
	// false.
	firstTurnDone bool
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
	byID      map[string]GroupMember
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
	byID, byName := memberLookups(members)
	// The user's own private notes (密送) are stored here — the ONE point both
	// modes share, before any speaker runs, so a note to a member who speaks
	// THIS turn is already pending when that member's turn is built. Honored in
	// both modes (a note is a payload, orthogonal to routing — see storeUserNotes).
	o.storeUserNotes(groupID, byID, byName)
	return groupTurnSetup{
		members:   members,
		names:     memberNameMap(members),
		byID:      byID,
		byName:    byName,
		runner:    runner,
		activeIDs: activeIDs,
	}, nil
}

// runSpeakerTurn executes one speaker's turn end-to-end: it injects the pending
// runSpeakerTurn runs ONE speaker turn end-to-end (snapshot + execute). It is
// the single-turn convenience used by the host's own turn and the summary turn,
// which are never part of a parallel group. Member turns in a group go through
// snapshotTurn/executeTurn directly (so a parallel group can snapshot all of
// them before launching).
func (o *GroupOrchestrator) runSpeakerTurn(ctx context.Context, s groupTurnSetup, t GroupMember, instruction, sysPrompt string) groupMemberResult {
	st := speakerTurn{member: t, instruction: instruction, sysPrompt: sysPrompt}
	p := o.snapshotTurn(s, st, GroupTimelineHighWater(o.groupID))
	return o.executeTurn(ctx, s, p)
}

// handUserBack ends the current turn because the floor was handed to the human
// user: it clears the user's pending-note bookkeeping (the user reads notes in
// the UI card, never through an injected prompt), leaves a system line so the
// timeline shows why the turn stopped, and returns the (empty) result. Shared
// by both modes (host: the host named User; free: a speaker @-ed User).
func (o *GroupOrchestrator) handUserBack(groupID string) DrainResult {
	deletePendingBccForTarget(groupID, groupUserTarget())
	writeGroupSystemMessage(o.project, groupID,
		i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn"))
	return DrainResult{}
}

// speakerTurn is one queued speaker plus the mode-specific system prompt that
// rides with it. Host mode attaches the host's public directive to routed
// members and the "member" system prompt; free mode attaches only the free-mode
// system prompt.
type speakerTurn struct {
	member      GroupMember
	instruction string
	sysPrompt   string
}

// speakerGroup is a batch of speakers plus how they should be scheduled:
// parallel groups run their members CONCURRENTLY (and, per the snapshot rule,
// none sees another's output this round); sequential groups run them one after
// another, each seeing the previous speaker's output.
type speakerGroup struct {
	turns    []speakerTurn
	parallel bool
}

// preparedTurn is a speaker turn whose prompt has already been built from a
// timeline snapshot, ready to run. Splitting "build" from "run" is what makes a
// parallel group correct: every member's prompt is built from the SAME
// group-start snapshot BEFORE any of them writes, so none sees a sibling's
// output (the "simultaneous, mutually non-referencing" contract). Building
// lazily inside the goroutine would race — a fast member's first flush could
// land before a slow member builds its prompt.
type preparedTurn struct {
	turn    speakerTurn
	prompt  string
	pending []pendingBcc
	// highWater is the cursor target to advance to on success: the pre-speech
	// high-water for a sequential turn, or the group-start high-water for a
	// parallel turn (so the member does not "see" its siblings next round).
	highWater int64
}

// snapshotTurn builds a preparedTurn: it resolves the notes this speaker
// consumes, builds the prompt from the timeline up to the speaker's OWN cursor
// (never the group high-water — that would skip rows the speaker has not yet
// seen and lose them permanently), and captures the cursor advance target. The
// /cb-* template goes to the turn's FIRST speaker (o.firstTurnDone), decided
// here in the single-threaded snapshot phase so it never depends on goroutine
// scheduling order.
func (o *GroupOrchestrator) snapshotTurn(s groupTurnSetup, t speakerTurn, highWater int64) preparedTurn {
	pending := pendingBccForTarget(o.groupID, s.names[t.member.ID])
	bcc := joinPendingBcc(pending)
	cursor := GetMemberCursor(t.member.ID)
	prompt := groupInjectionTextOrEmpty(o.groupID, t.member.ID, cursor, s.names, t.instruction, bcc) + t.sysPrompt
	if !o.firstTurnDone {
		o.firstTurnDone = true
		if o.commandInjection != "" {
			prompt = o.commandInjection + "\n\n" + prompt
		}
	}
	return preparedTurn{turn: t, prompt: prompt, pending: pending, highWater: highWater}
}

// executeTurn runs a prepared turn, then applies the clean-turn bookkeeping:
// advance the cursor to highWater and, on a clean turn, deliver the notes this
// speaker consumed and persist the notes it emitted. Mirrors the cursor rule
// (decision #69): a failed/cancelled turn advances nothing and keeps its notes.
func (o *GroupOrchestrator) executeTurn(ctx context.Context, s groupTurnSetup, p preparedTurn) groupMemberResult {
	res := s.runner(ctx, o.groupID, groupMemberTurn{MemberRowID: p.turn.member.ID, Prompt: p.prompt})
	if res.CancelReason != "" {
		// The user stopped the turn mid-speech. Delivered notes are NOT cleared
		// (the turn consumed nothing) — they re-send (decision #69).
		return res
	}
	advanceCursorOnSuccess(p.turn.member.ID, p.highWater, res)
	if res.Err == "" {
		ids := make([]int64, 0, len(p.pending))
		for _, pb := range p.pending {
			ids = append(ids, pb.ID)
		}
		deletePendingBcc(ids)
		o.storeSpeakerNotes(o.groupID, p.turn.member.ID, s.byID, s.byName)
	}
	return res
}

// groupNamesUser reports whether a group includes the human user. A parallel
// group must not: the user cannot speak concurrently, and reaching a User turn
// ends the whole round (handUserBack), which would silently drop the group's
// other members.
func groupNamesUser(g speakerGroup) bool {
	for _, t := range g.turns {
		if t.member.ID == groupUserTargetID {
			return true
		}
	}
	return false
}

// drainSpeakers is the ONE turn kernel both modes share. It runs queued GROUPS
// in order; when the queue empties it calls refill to obtain more — host mode
// runs the host turn there and returns its routed groups, free mode returns
// "done". A clean turn's output is offered to onSpoke so the mode can extend
// the queue (free mode appends the speaker's @-mentions; host mode passes nil,
// since its members cannot route).
//
// Groups run strictly one after another (group-serial), so the next group's
// snapshot sees every prior group's output. Within a PARALLEL group the members
// run concurrently from one shared snapshot (see runSpeakerGroup).
//
// The kernel owns the rules that must NOT drift between modes:
//   - groupCtx cancellation ⇒ a user-cancel result (checked before every
//     group AND before every refill, so a stop during the host turn is seen);
//   - naming the human user ⇒ hand the floor back and end the turn;
//   - a cancelled speaker turn ⇒ stop the WHOLE turn (the rest of the queue is
//     dropped; nothing runs after a stop);
//   - the /cb-* template reaching exactly one speaker (snapshotTurn's job).
func (o *GroupOrchestrator) drainSpeakers(
	ctx context.Context,
	s groupTurnSetup,
	queue []speakerGroup,
	refill func(ctx context.Context) ([]speakerGroup, bool, DrainResult),
	onSpoke func(ctx context.Context, sp speakerTurn, queue []speakerGroup, pending []speakerTurn) []speakerGroup,
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
		g := queue[0]
		queue = queue[1:]
		res, appended, stop := o.runSpeakerGroup(ctx, s, g, onSpoke)
		if stop {
			return res
		}
		queue = append(queue, appended...)
	}
}

// runSpeakerGroup runs one group and returns the resulting DrainResult, any
// groups the mode appended (free-mode relay), and whether the whole turn must
// stop. A sequential group runs its turns in order (each snapshot sees the
// previous turn's output); a parallel group snapshots every turn from the SAME
// group-start high-water, then runs them concurrently.
//
// A parallel group that names the human user is SPLIT (P7): the AI members keep
// the tag's `parallel` mode and run concurrently, then the floor is handed to
// the user and the round ends. The user cannot speak concurrently, and a User
// turn ends the round (handUserBack), so the user must come last — otherwise the
// members after it would be silently dropped.
func (o *GroupOrchestrator) runSpeakerGroup(
	ctx context.Context,
	s groupTurnSetup,
	g speakerGroup,
	onSpoke func(ctx context.Context, sp speakerTurn, queue []speakerGroup, pending []speakerTurn) []speakerGroup,
) (DrainResult, []speakerGroup, bool) {
	if !g.parallel {
		return o.runSequentialGroup(ctx, s, g, onSpoke)
	}
	if groupNamesUser(g) {
		ai := splitOffUser(g)
		var appended []speakerGroup
		if len(ai.turns) > 0 {
			res, app, stop := o.runParallelGroup(ctx, s, ai, onSpoke)
			if stop {
				// A cancelled AI member stops the whole turn; do not hand back.
				return res, app, true
			}
			appended = app
		}
		// Hand the floor to the user last. The AI members' relay appends are
		// dropped (the round is over), matching the sequential path when a User
		// turn is reached.
		return o.handUserBack(o.groupID), appended, true
	}
	return o.runParallelGroup(ctx, s, g, onSpoke)
}

// splitOffUser returns the group's AI members as a group, preserving its mode
// and dropping the human-user turn(s). Order among the members is preserved.
func splitOffUser(g speakerGroup) speakerGroup {
	ai := speakerGroup{parallel: g.parallel}
	for _, t := range g.turns {
		if t.member.ID == groupUserTargetID {
			continue
		}
		ai.turns = append(ai.turns, t)
	}
	return ai
}

// runSequentialGroup runs a group's turns one at a time. Each turn's snapshot is
// taken just before it runs, so it sees every previous turn's output.
func (o *GroupOrchestrator) runSequentialGroup(
	ctx context.Context,
	s groupTurnSetup,
	g speakerGroup,
	onSpoke func(ctx context.Context, sp speakerTurn, queue []speakerGroup, pending []speakerTurn) []speakerGroup,
) (DrainResult, []speakerGroup, bool) {
	var appended []speakerGroup
	for i, t := range g.turns {
		if ctx.Err() != nil {
			return DrainResult{CancelReason: cancelReasonUser}, appended, true
		}
		if t.member.ID == groupUserTargetID {
			return o.handUserBack(o.groupID), appended, true
		}
		p := o.snapshotTurn(s, t, GroupTimelineHighWater(o.groupID))
		r := o.executeTurn(ctx, s, p)
		if r.CancelReason != "" {
			return DrainResult{CancelReason: r.CancelReason}, appended, true
		}
		if r.Err == "" && onSpoke != nil {
			// Pass this group's NOT-YET-SPOKEN turns as `pending`: a member still
			// waiting later in the SAME group must not be re-queued (it would
			// speak twice). They are neither in the outer queue nor in `appended`.
			appended = onSpoke(ctx, t, appended, g.turns[i+1:])
		}
	}
	return DrainResult{}, appended, false
}

// maxParallelLaunches bounds how many members of a parallel group run at once.
// Each member is a separate agent process (hundreds of MB), so an unbounded
// fan-out on a 10-member group would spawn 10 processes simultaneously. This is
// a LAUNCH cap, not an injection cap: every member's prompt is still snapshotted
// from the group-start high-water BEFORE any of them runs (see runParallelGroup),
// so batching does not break "mutually non-referencing" — a later batch sees the
// same group-start snapshot as the first.
const maxParallelLaunches = 3

// runParallelGroup runs a group's turns CONCURRENTLY, at most maxParallelLaunches
// at a time. Every turn is snapshotted from the SAME group-start high-water
// BEFORE any of them runs (the snapshot-then-launch invariant), so no member
// sees a sibling's output this round — including members in a later batch, whose
// prompts were fixed before the first batch launched. Each member's cursor then
// advances to that high-water on success.
func (o *GroupOrchestrator) runParallelGroup(
	ctx context.Context,
	s groupTurnSetup,
	g speakerGroup,
	onSpoke func(ctx context.Context, sp speakerTurn, queue []speakerGroup, pending []speakerTurn) []speakerGroup,
) (DrainResult, []speakerGroup, bool) {
	highWater := GroupTimelineHighWater(o.groupID)
	prepared := make([]preparedTurn, 0, len(g.turns))
	for _, t := range g.turns {
		prepared = append(prepared, o.snapshotTurn(s, t, highWater))
	}

	var (
		mu      sync.Mutex
		cancel  string
		results = make([]groupMemberResult, len(prepared))
	)
	// Launch in batches of maxParallelLaunches. Each batch is awaited before the
	// next launches, bounding concurrent processes; the prompts were all fixed
	// above, so batching does not affect what any member sees.
	for start := 0; start < len(prepared); start += maxParallelLaunches {
		if ctx.Err() != nil {
			mu.Lock()
			if cancel == "" {
				cancel = cancelReasonUser
			}
			mu.Unlock()
			break
		}
		end := min(start+maxParallelLaunches, len(prepared))
		eg, egCtx := errgroup.WithContext(ctx)
		for i := start; i < end; i++ {
			eg.Go(func() error {
				r := o.executeTurn(egCtx, s, prepared[i])
				mu.Lock()
				results[i] = r
				if r.CancelReason != "" && cancel == "" {
					cancel = r.CancelReason
				}
				mu.Unlock()
				return nil
			})
		}
		_ = eg.Wait()
		// A user cancel stops the whole turn: do not launch further batches.
		mu.Lock()
		stopped := cancel != ""
		mu.Unlock()
		if stopped {
			break
		}
	}
	if cancel != "" {
		return DrainResult{CancelReason: cancel}, nil, true
	}
	// Free-mode relay: extend the queue from each clean speaker's output, in
	// group order (deterministic). `pending` is this group's OTHER members (all
	// of them — in a parallel group they are mutually non-referencing but still
	// must not be re-queued by a sibling's @), so a mention of a group peer does
	// not duplicate it.
	var appended []speakerGroup
	if onSpoke != nil {
		for i, t := range g.turns {
			if results[i].Err == "" {
				pending := make([]speakerTurn, 0, len(g.turns)-1)
				pending = append(pending, g.turns[:i]...)
				pending = append(pending, g.turns[i+1:]...)
				appended = onSpoke(ctx, t, appended, pending)
			}
		}
	}
	return DrainResult{}, appended, false
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
	// hostSysPrompt is the host's routing instruction; the host turn otherwise
	// runs through the SAME runSpeakerTurn path as a member (cursor + note
	// bookkeeping + command injection), so only the prompt differs.
	hostSysPrompt := BuildHostSystemPrompt(activeMemberNamesExcept(s.members, hostMemberID))
	refill := func(ctx context.Context) ([]speakerGroup, bool, DrainResult) {
		if round >= maxRounds {
			// Round cap reached: run the host once more purely for the summary.
			o.summarize(ctx, s, hostMemberID)
			return nil, false, DrainResult{}
		}
		round++
		// Host speaks and routes.
		hostMember := GroupMember{ID: hostMemberID, Name: s.names[hostMemberID]}
		hostRes := o.runSpeakerTurn(ctx, s, hostMember, "", hostSysPrompt)
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
				o.summarize(ctx, s, hostMemberID)
				return nil, false, DrainResult{Err: "host turn failed repeatedly"}
			}
			fb, ok := o.pickFallbackMember(hostMemberID, s.members)
			if !ok {
				return nil, false, DrainResult{Err: "host turn failed and no member could take over"}
			}
			return []speakerGroup{{turns: []speakerTurn{memberTurn(fb, "", s)}}}, true, DrainResult{}
		}
		// A successful host turn clears the failure streak.
		o.hostFailures = 0

		route := grouprouting.Parse(o.lastMemberOutput(groupID, hostMemberID))
		if route.End {
			// The end-signal turn already contains the summary (B2 prompt).
			return nil, false, DrainResult{}
		}

		groups := resolveSpeakerGroups(route, s.members, hostMemberID, func(m GroupMember, instruction string) speakerTurn {
			return memberTurn(m, instruction, s)
		})
		if len(groups) == 0 {
			// Parse failed or no valid speakers: fall back to round-robin.
			//
			// Two consecutive failures mean the host is not producing usable
			// routing at all — continuing would burn the whole round budget on
			// a broken loop. Abort and finalize (decision #56).
			o.parseFailures++
			if o.parseFailures >= maxConsecutiveParseFailures {
				slog.Warn("group: aborting after consecutive parse failures",
					"group", groupID, "failures", o.parseFailures)
				o.summarize(ctx, s, hostMemberID)
				return nil, false, DrainResult{}
			}
			fb, ok := o.pickFallbackMember(hostMemberID, s.members)
			if !ok {
				return nil, false, DrainResult{}
			}
			return []speakerGroup{{turns: []speakerTurn{memberTurn(fb, "", s)}}}, true, DrainResult{}
		}
		// A usable routing decision resets the failure streak.
		o.parseFailures = 0

		// Members speak in group order; within a group, in member order. A
		// parallel group's members all snapshot from the group-start high-water
		// (see runSpeakerGroup), so none sees a sibling's output this round.
		return groups, true, DrainResult{}
	}

	return o.drainSpeakers(ctx, s, nil, refill, nil)
}

// resolveSpeakerGroups maps a parsed route's public mention GROUPS to member
// groups, preserving each tag's mode and instruction. Members are matched by
// row id then name; unknown/left targets and excludeID are dropped; a member
// claimed by an earlier group is not re-listed (it would speak twice). A group
// left with no unclaimed members is dropped.
//
// turnFor builds each member's queued turn. It MUST differ by mode: host mode
// passes memberTurn (the host's directive + the "member, not host" prompt), free
// mode passes a builder using BuildFreeMemberSystemPrompt and NO instruction —
// a free member must be told it may @ others (its prompt teaches the mention
// tag), and the user's mention body is already in the injected user line, so
// repeating it as a host directive is both wrong (there is no host) and
// redundant. Passing memberTurn in free mode told the seeded member NOT to emit
// mention tags, which broke the relay at its first step.
func resolveSpeakerGroups(route grouprouting.Result, members []GroupMember, excludeID string, turnFor func(GroupMember, string) speakerTurn) []speakerGroup {
	if !route.Found {
		return nil
	}
	byID, byName := memberLookups(members)
	seen := map[string]bool{}
	out := make([]speakerGroup, 0, len(route.Groups))
	for _, gr := range route.Groups {
		g := speakerGroup{parallel: gr.Parallel}
		for _, tgt := range gr.Members {
			m, ok := lookupMember(tgt, byID, byName)
			if !ok || seen[m.ID] || m.ID == excludeID {
				continue
			}
			if m.Left && m.ID != groupUserTargetID {
				continue
			}
			seen[m.ID] = true
			g.turns = append(g.turns, turnFor(m, gr.Instruction))
		}
		if len(g.turns) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// memberTurn builds the queued turn for a member in HOST mode: the host's public
// directive plus the member system prompt (so a member knows it is not the
// host). A routed member and a round-robin fallback member get the SAME prompt
// — without the system prompt a fallback member would not know it must not
// route, which is exactly how members came to declare themselves the chair.
func memberTurn(m GroupMember, instruction string, s groupTurnSetup) speakerTurn {
	return speakerTurn{
		member:      m,
		instruction: instruction,
		sysPrompt:   BuildMemberSystemPrompt(memberRoster(s.members), s.names[m.ID]),
	}
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

	// Seed the queue from the user's message: the @-mentioned members grouped by
	// their mention tags (a `mode="parallel"` tag seeds a parallel group), or
	// every active member as one sequential group when nobody is mentioned. The
	// /cb-* command injection is NOT handled here — the snapshot phase gives it
	// to whichever speaker goes first (design §13.3 #F16).
	queue := o.freeInitialGroups(s)
	if len(queue) == 0 {
		return DrainResult{}
	}

	// The queue is self-extending: free mode has no router, so an empty queue
	// means the relay is over.
	refill := func(context.Context) ([]speakerGroup, bool, DrainResult) {
		return nil, false, DrainResult{}
	}
	onSpoke := func(_ context.Context, sp speakerTurn, q []speakerGroup, pending []speakerTurn) []speakerGroup {
		return o.appendFreeTargets(q, pending, groupID, sp.member.ID, s.members, s.names)
	}
	return o.drainSpeakers(ctx, s, queue, refill, onSpoke)
}

// freeInitialGroups resolves the free-mode seed as groups. When the user's
// message carries mention tags, their per-tag mode and order are honored (P5);
// otherwise every active member forms one sequential group in roster order.
//
// The turn builder is freeTurn: a seeded free member must get the FREE-mode
// system prompt (which teaches it to @ others and to hand the floor back) and no
// host instruction. Reusing memberTurn here — as an earlier revision did — told
// the member NOT to emit mention tags (the host-mode prompt's rule), which
// breaks the free relay at its very first step, and injected the user's mention
// body as a phantom "host directive".
func (o *GroupOrchestrator) freeInitialGroups(s groupTurnSetup) []speakerGroup {
	freeTurn := func(m GroupMember, _ string) speakerTurn {
		return speakerTurn{
			member:    m,
			sysPrompt: BuildFreeMemberSystemPrompt(memberRoster(s.members), s.names[m.ID]),
		}
	}
	// The group's "并发执行" switch (free mode only) makes the USER's seed run
	// concurrently: every member the user @-names — or the whole active roster
	// when they name nobody — forms ONE parallel group. It is read here, at
	// drain time, so the direct-send and queued paths agree (the setting is the
	// single source of truth; the message text carries no mode for user cards).
	//
	// It NEVER touches an AGENT's own `mode="parallel"` tags: those ride in the
	// agent's output and are honored by appendFreeTargets regardless of this
	// switch (the switch governs only what the USER seeds).
	parallelSeed := GetGroupParallelDefault(o.groupID)

	route := grouprouting.Parse(o.userMessage)
	if groups := resolveSpeakerGroups(route, s.members, "", freeTurn); len(groups) > 0 {
		if parallelSeed {
			return []speakerGroup{mergeParallel(groups)}
		}
		return groups
	}
	active := activeMembers(s.members)
	if len(active) == 0 {
		return nil
	}
	g := speakerGroup{parallel: parallelSeed}
	for _, m := range active {
		g.turns = append(g.turns, freeTurn(m, ""))
	}
	return []speakerGroup{g}
}

// mergeParallel flattens the user's seeded groups into ONE parallel group,
// preserving member order. Cross-group de-duplication is already applied by
// resolveSpeakerGroups (a member appears in at most one group), so flattening
// cannot make anyone speak twice. Used by the free-mode "并发执行" switch.
func mergeParallel(groups []speakerGroup) speakerGroup {
	g := speakerGroup{parallel: true}
	for _, gr := range groups {
		g.turns = append(g.turns, gr.turns...)
	}
	return g
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
	for _, m := range activeMembers(members) {
		if m.ID == hostMemberID {
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

// storeSpeakerNotes persists the private notes a speaker emitted in THIS turn,
// so each note is delivered on its target's next turn (the group_pending_bcc
// contract). It re-parses the speaker's own output (the orchestrator does not
// carry a pre-parsed route) and delegates validation/storage to storeRouteNotes.
// Called by runSpeakerTurn for EVERY speaker — host and member, both modes — so
// the note path is identical everywhere.
func (o *GroupOrchestrator) storeSpeakerNotes(groupID, speakerID string, byID, byName map[string]GroupMember) {
	output := o.lastMemberOutput(groupID, speakerID)
	if output == "" {
		return
	}
	storeRouteNotes(groupID, grouprouting.Parse(output), speakerID, byID, byName)
}

// storeRouteNotes persists a parsed route's private notes, validating each
// target against the roster: an unknown or left target is dropped (it could
// never be delivered — storing it would be silent loss plus unbounded growth of
// group_pending_bcc), and a self-directed note is dropped (it would be
// delivered on the speaker's own next turn).
//
// The target is resolved by ROW ID first, then by display name (lookupMember).
// A USER's note carries a member ROW ID (the frontend writes ids), while an
// agent writes a display name; resolving by name alone silently discarded every
// user note as "unknown target". The note is keyed by the RESOLVED member name,
// which is exactly what the delivery lookup (pendingBccForTarget(names[t.ID]))
// uses.
func storeRouteNotes(groupID string, route grouprouting.Result, speakerID string, byID, byName map[string]GroupMember) {
	for _, e := range route.Bcc {
		for _, target := range e.Targets {
			m, ok := lookupMember(target, byID, byName)
			if !ok || m.ID == speakerID {
				continue
			}
			// A LEFT member never speaks again, so a note stored for one could
			// never be delivered — it would sit in group_pending_bcc forever.
			// (byID includes left members, so the check must be explicit; the
			// name map already excludes them. The user sentinel is never left.)
			if m.Left && m.ID != groupUserTargetID {
				continue
			}
			addPendingBcc(groupID, m.Name, e.Content)
		}
	}
}

// storeUserNotes persists the private notes a USER emitted in this turn's
// message, so each is delivered on its target's next turn — the same
// group_pending_bcc contract agent notes use (decision #97), reached through the
// same storeRouteNotes path so validation cannot drift.
//
// The user is not a speaker, so its notes are stored separately at turn start.
// speakerID is the reserved user sentinel: storeRouteNotes drops a note whose
// target resolves to the speaker, which correctly drops a note the user
// addressed to themselves (they read it in the UI card, not through a prompt).
//
// Honored in BOTH modes: routing (@) is a host-mode no-op (decision #47), but a
// private note is a payload, orthogonal to who routes the discussion — the user
// can privately brief a member regardless of mode.
func (o *GroupOrchestrator) storeUserNotes(groupID string, byID, byName map[string]GroupMember) {
	if o.userMessage == "" {
		return
	}
	storeRouteNotes(groupID, grouprouting.Parse(o.userMessage), groupUserTargetID, byID, byName)
}

// freeInitialGroups is the free-mode seed (see runFreeLoop); it lives next to
// runFreeLoop. resolveSpeakerGroups is shared by both modes.

// freeRelay carries the per-speaker state appendFreeTargets needs to resolve and
// de-duplicate the @-mentions a free-mode speaker emitted.
type freeRelay struct {
	groupID   string
	speakerID string
	members   []GroupMember
	names     map[string]string
	byID      map[string]GroupMember
	byName    map[string]GroupMember
	// waiting is the set of member row ids already queued (outer queue + the
	// current group's not-yet-spoken turns). A target in this set is skipped.
	waiting map[string]bool
}

// appendFreeTargets parses the just-finished speaker's output for @-mentions and
// appends the valid, not-already-queued targets to the queue's tail as GROUPS
// (preserving each tag's mode). Returns the (possibly extended) queue.
//
// `queue` is the remaining OUTER queue (groups not yet started) and `pending`
// is the current group's not-yet-spoken turns. Both count as "waiting" for the
// dedup rule: a member still queued anywhere must not be appended again. Missing
// `pending` is what let a member named by a peer in the SAME group be queued
// twice (it spoke twice) — the outer queue does not contain a group peer that is
// simply later in the current group.
//
// Dedup rule (decision #F9): only targets already WAITING are skipped. A member
// that has already spoken may be @-ed again and will speak again — this is what
// makes unlimited relay possible (A@B, B@A loops).
//
// Invalid targets (decision #F7): a self-mention and a left-member mention are
// dropped silently (model slips not worth surfacing); an unknown NAME emits a
// visible system notice (it usually means a member was removed/renamed, or the
// model hallucinated — the user should know).
func (o *GroupOrchestrator) appendFreeTargets(queue []speakerGroup, pending []speakerTurn, groupID, speakerID string, members []GroupMember, names map[string]string) []speakerGroup {
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
	for _, g := range queue {
		for _, st := range g.turns {
			waiting[st.member.ID] = true
		}
	}
	for _, st := range pending {
		waiting[st.member.ID] = true
	}
	rl := freeRelay{
		groupID: groupID, speakerID: speakerID, members: members, names: names,
		byID: byID, byName: byName, waiting: waiting,
	}

	var unknown []string
	for _, gr := range route.Groups {
		g, miss := o.freeGroupFromRoute(gr, &rl)
		unknown = append(unknown, miss...)
		if len(g.turns) > 0 {
			queue = append(queue, g)
		}
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

// freeGroupFromRoute resolves ONE mention group's targets to a speakerGroup,
// applying the free-mode validity rules (unknown → reported, self/left → silent)
// and the waiting-set dedup (marking each accepted member as waiting). Returns
// the group plus the unknown names it saw.
func (o *GroupOrchestrator) freeGroupFromRoute(gr grouprouting.MentionGroup, rl *freeRelay) (speakerGroup, []string) {
	g := speakerGroup{parallel: gr.Parallel}
	var unknown []string
	for _, tgt := range gr.Members {
		m, ok := lookupMember(tgt, rl.byID, rl.byName)
		if !ok {
			unknown = append(unknown, tgt)
			continue
		}
		if m.ID == rl.speakerID {
			continue // self-mention: drop silently
		}
		if m.Left && m.ID != groupUserTargetID {
			continue // left member: drop silently
		}
		if rl.waiting[m.ID] {
			continue // already queued: do not duplicate
		}
		rl.waiting[m.ID] = true
		g.turns = append(g.turns, speakerTurn{
			member:    m,
			sysPrompt: BuildFreeMemberSystemPrompt(memberRoster(rl.members), rl.names[m.ID]),
		})
	}
	return g, unknown
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
func (o *GroupOrchestrator) summarize(ctx context.Context, s groupTurnSetup, hostMemberID string) {
	hostMember := GroupMember{ID: hostMemberID, Name: s.names[hostMemberID]}
	o.runSpeakerTurn(ctx, s, hostMember, "",
		BuildHostSummaryPrompt(activeMemberNamesExcept(s.members, hostMemberID)))
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
	byName[groupUserTarget()] = GroupMember{ID: groupUserTargetID, Name: groupUserTarget()}
	return byID, byName
}

// lookupMember resolves a raw mention target to a member, trying the row id
// first (a user's @-mention carries the row id) then the display name (an agent
// writes the name). Both the host's routing resolution and the free-mode relay
// use this, so the "how is a target matched" rule lives in one place.
func lookupMember(tgt string, byID, byName map[string]GroupMember) (GroupMember, bool) {
	t := strings.TrimSpace(tgt)
	if m, ok := byID[t]; ok {
		return m, true
	}
	m, ok := byName[t]
	return m, ok
}

// memberInfos converts the group's members into the HostMemberInfo shape the
// prompt builders use (name + specialty + left marker). excludeID, when
// non-empty, drops that member (the host must not be offered as a routing
// target: naming itself caused the self-route loop). withUser appends the human
// user as a routable participant (reserved name "User") — used by the host
// prompt, which must be able to call on the user, but not by a member's own
// roster (a member already knows the user is in the room via othersLine).
//
// Left members are INCLUDED (marked, not dropped): their past speech is still
// on the timeline, so omitting them would leave speech from a name that cannot
// be addressed. Marking them is what stops the host routing to someone gone
// (which would silently fall back to round-robin).
func memberInfos(members []GroupMember, excludeID string, withUser bool) []HostMemberInfo {
	out := make([]HostMemberInfo, 0, len(members)+1)
	for _, m := range members {
		if m.ID == excludeID {
			continue
		}
		out = append(out, HostMemberInfo{
			Name:      m.Name,
			Specialty: GetAgentSpecialty(m.AgentID),
			Left:      m.Left,
		})
	}
	if withUser {
		out = append(out, HostMemberInfo{Name: groupUserTarget()})
	}
	return out
}

// memberRoster is the full roster (no exclusions, no appended user) used by the
// member-facing system prompts.
func memberRoster(members []GroupMember) []HostMemberInfo {
	return memberInfos(members, "", false)
}

// activeMemberNamesExcept is the host's routable list: every member but the host
// itself, plus the human user.
func activeMemberNamesExcept(members []GroupMember, hostMemberID string) []HostMemberInfo {
	return memberInfos(members, hostMemberID, true)
}

// activeMembers returns the members that can still speak (not left), in roster
// order. Shared by the free-mode seed and the round-robin fallback so the
// "who is eligible" rule lives in one place.
func activeMembers(members []GroupMember) []GroupMember {
	out := make([]GroupMember, 0, len(members))
	for _, m := range members {
		if !m.Left {
			out = append(out, m)
		}
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
	defer recoverGroupDrainPanic(groupID)

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
func recoverGroupDrainPanic(groupID string) {
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
