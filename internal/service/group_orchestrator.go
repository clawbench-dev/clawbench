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
	// drained message). It is prepended to the FIRST host turn only, so the host
	// executes the built-in command once and its spoken result enters the
	// timeline for the members to discuss. Empty = the message was not a
	// built-in command (or no renderer is wired).
	commandInjection string
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
	// A built-in /cb-* command rides in the HOST's first prompt only: the host
	// (which owns routing and speaks to the group) runs the command and reports
	// the result as its speech, and the members discuss that. It is NOT injected
	// into the timeline content (the bubble keeps the user's literal text) and
	// NOT injected to members (the API contract is noise to them, decision #9).
	o.commandInjection = renderGroupCommandForHost(row.Content, o.project, groupID)
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
	res := o.runRounds(groupCtx, groupID)
	finalizeGroupOrphans(groupID)
	triggerChatSummarization(context.Background(), groupID)
	return res
}

// runRounds is the host/member decision loop, without the per-turn
// finalization (see runLoop). groupCtx cancellation is reported as a user
// cancel.
func (o *GroupOrchestrator) runRounds(groupCtx context.Context, groupID string) DrainResult {
	// Resolve the host and member roster.
	hostMemberID := GetGroupHostMember(groupID)
	if hostMemberID == "" {
		return DrainResult{Err: fmt.Sprintf("group %s has no host member", groupID)}
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return DrainResult{Err: fmt.Sprintf("list group members: %v", err)}
	}
	names := memberNameMap(members)
	maxRounds := GetGroupMaxRounds(groupID)

	// Protect every member's ACP connection for the duration of the turn: the
	// idle sweep keys off the member row id, which is otherwise never "running"
	// (decision #73). Registered here (after the roster is known) and cleared on
	// return, so it covers every exit path including panics.
	activeMemberIDs := make([]string, 0, len(members))
	for _, m := range members {
		activeMemberIDs = append(activeMemberIDs, m.ID)
	}
	markGroupMembersActive(activeMemberIDs)
	defer clearGroupMembersActive(activeMemberIDs)

	runner := o.runTurn
	if runner == nil {
		runner = o.defaultRunner
	}

	// Clean up any orphaned streaming rows from a previous crashed group turn
	// before we start writing new ones (C1 residual).
	finalizeGroupOrphans(groupID)

	// 3. Loop.
	for round := 0; round < maxRounds; round++ {
		if groupCtx.Err() != nil {
			return DrainResult{CancelReason: cancelReasonUser}
		}
		// Host speaks and routes.
		hostCursor := GetMemberCursor(hostMemberID)
		hostPrompt := o.hostPrompt(hostMemberID, hostCursor, names, members)
		// Prepend the rendered /cb-* template to the FIRST host turn only
		// (decision #79 revision): the host executes the built-in command once
		// and its spoken result enters the timeline for the members to discuss.
		// Injecting on every round would re-run the command each round.
		if round == 0 && o.commandInjection != "" {
			hostPrompt = o.commandInjection + "\n\n" + hostPrompt
		}
		hostPreH := GroupTimelineHighWater(groupID)
		hostRes := runner(groupCtx, groupID, groupMemberTurn{MemberRowID: hostMemberID, Prompt: hostPrompt, IsHost: true})
		if hostRes.CancelReason != "" {
			// The user stopped the turn while the host was speaking. Do NOT fall
			// back to a member turn — that would keep running after a stop.
			return DrainResult{CancelReason: hostRes.CancelReason}
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
				o.summarize(groupCtx, runner, hostMemberID, names, members)
				return DrainResult{Err: "host turn failed repeatedly"}
			}
			if !o.speakNextMember(groupCtx, runner, members, names, hostMemberID) {
				return DrainResult{Err: "host turn failed and no member could take over"}
			}
			continue
		}
		// A successful host turn clears the failure streak.
		o.hostFailures = 0
		advanceCursorOnSuccess(hostMemberID, hostPreH, hostRes)

		route := grouprouting.Parse(o.lastHostOutput(groupID, hostMemberID))
		if route.End {
			// The end-signal turn already contains the summary (B2 prompt).
			return DrainResult{}
		}

		targets := o.resolveTargets(route, members, hostMemberID)
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
				o.summarize(groupCtx, runner, hostMemberID, names, members)
				return DrainResult{}
			}
			if !o.speakNextMember(groupCtx, runner, members, names, hostMemberID) {
				return DrainResult{}
			}
			continue
		}
		// A usable routing decision resets the failure streak.
		o.parseFailures = 0

		// Persist this round's private notes BEFORE dispatching anyone. A note
		// addressed to a participant who is not named this round stays in the
		// table and is delivered with that participant's next turn, so the host
		// can "hand everyone a word, then call on one player first" (the
		// Who-Is-The-Spy setup). Delivery deletes the row; a failed/cancelled
		// turn keeps it for the next attempt (decision #69 semantics).
		for _, e := range route.Bcc {
			for _, name := range e.Targets {
				addPendingBcc(groupID, name, e.Content)
			}
		}

		// Members speak in order; each sees the previous speakers' output.
		for _, t := range targets {
			if groupCtx.Err() != nil {
				return DrainResult{CancelReason: cancelReasonUser}
			}
			// The user is a participant with no AI turn: naming them hands the
			// floor back. End the round (do NOT run the remaining targets) and
			// leave a system line so the timeline shows why it stopped.
			if t.ID == groupUserTargetID {
				// The user reads notes in the UI card, not through an injected
				// prompt — clear the bookkeeping so they are not re-sent.
				deletePendingBccForTarget(groupID, groupUserTarget)
				writeGroupSystemMessage(o.project, groupID,
					i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn"))
				return DrainResult{}
			}
			memberCursor := GetMemberCursor(t.ID)
			// The notes accumulated for THIS member (possibly from an earlier
			// round that did not name it).
			pending := pendingBccForTarget(groupID, names[t.ID])
			bcc := joinPendingBcc(pending)
			prompt := groupInjectionTextOrEmpty(groupID, t.ID, memberCursor, names, route.Instruction, bcc) +
				BuildMemberSystemPrompt(memberRoster(members), names[t.ID])
			preH := GroupTimelineHighWater(groupID)
			res := runner(groupCtx, groupID, groupMemberTurn{MemberRowID: t.ID, Prompt: prompt})
			if res.CancelReason != "" {
				// The user stopped the turn mid-speech. Stop the whole turn so
				// the remaining targets do not run after a stop. Delivered notes
				// are NOT cleared (the turn consumed nothing) — they re-send.
				return DrainResult{CancelReason: res.CancelReason}
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
		}
	}

	// 4. Round cap reached: run the host once more to produce a summary.
	o.summarize(groupCtx, runner, hostMemberID, names, members)
	return DrainResult{}
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

// lastHostOutput returns the host's most recent speech text on the group
// timeline (used to parse its routing decision).
func (o *GroupOrchestrator) lastHostOutput(groupID, hostMemberID string) string {
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].AgentID == hostMemberID && msgs[i].Role == "assistant" {
			return ExtractPlainText(msgs[i].Content)
		}
	}
	return ""
}

