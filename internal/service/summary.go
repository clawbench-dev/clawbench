package service

import (
	"log/slog"
	"strings"
	"sync"

	"clawbench/internal/grouprouting"
	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// AssistantConclusion extracts an assistant reply's conclusion text and applies
// the group-chat protocol policy for the session.
//
// A group timeline's speech may carry `<clawbench-mention>` tags; a private note
// must never leave the timeline (fail-closed), so a group session's conclusion
// is stripped with the injection-boundary primitive. A non-group session has no
// protocol, so its text is returned VERBATIM — stripping it would truncate an
// ordinary reply that merely DISCUSSES the tag syntax (an unclosed literal
// `<clawbench-mention` in prose dropped the whole tail: regression, message
// 58879, where a single chat's summary was cut from 1551 to 1258 chars).
//
// Every consumer of assistant conclusion text (reading summary, push preview,
// recommendation, TTS, RAG index) must go through this so the policy lives in
// exactly one place.
func AssistantConclusion(sessionID string, blocks []model.ContentBlock) string {
	return assistantConclusionWithPolicy(IsGroupSession(sessionID), blocks)
}

// assistantConclusionWithPolicy is AssistantConclusion with the group policy
// already resolved. Callers that must resolve the policy BEFORE holding a read
// cursor (GetConversationIndex) use this so the lookup is not repeated per row.
func assistantConclusionWithPolicy(isGroup bool, blocks []model.ContentBlock) string {
	return applyProtocolPolicy(isGroup, summarize.ExtractLastAnswerFromBlocks(blocks))
}

// AssistantFullAnswer returns the assistant reply's COMPLETE final answer,
// applying the same protocol policy as AssistantConclusion.
//
// It differs from AssistantConclusion in one way: every text block after the
// last tool_use is joined, not just the first one. A turn can be split across
// several adjacent text blocks (the accumulator starts a new block after each
// thinking/tool boundary), so taking only the first returns a prefix of the
// answer with no indication that anything was cut. AssistantConclusion keeps its
// "first block" contract because a summary is meant to be short; this one is for
// callers that promise the user the WHOLE reply (the /show command).
//
// Falls back to AssistantConclusion when nothing trails the last tool_use (the
// AI answered and then called a terminal tool), so both paths agree there.
func AssistantFullAnswer(sessionID string, blocks []model.ContentBlock) string {
	isGroup := IsGroupSession(sessionID)
	if text := trailingAnswerText(blocks); text != "" {
		return applyProtocolPolicy(isGroup, text)
	}
	return assistantConclusionWithPolicy(isGroup, blocks)
}

// trailingAnswerText joins every non-blank text block after the last tool_use.
// Returns "" when the blocks carry no tool_use or nothing follows it, which is
// the signal to fall back to the conclusion extractor.
func trailingAnswerText(blocks []model.ContentBlock) string {
	lastToolIdx := -1
	for i, b := range blocks {
		if b.Type == eventTypeToolUse {
			lastToolIdx = i
		}
	}
	if lastToolIdx < 0 {
		return ""
	}
	var parts []string
	for i := lastToolIdx + 1; i < len(blocks); i++ {
		if blocks[i].Type == contentKeyText && strings.TrimSpace(blocks[i].Text) != "" {
			parts = append(parts, blocks[i].Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// applyProtocolPolicy strips protocol tags according to the session kind. This
// is the single place the policy lives; every extractor above routes through it.
func applyProtocolPolicy(isGroup bool, text string) string {
	if isGroup {
		return grouprouting.StripProtocolTags(text)
	}
	// Single chat: strip ONLY the auto-cruise completion signal. A single chat is
	// not parsed for mention tags (doing so re-opened regression 58879, where a
	// reply DISCUSSING the syntax was truncated), but the self-closing,
	// content-free <clawbench-goal-satisfied/> is emitted by single-chat cruise
	// and must not reach the summary / preview / TTS / RAG text. The frontend
	// strips it for the bubble; this is the backend boundary for every text that
	// leaves the device.
	return grouprouting.StripGoalSatisfied(text)
}

// summaryInFlight tracks chat-message IDs currently being summarized by the
// bulk background paths (triggerChatSummarization, backfillMissingSummaries).
// GetChatHistoryPaged spawns a backfill goroutine on every read, so without this
// dedup concurrent reads would generate duplicate summary goroutines — and
// duplicate DB writes + WS broadcasts — for the same message.
var summaryInFlight sync.Map // map[int64]struct{} keyed by chat_history message ID

// summarizeMessageOnce runs summarizeMessage under a per-message in-flight guard.
// If another goroutine is already summarizing the same message, it returns false
// (skipped) and the in-flight call will persist the summary. Returns true when
// this call performed the summarization.
func summarizeMessageOnce(targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) bool {
	if _, loaded := summaryInFlight.LoadOrStore(targetID, struct{}{}); loaded {
		return false
	}
	defer summaryInFlight.Delete(targetID)
	_ = summarizeMessage(targetID, blocks, projectPath, sessionID)
	return true
}

// summarizeMessage extracts the last answer text and saves it as a reading
// summary for a chat message, without any AI call or length threshold.
// Cards (AskUserQuestion only — PermissionApproval is deliberately excluded, see
// isSummaryCardTool) are extracted unchanged.
// Both interactive chat (triggerChatSummarization, backfillMissingSummaries)
// and tasks (executeTask) route through this function.
// Returns an error when the summary could not be saved; async callers discard
// it and rely on the internal logging.
func summarizeMessage(targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) error {
	text := AssistantConclusion(sessionID, blocks)
	if text == "" {
		return nil
	}
	cards := extractSummaryCards(blocks)
	if err := SaveSummaryWithCards("chat_message", targetID, text, cards); err != nil {
		slog.Warn(
			"failed to save summary",
			slog.Int64("target_id", targetID),
			slog.String("err", err.Error()),
		)
		return err
	}
	broadcastSummaryUpdate("chat_message", targetID, text, cards, projectPath, sessionID)
	return nil
}

// GenerateMessageSummaryOnDemand generates a reading summary for a single chat
// message on demand, e.g. when the user clicks the summary button on a
// historical message that has no summary yet. If a summary already exists it is
// returned unchanged. Only non-streaming assistant messages can be summarized
// (matching triggerChatSummarization); anything else returns ok=false. Returns
// the summary text (empty when no answer could be extracted, e.g. the message
// has no text/tool blocks), its cards, whether a summary is available, and any
// load/save error.
func GenerateMessageSummaryOnDemand(messageID int64) (summary string, cards *model.SummaryCards, ok bool, err error) {
	msg, err := GetMessageByID(messageID)
	if err != nil {
		return "", nil, false, err
	}
	if msg.Role != roleAssistant || msg.Streaming {
		return "", nil, false, nil
	}
	if existing, found := GetSummary("chat_message", messageID); found {
		_, existingCards, _ := GetSummaryWithCards("chat_message", messageID)
		return existing, existingCards, true, nil
	}
	// Non-JSON content (e.g. a plain-text assistant message) means there are no
	// blocks to summarize — treated as "no summary available", not an error.
	blocks, _ := parseMessageBlocks(msg.Content)
	if len(blocks) == 0 {
		return "", nil, false, nil
	}
	// Save errors must propagate so the caller can surface a real failure
	// instead of silently reporting "no summary" to the user.
	if err := summarizeMessage(messageID, blocks, msg.ProjectPath, msg.SessionID); err != nil {
		return "", nil, false, err
	}
	if existing, found := GetSummary("chat_message", messageID); found {
		_, existingCards, _ := GetSummaryWithCards("chat_message", messageID)
		return existing, existingCards, true, nil
	}
	return "", nil, false, nil
}

// broadcastSummaryUpdate emits a summary_update WebSocket event for a target.
func broadcastSummaryUpdate(targetType string, targetID int64, summary string, cards *model.SummaryCards, projectPath, sessionID string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "summary_update",
		Data: ws.SummaryUpdateData{
			TargetType:   targetType,
			TargetID:     targetID,
			Summary:      summary,
			SummaryCards: cards,
			ProjectPath:  projectPath,
			SessionID:    sessionID,
		},
	})
}
