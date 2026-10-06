package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"clawbench/internal/ai"
	"clawbench/internal/grouprouting"
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
type groupMemberResult struct {
	Err string
}

// groupTurnRunner runs one member's turn. Production wires it to runTurn with a
// TurnSpec whose SessionID is the member row and TimelineSessionID the group.
// It is a field on the orchestrator so tests can inject a scripted runner.
type groupTurnRunner func(ctx context.Context, groupID string, turn groupMemberTurn) groupMemberResult

// GroupOrchestrator runs one group turn.
type GroupOrchestrator struct {
	groupID string
	project string
	// queueID / senderClientID identify the sending device's optimistic bubble
	// so the user_message echo lets it adopt the DB id instead of rendering a
	// duplicate. Empty for turns not initiated by a client send.
	queueID        string
	senderClientID string
	// runTurn is injectable for tests; nil means the production runTurn path.
	runTurn groupTurnRunner
	// fallbackIdx is the round-robin cursor for the parse-failure fallback, so
	// consecutive failures address DIFFERENT members instead of the same first
	// one every time (decision #56).
	fallbackIdx int
	// parseFailures counts consecutive unparseable host routings. Reset by a
	// usable decision; reaching maxConsecutiveParseFailures ends the turn.
	parseFailures int
}

// maxConsecutiveParseFailures is how many unparseable host routings in a row
// end the discussion early (decision #56). Two is enough to tell "the host
// fumbled once" from "the host cannot route at all".
const maxConsecutiveParseFailures = 2

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

// RunGroupTurn executes one full group turn for userMessage.
//
// It is intentionally sequential (v1): members speak one at a time so each sees
// the previous speakers' same-round output and only one streaming row is written
// to the group timeline at any moment.
func (o *GroupOrchestrator) RunGroupTurn(ctx context.Context, userMessage string, files []model.FileEntry) error {
	groupID := o.groupID

	// Register the group as running with a cancelable context so a frontend
	// "stop" on the group session (CancelSession(groupID)) propagates into the
	// member turns, whose TurnSpec.Ctx is derived from groupCtx. Without this a
	// stop would only cancel a group placeholder runner that does not exist.
	groupCtx, groupCancel := context.WithCancel(ctx)
	defer groupCancel()
	RegisterExternalExecution(groupCtx, groupCancel, groupID)
	SetSessionRunning(groupID, true, true)
	defer SetSessionRunning(groupID, false, true)

	// 1. Persist the user message to the group timeline and broadcast it.
	// Attachments are stored on the row (decision #65): the bubble renders them
	// via `files`, and the injection layer renders them as prompt prefixes.
	msgID, err := AddChatMessageWithAgent(o.project, groupBackend(groupID), groupID, "user", userMessage, files, false, "", "")
	if err != nil {
		return fmt.Errorf("persist group user message: %w", err)
	}
	emitGroupUserMessage(groupID, msgID, userMessage, o.queueID, o.senderClientID)

	// 2. Run the loop, then emit the terminal event ourselves: this is the
	// STANDALONE entry point (a client send that claimed the session), so nobody
	// else will send "done". The drain entry point (RunGroupTurnDrain) skips
	// this and lets RunDrainLoop own the terminal.
	o.runLoop(groupCtx, groupID)
	emitGroupTerminal(groupID)
	return nil
}