// resolveTargets maps the host's named speakers to active member rows, in the
// order named. Unknown/left names are dropped, and so is the host itself — the
// host must not be able to route to itself, which would turn every "member
// turn" into another host turn and starve the real members (the reported "only
// the host ever speaks" defect).
func (o *GroupOrchestrator) resolveTargets(route grouprouting.Result, members []GroupMember, hostMemberID string) []GroupMember {
	if !route.Found {
		return nil
	}
	byName := map[string]GroupMember{}
	for _, m := range members {
		if m.Left || m.ID == hostMemberID {
			continue
		}
		byName[strings.TrimSpace(m.Name)] = m
	}
	// The human user is routable under the reserved name. It has no member row
	// (no backend, no AI turn); the sentinel id marks it for the loop below,
	// which stops and waits instead of running a turn.
	byName[groupUserTarget] = GroupMember{ID: groupUserTargetID, Name: groupUserTarget}
	out := make([]GroupMember, 0, len(route.Speakers))
	seen := map[string]bool{}
	for _, name := range route.Speakers {
		m, ok := byName[strings.TrimSpace(name)]
		if !ok || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

// speakNextMember runs one round-robin member turn (fallback path), skipping
// the host so a fallback can never re-run the host. Returns false when no
// active non-host member exists.
//
// The pick ROTATES via o.fallbackIdx (decision #56): when the host's routing is
// repeatedly unparseable, always choosing the first member would turn the
// discussion into that member's monologue. The index advances past whatever was
// picked and wraps, skipping left members and the host.
func (o *GroupOrchestrator) speakNextMember(ctx context.Context, runner groupTurnRunner, members []GroupMember, names map[string]string, hostMemberID string) bool {
	eligible := make([]GroupMember, 0, len(members))
	for _, m := range members {
		if m.Left || m.ID == hostMemberID {
			continue
		}
		eligible = append(eligible, m)
	}
	if len(eligible) == 0 {
		return false
	}
	if o.fallbackIdx >= len(eligible) {
		o.fallbackIdx = 0
	}
	m := eligible[o.fallbackIdx]
	o.fallbackIdx = (o.fallbackIdx + 1) % len(eligible)

	cursor := GetMemberCursor(m.ID)
	prompt := groupInjectionTextOrEmpty(o.groupID, m.ID, cursor, names, "", "")
	preH := GroupTimelineHighWater(o.groupID)
	res := runner(ctx, o.groupID, groupMemberTurn{MemberRowID: m.ID, Prompt: prompt})
	// Same cursor rule as the routed and host paths (decision #69 + design
	// §5.1): advance to the PRE-speech high-water, and only on a clean turn.
	advanceCursorOnSuccess(m.ID, preH, res)
	return true
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
		AgentID:           agentID,
		ChatReq:           req,
		FileDir:           resolveFileDir(project),
		DrainOnFinalize:   true,
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

// activeMemberNamesExcept returns the members the host may route to, with the
// metadata that makes routing sensible (decision #39/#41): every member EXCEPT
// the host itself, each carrying its specialty and whether it has left.
//
// Left members are INCLUDED (marked, not dropped): their past speech is still
// on the timeline, so omitting them would make the host see speech from a name
// it cannot address. Marking them is what stops the host from routing to
// someone who is gone (which would silently fall back to round-robin).
//
// The host must never be offered as a routing target (see resolveTargets).
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
