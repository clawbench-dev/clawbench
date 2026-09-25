package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// DingTalkSessionInfo carries session metadata for the DingTalk session command feature.
type DingTalkSessionInfo struct {
	ID          string
	Title       string
	ProjectPath string
	Backend     string
	AgentID     string
	Model       string
}

// FeishuSessionInfo carries session metadata for the Feishu session command feature.
// It shares the same structure as DingTalkSessionInfo.
type FeishuSessionInfo = DingTalkSessionInfo

// FindSessionsByPrefix finds non-archived chat sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := dbRead.QueryContext(
		context.Background(),
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE LOWER(id) LIKE LOWER(?) AND archived = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT 10`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

// ListRecentSessions returns the most recently updated non-archived chat sessions.
func ListRecentSessions(limit int) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := dbRead.QueryContext(
		context.Background(),
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE archived = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

// FindRunningSessionsByPrefix finds currently-running sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindRunningSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	runningIDs := GetRunningSessionIDs()
	if len(runningIDs) == 0 {
		return nil, nil
	}

	lowerPrefix := strings.ToLower(prefix)
	var matchingIDs []string
	for _, id := range runningIDs {
		if len(id) >= len(lowerPrefix) && strings.ToLower(id[:len(lowerPrefix)]) == lowerPrefix {
			matchingIDs = append(matchingIDs, id)
		}
	}
	if len(matchingIDs) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	args := make([]any, len(matchingIDs))
	for i, id := range matchingIDs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args[i] = id
	}

	rows, err := dbRead.QueryContext(
		context.Background(),
		fmt.Sprintf(
			`SELECT id, title, project_path, backend, agent_id, model
			 FROM chat_sessions
			 WHERE id IN (%s) AND archived = 0`,
			sb.String(),
		),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

func scanDingTalkSessionInfos(rows *sql.Rows) []DingTalkSessionInfo {
	var results []DingTalkSessionInfo
	for rows.Next() {
		var info DingTalkSessionInfo
		if err := rows.Scan(&info.ID, &info.Title, &info.ProjectPath, &info.Backend, &info.AgentID, &info.Model); err != nil {
			slog.Warn("scanDingTalkSessionInfos: skipping row", "error", err)
			continue
		}
		results = append(results, info)
	}
	return results
}

// SendMessageToSessionFromDingTalk sends a message to a non-running session from DingTalk.
func SendMessageToSessionFromDingTalk(sessionID, message string, files []model.FileEntry) error {
	return sendMessageToSessionFromPush(sessionID, message, files)
}

// SendMessageToSessionFromFeishu sends a message to a non-running session from Feishu.
func SendMessageToSessionFromFeishu(sessionID, message string, files []model.FileEntry) error {
	return sendMessageToSessionFromPush(sessionID, message, files)
}

// GetSessionInfoForPush returns session metadata for a push backend, or an
// error when the session does not exist (or is archived).
//
// Push backends need ProjectPath to place a downloaded IM attachment in the
// session's own .clawbench/uploads/ directory.
func GetSessionInfoForPush(sessionID string) (DingTalkSessionInfo, error) {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return DingTalkSessionInfo{}, fmt.Errorf("session %s not found", sessionID)
	}
	return DingTalkSessionInfo{
		ID:          sessionID,
		Title:       info.Title,
		ProjectPath: info.ProjectPath,
		Backend:     info.Backend,
		AgentID:     info.AgentID,
		Model:       info.Model,
	}, nil
}

// sendMessageToSessionFromPush is the shared implementation for sending a message
// to a non-running session from any push backend (DingTalk, Feishu, etc.).
//
// files are the message's attachments, already written to disk by the caller.
// An empty message with files is valid: a bare file sent from IM carries no
// text, and the execution engine injects the attachment path into the prompt.
func sendMessageToSessionFromPush(sessionID, message string, files []model.FileEntry) error {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Mint a queue id for the message. An IM backend has no queue id of its
	// own, and the id is the queue entity's identity (cancel/inject address it)
	// as well as the token the sender-side dedup uses on queue_added.
	queueID := newPushQueueID()

	// Persist the message + start execution or signal the running drain loop.
	// EnqueueAndMaybeStart handles the B2 drain-loop exit race internally and
	// emits the appropriate announcement (user_message when idle, queue_added
	// when a runner is live).
	_, _, err := EnqueueAndMaybeStart(EnqueueStartConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     message,
		Files:       files,
		QueueID:     queueID,
	})
	return err
}

// newPushQueueID mints a queue id for a message that arrives from an IM
// backend, which (unlike the web client) has no queue id of its own.
//
// The format mirrors newQueueID so ids from both paths look alike; uniqueness
// comes from the timestamp plus a monotonic counter, not from randomness, so a
// burst of messages cannot collide on a coarse clock.
func newPushQueueID() string {
	return fmt.Sprintf("q-%s-%d", time.Now().Format("20060102150405"), pushQueueSeq.Add(1))
}

// pushQueueSeq disambiguates push queue ids minted within the same second.
var pushQueueSeq atomic.Int64

// LaunchConfig configures a session execution launched from non-HTTP contexts.
type LaunchConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string
	AgentID     string
	Message     string
	// Files are the message's attachments. They MUST be carried here: this
	// engine builds its own prompt (executeStreamRunShared) and does not go
	// through the handler's prompt builder, so omitting them silently drops
	// every attachment — the URL of a quoted issue/PR and ordinary files alike.
	Files []model.FileEntry

	// RunCtx is the execution context returned by TryClaimSessionRun. It must be
	// passed through so the execution is both the one the claim created and the
	// one a cancel reaches. Nil falls back to a registry lookup (see
	// LaunchSessionExecution).
	RunCtx context.Context
}

// LaunchSessionExecution starts the AI execution goroutine for a session.
// The caller must have already persisted the user message and called TrySetSessionRunning.
func LaunchSessionExecution(cfg LaunchConfig) {
	sessionID := cfg.SessionID
	// The execution context is the one the claim returned (see
	// TryClaimSessionRun). It is passed in rather than looked up here on purpose:
	// a cancel landing between the claim and this call removes the registry
	// entry, and a lookup would then find nothing — after the caller had already
	// consumed the message row, which the reaper cannot recover (it only scans
	// rows still queued).
	ctx := cfg.RunCtx
	if ctx == nil {
		// Defensive: a caller that bypassed TryClaimSessionRun. Fall back to the
		// registry so behavior is unchanged for such callers, and warn because
		// it re-opens the window this field exists to close.
		ctx = runnerContext(sessionID)
	}
	if ctx == nil {
		slog.Warn("launch: no execution context for session, nothing to run",
			slog.String("session", sessionID))
		return
	}

	go func() {
		defer handleSessionPanic(cfg, sessionID)

		// Single cleanup: removes the runner and cancels its context.
		defer FinishSessionRun(sessionID)
		defer handleACPCleanup(sessionID, cfg.AgentID)

		markDoneAndSendFinal := func(event ai.StreamEvent) {
			SetSessionRunning(sessionID, false, true) // skip event — we emit directly
			emitDrainEvent(sessionID, event)
			// DingTalk/Feishu push — EmitSessionEvent is skipped above, so push
			// must be triggered here for normal completion.
			// Skip for cancelled: CancelSession already calls EmitSessionEvent("cancelled")
			// which handles push. Skip for error: no meaningful push content.
			if event.Type == eventTypeDone {
				// Only the first terminal state may push + broadcast "completed".
				// If a concurrent CancelSession already claimed the terminal guard
				// (broadcasting "cancelled"), EmitSessionPushNotification returns
				// false and we must NOT also broadcast "completed" — otherwise
				// clients see cancelled followed by a contradictory completed.
				if !EmitSessionPushNotification(sessionID, statusCompleted) {
					return
				}
				// Global WS broadcast so all clients (even ones that missed the
				// stream "done") clear the session's running flag.
				emitSessionEvent(sessionID, statusCompleted, false, false, true)
			}
		}

		result := executeStreamRunShared(ctx, cfg)
		RunDrainLoop(DrainConfig{
			SessionID:   sessionID,
			ProjectPath: cfg.ProjectPath,
			BackendName: cfg.BackendName,
			ExecuteRunWithMessage: func(_ int64, row QueuedRow) DrainResult {
				cfg.Message = row.Content
				// Carry the drained row's own attachments, replacing whatever the
				// previous turn carried — otherwise turn N's files would be
				// re-prefixed onto turn N+1's prompt.
				cfg.Files = row.Files
				nextResult := executeStreamRunShared(ctx, cfg)
				return DrainResult{
					CancelReason:   nextResult.cancelReason,
					Err:            nextResult.err,
					Empty:          nextResult.empty,
					AbnormalReason: nextResult.abnormalReason,
				}
			},
			// Resume an abnormally terminated turn by sending a localized
			// "continue" and running one more turn. The prompt/queueID come from
			// the shared runner, which also owns the delay and cancel checks.
			AutoContinue: NewAutoContinueRunner(AutoContinueRunnerConfig{
				Ctx:         ctx,
				SessionID:   sessionID,
				ProjectPath: cfg.ProjectPath,
				BackendName: cfg.BackendName,
				RunTurn: func(prompt string) DrainResult {
					// Mutate the shared cfg for this attempt only; the drain loop
					// overwrites Message/Files when it dequeues the next real user
					// message, so nothing leaks forward.
					cfg.Message = prompt
					cfg.Files = nil
					res := executeStreamRunShared(ctx, cfg)
					return DrainResult{
						CancelReason:   res.cancelReason,
						Err:            res.err,
						Empty:          res.empty,
						AbnormalReason: res.abnormalReason,
					}
				},
			}),
			MarkDoneAndSendFinal: markDoneAndSendFinal,
		}, DrainResult{
			CancelReason:   result.cancelReason,
			Err:            result.err,
			Empty:          result.empty,
			AbnormalReason: result.abnormalReason,
		})
	}()
}

// EnqueueStartConfig carries the parameters for EnqueueAndMaybeStart.
type EnqueueStartConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string
	AgentID     string
	Message     string
	Files       []model.FileEntry
	QueueID     string
	// ModelID / Transport are persisted to the session so the drain loop's
	// buildChatRequestFromQueue uses the user's choices, not agent defaults
	// (parity with the POST /api/ai/chat path).
	ModelID   string
	Transport string
	// SenderClientID is the WS client id of the sending device, echoed on the
	// queue_added event so the sender can skip its own optimistic entry.
	SenderClientID string
}

// EnqueueAndMaybeStart is the unified enqueue entry point (POST /api/ai/queue
// and the busy branch of POST /api/ai/chat).
//
// It inserts the message into queued_messages, then:
//   - if the session is idle, claims it, materializes it into chat_history, and
//     starts an execution goroutine that runs it and then drains the rest of the
//     queue (started=true, msgID = the chat_history row id);
//   - if the session is already running, marks its runner as having pending work
//     and returns (started=false, msgID=0) — that runner's loop will claim and
//     materialize it.
//
// A message can no longer be stranded between those two outcomes: a runner only
// exits after re-checking for late work under the same lock this function uses
// to submit (see retireRunner).
//
// Emission is centralized here so the two HTTP callers cannot drift:
//   - idle: the materialized message is announced with user_message (it is a
//     real chat_history row now);
//   - busy: the queued message is announced with queue_added (it has no
//     chat_history row yet, so there is no message id).
func EnqueueAndMaybeStart(cfg EnqueueStartConfig) (started bool, msgID int64, err error) {
	// Sending always queues while a turn is running — for every backend.
	//
	// Mid-turn injection is NOT attempted here on purpose: joining the running
	// turn is an explicit action on the queued entry (POST /api/ai/queue/inject),
	// so the flow is uniform across backends and the message is always visible
	// and cancelable in the queue instead of silently disappearing into the
	// reply being written.

	// Persist model/transport selection so the drain loop uses the user's
	// choices (parity with the POST /api/ai/chat handler).
	if cfg.ModelID != "" {
		if updateErr := UpdateSessionModel(cfg.SessionID, cfg.ModelID); updateErr != nil {
			slog.Warn("enqueue: failed to persist session model",
				slog.String("session", cfg.SessionID), slog.String("error", updateErr.Error()))
		}
	}
	if cfg.Transport != "" {
		if updateErr := UpdateSessionTransport(cfg.SessionID, cfg.Transport); updateErr != nil {
			slog.Warn("enqueue: failed to persist session transport",
				slog.String("session", cfg.SessionID), slog.String("error", updateErr.Error()))
		}
	}

	// Mint the queue id here (not inside AddQueuedMessage) so the queue_added
	// broadcast below carries the same id the row stores. AddQueuedMessage's own
	// empty-id fallback would otherwise generate an id this function never sees,
	// and clients would hold an entry they can never match to its drain.
	effectiveQueueID := cfg.QueueID
	if effectiveQueueID == "" {
		effectiveQueueID = NewQueueID()
	}

	queueRowID, err := AddQueuedMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, cfg.Message, cfg.Files, effectiveQueueID, "")
	if err != nil {
		return false, 0, err
	}

	// Claim the session. created=true means we must start the execution; false
	// means a live runner exists and has been woken to pick this message up.
	if runCtx, created := TryClaimSessionRun(cfg.SessionID); created {
		// Session was idle — claim and materialize the row we just inserted so
		// the execution has a real chat_history user message (and so the drain
		// loop cannot pick it up a second time).
		//
		// Claim BY ID: a concurrent enqueue may have slipped an earlier row into
		// the queue between our insert and the claim, and that earlier row
		// belongs to the drain loop, not to this execution.
		row, matMsgID, ok, cerr := ClaimByIDAndMaterialize(cfg.SessionID, queueRowID)
		if cerr != nil || !ok {
			// The row we just inserted is gone — a concurrent cancel/clear took
			// it. Nothing to run; report failure so the caller restores input.
			slog.Warn("enqueue: queued row vanished before materialize",
				slog.String("session", cfg.SessionID),
				slog.Int64("queue_row_id", queueRowID),
				slog.Any("error", cerr))
			return false, 0, fmt.Errorf("queued message could not be materialized")
		}
		// Announce the real user message so every device renders it inline.
		emitUserMessage(cfg.SessionID, matMsgID, row)

		// Start execution now; the loop inside will consume the REST of the
		// queue (any messages beyond the first).
		LaunchSessionExecution(LaunchConfig{
			SessionID:   cfg.SessionID,
			ProjectPath: cfg.ProjectPath,
			BackendName: cfg.BackendName,
			AgentID:     cfg.AgentID,
			Message:     cfg.Message,
			Files:       cfg.Files,
			RunCtx:      runCtx,
		})
		return true, matMsgID, nil
	}

	// A runner already exists and has been woken; its loop will claim and
	// materialize this message. Announce it as queued so other devices render it.
	//
	// effectiveQueueID (not cfg.QueueID) is broadcast: when the caller sent no
	// id, AddQueuedMessage generated one, and that stored id is what the later
	// queue_drain/queue_inject events carry. Broadcasting the empty input would
	// leave clients holding an entry they can never match or cancel.
	ws.EmitToSession(cfg.SessionID, ai.StreamEvent{
		Type: "queue_added",
		QueueAdded: &ai.QueueAddedData{
			QueueID:        effectiveQueueID,
			Text:           cfg.Message,
			Files:          cfg.Files,
			SenderClientID: cfg.SenderClientID,
		},
	})
	return false, 0, nil
}

// handleSessionPanic recovers from panics in the session goroutine.
func handleSessionPanic(cfg LaunchConfig, sessionID string) {
	if r := recover(); r != nil {
		slog.Error(
			"session goroutine panicked",
			slog.String("session", sessionID),
			slog.Any("panic", r),
			slog.String("stack", string(debug.Stack())),
		)
		// Retire the runner and cancel its context together, so the session is
		// not left running with nothing to cancel it.
		FinishSessionRun(sessionID)
		emitDrainEvent(sessionID, ai.StreamEvent{Type: eventTypeError, Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
		// Push cancelled notification — panic is a terminal state
		EmitSessionPushNotification(sessionID, statusCancelled)
		errMsg := "AI internal error, please retry"
		errContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{map[string]string{contentKeyType: blockTypeWarning, contentKeyText: errMsg, contentKeyReason: ai.ReasonPanic}}})
		_, _ = FinalizeStreamingMessage(cfg.ProjectPath, cfg.BackendName, sessionID, string(errContent))
	}
}

// handleACPCleanup marks the ACP connection as idle after session completion.
func handleACPCleanup(sessionID, agentID string) {
	effectiveTransport := transportCLI
	if t := GetSessionTransport(sessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[agentID]; ok && agent.Transport != "" {
		effectiveTransport = agent.Transport
	}
	if effectiveTransport == transportACPStdio {
		slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", agentID)
		ai.GetACPConnManager().MarkIdle(sessionID)
	}
}

// forkToolOutputMaxLen is the maximum number of runes kept from a tool_use
// output field when building fork context.  Long outputs (file reads, command
// results, etc.) are truncated to avoid blowing up the context window of the
// forked session.
const forkToolOutputMaxLen = 500

// truncateRunes returns s truncated to maxRunes with a "...(truncated)" suffix
// when the string exceeds the limit.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "...(truncated)"
}

// FormatToolUseBlock renders a tool_use ContentBlock as structured JSON wrapped
// in <tool_use> tags. Enriches the block with input/output from the detail table
// when available. Applies truncation to keep the output reasonable.
func FormatToolUseBlock(b model.ContentBlock, toolCallMap map[string]*ToolCallRecord) string {
	// Base fields from the slim content block
	obj := map[string]any{
		"name": b.Name,
		"id":   b.ID,
	}
	if b.Status != "" {
		obj["status"] = b.Status
	}
	if b.Done {
		obj["done"] = true
	}
	if b.DurationMs > 0 {
		obj["duration_ms"] = b.DurationMs
	}
	if b.Summary != "" {
		obj["summary"] = b.Summary
	}

	// Enrich with input/output from chat_tool_calls detail table
	tc, found := toolCallMap[b.ID]
	if found {
		inputStr := string(tc.Input)
		obj["input"] = inputStr
		obj["output"] = truncateRunes(tc.Output, forkToolOutputMaxLen)
	} else if b.Input != nil {
		// Fallback: use input from content block (interactive tools keep input inline)
		inputJSON, _ := json.Marshal(b.Input)
		obj["input"] = string(inputJSON)
		if b.Output != "" {
			obj["output"] = truncateRunes(b.Output, forkToolOutputMaxLen)
		}
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return "<tool_use>" + string(jsonBytes) + "</tool_use>"
}

type streamRunResultShared struct {
	cancelReason   string
	err            string
	empty          bool
	abnormalReason string
}

// executeStreamRunShared runs one AI backend execution.
// Uses the correct SessionExecutor API: NewSessionExecutor(ctx, RunConfig) -> RunWithChannel(eventCh) -> Finalize(result, eventCh)
func executeStreamRunShared(ctx context.Context, cfg LaunchConfig) streamRunResultShared {
	fileDir := resolveFileDir(cfg.ProjectPath)

	// Prefix the attachments onto the prompt. runTurn is the single turn
	// implementation, but the prompt itself is built here, so the attachment
	// classification must happen on this side too — otherwise every attachment
	// is silently dropped (both an ordinary file's path and a quoted issue/PR's
	// URL). Paths arrive already resolved/validated (the handler resolves them
	// before persisting, and the drain loop reads them back from the DB), so
	// there is no legacy filePaths channel to de-duplicate against.
	prompt := cfg.Message
	parts := model.ClassifyAttachments(cfg.Files, nil)
	prompt = model.ApplyAttachmentPrefixes(prompt, nil, nil, parts)

	chatReq := BuildChatRequest(prompt, cfg.SessionID, cfg.ProjectPath, cfg.BackendName, cfg.AgentID, "", "", "", "", fileDir, model.HasAttachmentEntries(cfg.Files))

	// The one AI-turn implementation — shared with the /api/ai/chat handler and
	// the scheduler so a fix can no longer land in only one copy.
	res := runTurn(TurnSpec{
		Ctx:             ctx,
		Mode:            ModeInteractive,
		ProjectPath:     cfg.ProjectPath,
		BackendName:     cfg.BackendName,
		SessionID:       cfg.SessionID,
		AgentID:         cfg.AgentID,
		ChatReq:         chatReq,
		FileDir:         fileDir,
		DrainOnFinalize: true,
		LocalizeError:   serviceLocalizeError,
	})
	return streamRunResultShared{
		cancelReason:   res.CancelReason,
		err:            res.Err,
		empty:          res.Empty,
		abnormalReason: res.AbnormalReason,
	}
}