// RunGroupTurnDrain runs one group turn for a message the drain loop already
// materialized. It differs from RunGroupTurn in exactly two ways, both required
// for the drain loop to own the turn lifecycle (decision #45/#58):
//
//   - It does NOT persist or announce the user message: ClaimNextAndMaterialize
//     already wrote the chat_history row and emitted user_message, so doing it
//     again would duplicate both.
//   - It does NOT emit a terminal event: RunDrainLoop decides when the whole
//     queue is drained and sends "done" exactly once, then.
//
// It returns a DrainResult the loop uses to decide whether to continue.
func RunGroupTurnDrain(ctx context.Context, groupID string, msgID int64, row QueuedRow, runner groupTurnRunner) DrainResult {
	o := NewGroupOrchestrator(groupID)
	if runner != nil {
		o.runTurn = runner
	}
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

// runLoop executes the host/member rounds and returns the outcome WITHOUT
// emitting any terminal event or touching the session's running state — both
// belong to the caller (standalone vs drain). groupCtx cancellation is reported
// as a user cancel.
func (o *GroupOrchestrator) runLoop(groupCtx context.Context, groupID string) DrainResult {
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
		hostRes := runner(groupCtx, groupID, groupMemberTurn{MemberRowID: hostMemberID, Prompt: hostPrompt, IsHost: true})
		if hostRes.Err == "" {
			// Advance only on success: the cursor means "has processed up to
			// here". A failed turn processed nothing, so advancing would make
			// the injected context permanently unreachable (decision #69).
			SetMemberCursor(hostMemberID, GroupTimelineHighWater(groupID))
		} else {
			slog.Warn("group: host turn failed", "group", groupID, "err", hostRes.Err)
			// Host failed: try round-robin once, then stop.
			if !o.speakNextMember(groupCtx, runner, members, names, hostMemberID) {
				return DrainResult{Err: "host turn failed and no member could take over"}
			}
			continue
		}

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

		// Members speak in order; each sees the previous speakers' output.
		for _, t := range targets {
			if groupCtx.Err() != nil {
				return DrainResult{CancelReason: cancelReasonUser}
			}
			memberCursor := GetMemberCursor(t.ID)
			prompt := groupInjectionTextOrEmpty(groupID, t.ID, memberCursor, names, route.Instruction)
			res := runner(groupCtx, groupID, groupMemberTurn{MemberRowID: t.ID, Prompt: prompt})
			if res.Err == "" {
				// Advance only on success (decision #69): a failed member
				// processed nothing, so the messages it just received must be
				// re-injected next time instead of being lost. The failed
				// turn's residue stays on the timeline for the user.
				SetMemberCursor(t.ID, GroupTimelineHighWater(groupID))
			} else {
				slog.Warn("group: member turn failed", "group", groupID, "member", t.ID, "err", res.Err)
			}
		}
	}

	// 4. Round cap reached: run the host once more to produce a summary.
	o.summarize(groupCtx, runner, hostMemberID, names, members)
	return DrainResult{}
}

// emitGroupTerminal finalizes a group turn for the frontend. runTurn
// deliberately emits no terminal WS event (the caller owns it), so without this
// the last member's streaming bubble would stay "streaming" forever and the
// session would never leave the running state. Mirrors the single-agent
// handler: clear running first (so a loadHistory triggered by "done" sees the
// terminal state), then emit "done" and the terminal "completed" update.
//
// It is a var seam so tests can observe the terminal contract without a WS hub.
//
// Before announcing the terminal state it finalizes any streaming row still
// open on the group timeline. That is a safety net for a Finalize that itself
// failed (a DB write error): without it the row stays streaming=1 forever and
// the next reload renders a phantom bubble that never ends. It is NOT a race
// guard — every caller runs after the turn's runner returned, so no member turn
// is still in flight (design §12.7(19)).
var emitGroupTerminal = func(groupID string) {
	finalizeOrphanedStreamingMessages(groupID, "interrupt")
	// Summarize the whole discussion ONCE here (decision #55). Member turns
	// deliberately skip summarization, so without this a group turn would
	// produce no reading summaries at all.
	triggerChatSummarization(context.Background(), groupID)
	SetSessionRunning(groupID, false, true)
	ws.EmitToSession(groupID, ai.StreamEvent{Type: eventTypeDone})
	// Broadcast the terminal status so every client clears the running flag,
	// even those that missed the stream-level "done".
	EmitSessionEventWSOnly(groupID, "completed", false)
}

// summarize runs the host once more with the summary-only prompt. The host's
// routing tags in this turn are ignored (never parsed).
func (o *GroupOrchestrator) summarize(ctx context.Context, runner groupTurnRunner, hostMemberID string, names map[string]string, members []GroupMember) {
	cursor := GetMemberCursor(hostMemberID)
	prompt := groupInjectionTextOrEmpty(o.groupID, hostMemberID, cursor, names, "") + BuildHostSummaryPrompt(activeMemberNamesExcept(members, hostMemberID))
	runner(ctx, o.groupID, groupMemberTurn{MemberRowID: hostMemberID, Prompt: prompt, IsHost: true})
	SetMemberCursor(hostMemberID, GroupTimelineHighWater(o.groupID))
}

// hostPrompt builds the host's turn prompt: the incremental group context plus
// the host instruction (routing rules). The selectable list EXCLUDES the host
// itself, so the model cannot name itself (which caused the self-route loop).
func (o *GroupOrchestrator) hostPrompt(hostMemberID string, cursor int64, names map[string]string, members []GroupMember) string {
	ctxText := groupInjectionTextOrEmpty(o.groupID, hostMemberID, cursor, names, "")
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
	prompt := groupInjectionTextOrEmpty(o.groupID, m.ID, cursor, names, "")
	res := runner(ctx, o.groupID, groupMemberTurn{MemberRowID: m.ID, Prompt: prompt})
	// Advance only on success (decision #69) — same rule as the routed and
	// host paths. The round-robin fallback is still "a turn", so a failed
	// one must not swallow the context it was handed.
	if res.Err == "" {
		SetMemberCursor(m.ID, GroupTimelineHighWater(o.groupID))
	} else {
		slog.Warn("group: fallback member turn failed", "group", o.groupID, "member", m.ID, "err", res.Err)
	}
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
// "done" (emitted once by emitGroupTerminal) is a different event: it also
// clears loading and ends the run, so it cannot be used per member.
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
	return groupMemberResult{Err: res.Err}
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

// RunGroupTurnForSession runs one group turn for the given group session. It is
// the entry point used by the HTTP handler's group delegation. queueID and
// senderClientID are the sending device's optimistic-bubble identifiers (may be
// empty); they travel on the user_message echo so the sender adopts the DB id.
// files are the message's attachments (decision #65) — persisted on the user
// message and rendered into each member's injected context.
func RunGroupTurnForSession(ctx context.Context, groupID, userMessage string, files []model.FileEntry, queueID, senderClientID string) error {
	o := NewGroupOrchestrator(groupID)
	o.queueID = queueID
	o.senderClientID = senderClientID
	return o.RunGroupTurn(ctx, userMessage, files)
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
func activeMemberNamesExcept(members []GroupMember, hostMemberID string) []HostMemberInfo {
	out := make([]HostMemberInfo, 0, len(members))
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
	return out
}

func groupInjectionTextOrEmpty(groupID, self string, cursor int64, names map[string]string, instruction string) string {
	text, err := groupInjectionText(groupID, self, cursor, names, instruction)
	if err != nil {
		slog.Warn("group: injection load failed", "group", groupID, "err", err)
		return ""
	}
	return text
}

// finalizeGroupOrphans closes any streaming=1 rows left on the group timeline
// by a previous crashed/aborted group turn, so they do not surface as phantom
// streaming bubbles on reload (review C1 residual). Uses the group session as
// the orphan-finalize target.
func finalizeGroupOrphans(groupID string) {
	finalizeOrphanedStreamingMessages(groupID, "interrupt")
}

// emitGroupUserMessage broadcasts a persisted group user message to the group's
// subscribers. It is a seam (var) so tests can observe without the WS hub.
// queueID/senderClientID let the sending device adopt the DB id instead of
// rendering a duplicate bubble (same contract as the single-agent path).
var emitGroupUserMessage = func(groupID string, msgID int64, text, queueID, senderClientID string) {
	ws.EmitToSession(groupID, ai.StreamEvent{
		Type: eventTypeUserMessage,
		UserMessage: &ai.UserMessageData{
			MessageID:      msgID,
			Content:        text,
			QueueID:        queueID,
			SenderClientID: senderClientID,
		},
	})
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
// #40/#43). Like emitGroupUserMessage it is a seam (var) so tests can observe
// without the WS hub. msgID is the chat_history row id; the frontend dedups on
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
// the whole queue is empty. That is why the orchestrator's own standalone entry
// (RunGroupTurn) emits the terminal itself while this one does not.
//
// runCtx is the claim's context (from TryClaimSessionRun or the enqueue path),
// so CancelSession reaches every member turn. The caller is responsible for
// FinishSessionRun; this function does not touch the running flag, because the
// drain loop's MarkDoneAndSendFinal owns that transition.
func RunGroupDrainLoop(runCtx context.Context, groupID, projectPath string, firstMsgID int64, firstText string, firstFiles []model.FileEntry) {
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

	first := RunGroupTurnDrain(runCtx, groupID, firstMsgID,
		QueuedRow{SessionID: groupID, Content: firstText, Files: firstFiles}, nil)

	RunDrainLoop(DrainConfig{
		SessionID:   groupID,
		ProjectPath: projectPath,
		BackendName: GetSessionBackend(groupID),
		ExecuteRunWithMessage: func(msgID int64, row QueuedRow) DrainResult {
			return RunGroupTurnDrain(runCtx, groupID, msgID, row, nil)
		},
		// Each intermediate answer in a multi-message group drain is its own
		// completed turn: notify now, not once at the very end (decision #72).
		OnTurnAnswered: func() {
			EmitTurnAnsweredNotification(groupID)
		},
		MarkDoneAndSendFinal: markDoneAndSendFinal,
	}, first)
}
