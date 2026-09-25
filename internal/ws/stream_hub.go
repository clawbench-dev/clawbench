package ws

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"clawbench/internal/ai"
)

// ContextStateUsage represents the persisted usage state for a session,
// used as a DB fallback when no live ACP connection is available.
// Embeds ai.UsageState for compile-time parity with service.UsageStatePersist
// — both must serialize identical JSON shapes for DB/WS consistency.
type ContextStateUsage = ai.UsageState

// payloadKeyMessageID is the chat_stream payload field carrying a message's DB
// row id, shared by the user_message / queue_drain / queue_inject payloads.
const payloadKeyMessageID = "messageId"

// GetContextStateUsageFunc is a function that retrieves persisted usage state
// for a session. Injected by the service layer to avoid circular imports.
type GetContextStateUsageFunc func(sessionID string) *ContextStateUsage

// StreamStateLookupFunc returns the live run's state for a session: the
// streaming assistant row id, plus the question that run answers (id 0 and
// empty content when the run has no question, e.g. a scheduled task).
//
// Injected by the service layer to avoid a circular import.
type StreamStateLookupFunc func(sessionID string) (messageID int64, questionID int64, questionContent string)

// StreamHub manages session-scoped streaming event fan-out via WebSocket.
// It replaces the single-consumer SSE channel with multi-client WS delivery.
// Clients subscribe to specific sessions to receive their streaming events.
type StreamHub struct {
	mu                sync.RWMutex
	subscribers       map[string]map[string]struct{} // sessionID -> set of clientIDs
	mgr               *Manager
	getContextUsageFn GetContextStateUsageFunc // injected by service layer
	storeEventFn      func(ServerMessage)      // injected by service layer (write-ahead)
	streamStateFn     StreamStateLookupFunc    // injected by service layer
}

// SetGetContextStateUsageFunc injects a function that retrieves persisted usage
// state from DB. Called by the service layer during initialization to avoid
// circular imports.
func (h *StreamHub) SetGetContextStateUsageFunc(fn GetContextStateUsageFunc) {
	h.getContextUsageFn = fn
}

// SetEventStoreFunc injects a function that persists notifiable WS events
// (write-ahead). Called by the service layer to avoid circular imports.
func (h *StreamHub) SetEventStoreFunc(fn func(ServerMessage)) {
	h.mu.Lock()
	h.storeEventFn = fn
	h.mu.Unlock()
}

// SetStreamStateLookupFunc injects the lookup for a session's live run state
// (streaming row id + the question it answers). Called by the service layer at
// init, mirroring SetGetContextStateUsageFunc.
func (h *StreamHub) SetStreamStateLookupFunc(fn StreamStateLookupFunc) {
	h.mu.Lock()
	h.streamStateFn = fn
	h.mu.Unlock()
}

// NewStreamHub creates a StreamHub associated with the given Manager.
func NewStreamHub(mgr *Manager) *StreamHub {
	return &StreamHub{
		subscribers: make(map[string]map[string]struct{}),
		mgr:         mgr,
	}
}

// Manager returns the Manager associated with this StreamHub.
func (h *StreamHub) Manager() *Manager {
	return h.mgr
}

// Subscribe adds a client as a subscriber to a session's streaming events.
func (h *StreamHub) Subscribe(clientID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscribers[sessionID] == nil {
		h.subscribers[sessionID] = make(map[string]struct{})
	}
	h.subscribers[sessionID][clientID] = struct{}{}

	slog.Debug("streamhub: client subscribed to session", "client_id", clientID, "session_id", sessionID)
}

// Unsubscribe removes a client from a session's streaming events.
func (h *StreamHub) Unsubscribe(clientID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.subscribers[sessionID]; ok {
		delete(subs, clientID)
		if len(subs) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

// UnsubscribeAll removes a client from all session subscriptions.
// Called when a WS client disconnects.
func (h *StreamHub) UnsubscribeAll(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for sessionID, subs := range h.subscribers {
		delete(subs, clientID)
		if len(subs) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

// IsSubscribed reports whether clientID is currently subscribed to sessionID.
func (h *StreamHub) IsSubscribed(clientID, sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, ok := h.subscribers[sessionID][clientID]
	return ok
}

// HasSubscribers returns true if any client is subscribed to the session.
func (h *StreamHub) HasSubscribers(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	subs, ok := h.subscribers[sessionID]
	return ok && len(subs) > 0
}

// SubscriberCount returns how many clients are subscribed to a session.
// Used by the drop-diagnostics path to cross-check the "no subscribers"
// decision against the per-client subscription table.
func (h *StreamHub) SubscriberCount(sessionID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.subscribers[sessionID])
}

// Emit fans out a streaming event to all subscribed WS clients for a session.
func (h *StreamHub) Emit(sessionID string, event ai.StreamEvent) {
	msg, ok := buildChatStreamMessage(sessionID, event)
	if !ok {
		return
	}

	// Write-ahead: persist notifiable events (user_message) before broadcast so
	// offline clients can recover them after reconnect.
	h.storeEventIfNotifiable(msg, event.Type)

	// Fan out to subscribers
	h.mu.RLock()
	subs, ok := h.subscribers[sessionID]
	if !ok || len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	// Copy subscriber list to avoid holding lock during sends
	clientIDs := make([]string, 0, len(subs))
	for id := range subs {
		clientIDs = append(clientIDs, id)
	}
	h.mu.RUnlock()

	// Send to each subscriber
	for _, clientID := range clientIDs {
		h.mgr.SendToClient(clientID, msg)
	}
}

// buildChatStreamMessage converts a StreamEvent into its chat_stream envelope.
// ok is false for events with no wire payload (they are dropped, as before).
func buildChatStreamMessage(sessionID string, event ai.StreamEvent) (ServerMessage, bool) {
	payload := StreamEventToPayload(event)
	if payload == nil {
		return ServerMessage{}, false
	}
	return ServerMessage{
		Type:  MessageTypeEvent,
		ID:    GenerateEventID(),
		Event: "chat_stream",
		Data: ChatStreamData{
			SessionID: sessionID,
			EventType: event.Type,
			Payload:   payload,
		},
	}, true
}

// storeEventIfNotifiable runs the injected write-ahead hook for event types it
// covers. The hook is only invoked when set; whether to actually persist is
// StoreNotifiableEvent's decision (it skips when every client is connected).
func (h *StreamHub) storeEventIfNotifiable(msg ServerMessage, eventType string) {
	if eventType != "user_message" {
		return
	}
	h.mu.RLock()
	storeFn := h.storeEventFn
	h.mu.RUnlock()
	if storeFn != nil {
		storeFn(msg)
	}
}

// EmitToSession emits a StreamEvent to all subscribers of a session.
// This is a convenience function that retrieves the Manager and StreamHub,
// checking for nil and subscriber presence. Use this instead of duplicating
// the nil-check pattern at each call site.
func EmitToSession(sessionID string, event ai.StreamEvent) {
	mgr := GetManager()
	if mgr == nil {
		recordDeliveryDrop(DropReasonNoManager, sessionID, event.Type)
		return
	}
	hub := mgr.StreamHub()
	if hub == nil {
		recordDeliveryDrop(DropReasonNoManager, sessionID, event.Type)
		return
	}
	if !hub.HasSubscribers(sessionID) {
		// No live subscriber: nothing to deliver. Record it — silently
		// returning here is what previously made a lost subscription
		// indistinguishable from a backend that never sent anything.
		//
		// The diagnostic payload is built lazily (only when the drop is
		// actually logged, not on every suppressed repeat) and distinguishes
		// "no client at all" from "clients connected but none subscribed to
		// THIS session" — the silent failure that leaves a UI stuck mid-stream.
		recordDeliveryDropWithDiagnostics(DropReasonNoSubscribers, sessionID, event.Type, func() []any {
			return mgr.SubscriptionDiagnostics(sessionID)
		})
		// Still hand the event to the write-ahead store. Emit is the ONLY place
		// that persists notifiable events, so returning here outright dropped
		// them for good: a user_message sent while the client was mid-reconnect
		// was never delivered AND never stored, so the reconnect replay could
		// not recover it either — the client then rendered the assistant reply
		// with no question above it until a full history reload.
		//
		// StoreNotifiableEvent decides for itself whether to persist (it skips
		// when every client is connected), so calling it unconditionally here
		// keeps that policy in one place.
		if msg, ok := buildChatStreamMessage(sessionID, event); ok {
			hub.storeEventIfNotifiable(msg, event.Type)
		}
		return
	}
	hub.Emit(sessionID, event)
}

// StreamEventToPayload converts an ai.StreamEvent to the payload data
// that was previously written as SSE `data:` fields. The payload format
// is kept identical to the SSE format for frontend compatibility.
func StreamEventToPayload(event ai.StreamEvent) any { //nolint:gocyclo // one branch per stream-event type; splitting would not simplify it
	// Simple empty-payload signal events
	switch event.Type {
	case "thinking_done", "done", "replay_done":
		return map[string]any{}
	}

	switch event.Type {
	case "content", "thinking":
		return simpleTextPayload(event)
	case "tool_use":
		return toolUsePayload(event)
	case "tool_result":
		return toolResultPayload(event)
	case "metadata":
		return event.Meta
	case "cancelled":
		return map[string]string{"reason": "cancelled"}
	case "error":
		return errorPayload(event)
	case "warning":
		return warningPayload(event)
	case "user_message":
		return userMessagePayload(event)
	case "stream_start":
		return streamStartPayload(event)
	case "stream_split":
		return streamSplitPayload(event)
	case "queue_drain":
		return queueDrainPayload(event)
	case "queue_inject":
		return queueInjectPayload(event)
	case "queue_cancel":
		return queueCancelPayload(event)
	case "queue_added":
		return queueAddedPayload(event)
	default:
		return acpStatePayload(event)
	}
}

// simpleTextPayload maps a bare-text event (content/thinking) to its keyed value.
// Sub-agent content carries parent_tool_call_id so the frontend can group it
// under the Agent card that spawned it.
func simpleTextPayload(event ai.StreamEvent) any {
	payload := map[string]string{}
	if event.Type == "thinking" {
		payload["text"] = event.Content
	} else {
		payload["content"] = event.Content
	}
	if event.ParentToolCallID != "" {
		payload["parent_tool_call_id"] = event.ParentToolCallID
	}
	return payload
}

// streamStartPayload builds the stream_start message payload. Returns nil when
// no StreamStart data is attached (the event is then skipped downstream).
func streamStartPayload(event ai.StreamEvent) any {
	if event.StreamStart == nil {
		return nil
	}
	return map[string]any{"message_id": event.StreamStart.MessageID}
}

// streamSplitPayload carries the new "after" assistant row opened when a
// mid-turn injection split the assistant reply in two.
func streamSplitPayload(event ai.StreamEvent) any {
	if event.StreamSplit == nil {
		return nil
	}
	return map[string]any{"message_id": event.StreamSplit.MessageID}
}

// acpStatePayload handles ACP state update event types (mode, config, commands, etc.)
func acpStatePayload(event ai.StreamEvent) any {
	switch event.Type {
	case "mode_update":
		return event.Mode
	case "config_update":
		return event.Config
	case "commands_update":
		if event.Commands == nil {
			return nil
		}
		return map[string]any{"commands": event.Commands}
	case "thinking_effort_update":
		return event.ThinkingEffort
	case "model_list_update":
		return event.ModelList
	case "plan_update":
		return event.Plan
	case "usage_update":
		return event.Usage
	default:
		return nil
	}
}

func toolUsePayload(event ai.StreamEvent) any {
	if event.Tool == nil {
		return nil
	}
	payload := map[string]any{
		"name": event.Tool.Name,
		"id":   event.Tool.ID,
		"done": event.Tool.Done,
	}
	if event.Tool.Status != "" {
		payload["status"] = event.Tool.Status
	}
	if event.Tool.ParentToolCallID != "" {
		payload["parent_tool_call_id"] = event.Tool.ParentToolCallID
	}
	attachToolMeta(payload, event.ToolMeta)
	// Interactive tools: include input so frontend can render permission UI
	nameLower := strings.ToLower(event.Tool.Name)
	if nameLower == "askuserquestion" || nameLower == "permissionapproval" {
		var input any
		if event.Tool.Input != "" {
			_ = json.Unmarshal([]byte(event.Tool.Input), &input)
		}
		if _, ok := input.(map[string]any); !ok {
			input = map[string]any{}
		}
		payload["input"] = input
	}
	return payload
}

func toolResultPayload(event ai.StreamEvent) any {
	if event.Tool == nil {
		return nil
	}
	payload := map[string]any{
		"id": event.Tool.ID,
	}
	if event.Tool.Name != "" {
		payload["name"] = event.Tool.Name
	}
	if event.Tool.Status != "" {
		payload["status"] = event.Tool.Status
	}
	if event.Tool.ParentToolCallID != "" {
		payload["parent_tool_call_id"] = event.Tool.ParentToolCallID
	}
	attachToolMeta(payload, event.ToolMeta)
	return payload
}

func attachToolMeta(payload map[string]any, meta *ai.ToolCallMeta) {
	if meta == nil {
		return
	}
	if meta.Summary != "" {
		payload["summary"] = meta.Summary
	}
	if meta.DisplayName != "" {
		payload["display_name"] = meta.DisplayName
	}
	if meta.FilePath != "" {
		payload["file_path"] = meta.FilePath
	}
	if meta.DurationMs > 0 {
		payload["duration_ms"] = meta.DurationMs
	}
}

func userMessagePayload(event ai.StreamEvent) any {
	if event.UserMessage == nil {
		return nil
	}
	payload := map[string]any{
		payloadKeyMessageID: event.UserMessage.MessageID,
		"content":           event.UserMessage.Content,
	}
	if len(event.UserMessage.Files) > 0 {
		payload["files"] = event.UserMessage.Files
	}
	if event.UserMessage.SenderClientID != "" {
		payload["senderClientId"] = event.UserMessage.SenderClientID
	}
	if event.UserMessage.QueueID != "" {
		payload["queueId"] = event.UserMessage.QueueID
	}
	return payload
}

// queueAddedPayload announces a message that was just enqueued (it has no
// chat_history row yet, so no message id). Clients add it to the queue panel;
// the sender skips its own echo via senderClientId.
func queueAddedPayload(event ai.StreamEvent) any {
	if event.QueueAdded == nil {
		return nil
	}
	payload := map[string]any{
		"queueId": event.QueueAdded.QueueID,
		"text":    event.QueueAdded.Text,
	}
	if len(event.QueueAdded.Files) > 0 {
		payload["files"] = event.QueueAdded.Files
	}
	if event.QueueAdded.SenderClientID != "" {
		payload["senderClientId"] = event.QueueAdded.SenderClientID
	}
	return payload
}

func errorPayload(event ai.StreamEvent) any {
	payload := map[string]any{"error": event.Error}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	if event.ErrorCode != 0 {
		payload["error_code"] = event.ErrorCode
	}
	if event.HTTPStatus != 0 {
		payload["http_status"] = event.HTTPStatus
	}
	if event.ErrorSource != "" {
		payload["error_source"] = event.ErrorSource
	}
	if event.ErrorDetail != "" {
		payload["error_detail"] = event.ErrorDetail
	}
	return payload
}

func warningPayload(event ai.StreamEvent) any {
	payload := map[string]any{"text": event.Content}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	if event.ErrorCode != 0 {
		payload["error_code"] = event.ErrorCode
	}
	if event.HTTPStatus != 0 {
		payload["http_status"] = event.HTTPStatus
	}
	if event.ErrorSource != "" {
		payload["error_source"] = event.ErrorSource
	}
	if event.ErrorDetail != "" {
		payload["error_detail"] = event.ErrorDetail
	}
	return payload
}

func queueDrainPayload(event ai.StreamEvent) any {
	if event.QueueEvent == nil {
		return nil
	}
	return map[string]any{
		"sessionId":         event.QueueEvent.SessionID,
		"queueId":           event.QueueEvent.QueueID,
		payloadKeyMessageID: event.QueueEvent.MessageID,
	}
}

// queueInjectPayload carries a message that joined the RUNNING turn (the queued
// bubble's "insert" action). Clients clear that bubble's pending state but must
// NOT open a new assistant placeholder — the reply already in flight continues.
func queueInjectPayload(event ai.StreamEvent) any {
	if event.QueueEvent == nil {
		return nil
	}
	return map[string]any{
		"sessionId":         event.QueueEvent.SessionID,
		"queueId":           event.QueueEvent.QueueID,
		payloadKeyMessageID: event.QueueEvent.MessageID,
	}
}

func queueCancelPayload(event ai.StreamEvent) any {
	if event.QueueEvent == nil {
		return nil
	}
	payload := map[string]any{
		"sessionId": event.QueueEvent.SessionID,
	}
	if len(event.QueueEvent.QueueIDs) > 0 {
		payload["queueIds"] = event.QueueEvent.QueueIDs
	}
	return payload
}

// EmitACPStateEvents sends cached ACP state as chat_stream events to a client.
// Called when a client subscribes to a running session, to replicate the
// SSE re-emit-on-connect behavior.
func (h *StreamHub) EmitACPStateEvents(clientID, sessionID string) {
	s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID)
	if s.Mode != nil || s.Config != nil || s.Effort != nil || len(s.Commands) > 0 || s.ModelList != nil || s.Plan != nil || s.Usage != nil {
		h.emitACPState(clientID, sessionID, s)
	} else if h.getContextUsageFn != nil {
		// No live ACP connection — fall back to DB for usage only.
		// Mode/thinking effort are not restored from DB here because agents
		// re-emit them on reconnect; only usage (cumulative tokens/cost)
		// needs DB persistence to survive server restarts.
		if usage := h.getContextUsageFn(sessionID); usage != nil {
			h.emitStateEvent(clientID, sessionID, "usage_update", usage)
			slog.Debug("streamhub: re-emitted DB usage on subscribe", "session_id", sessionID, "client_id", clientID)
		}
	}
}

func (h *StreamHub) emitACPState(clientID, sessionID string, s ai.ACPCachedState) {
	if s.Mode != nil {
		h.emitStateEvent(clientID, sessionID, "mode_update", s.Mode)
	}
	if s.Config != nil {
		h.emitStateEvent(clientID, sessionID, "config_update", s.Config)
	}
	if s.Effort != nil {
		h.emitStateEvent(clientID, sessionID, "thinking_effort_update", s.Effort)
	}
	if len(s.Commands) > 0 {
		h.emitStateEvent(clientID, sessionID, "commands_update", map[string]any{"commands": s.Commands})
	}
	if s.ModelList != nil {
		h.emitStateEvent(clientID, sessionID, "model_list_update", s.ModelList)
	}
	if s.Plan != nil {
		h.emitStateEvent(clientID, sessionID, "plan_update", s.Plan)
	}
	if s.Usage != nil {
		h.emitStateEvent(clientID, sessionID, "usage_update", s.Usage)
	}
	slog.Debug("streamhub: re-emitted cached ACP state on subscribe", "session_id", sessionID, "client_id", clientID)
}

// EmitStreamStartEvent sends a stream_start chat_stream event to a single
// client, carrying the streaming assistant row's id.
func (h *StreamHub) EmitStreamStartEvent(clientID, sessionID string, messageID int64) {
	h.emitStateEvent(clientID, sessionID, "stream_start", map[string]any{"message_id": messageID})
}

// EmitUserMessageEvent sends a user_message chat_stream event to a single
// client. Used by the subscribe-time recovery path to hand a late subscriber
// the question its live stream_start refers to.
func (h *StreamHub) EmitUserMessageEvent(clientID, sessionID string, messageID int64, content string) {
	h.emitStateEvent(clientID, sessionID, "user_message", map[string]any{
		payloadKeyMessageID: messageID,
		"content":           content,
	})
}

// EmitLiveRunStateToClient re-emits the state a client needs to render a run it
// subscribed to mid-flight: the question bubble, then the stream_start that
// anchors the reply to it.
//
// Why this must exist at all: the frontend buffers content/thinking/tool events
// that arrive with no streaming placeholder, and drains that buffer ONLY in its
// stream_start handler (useChatStream's sole replayBufferedEvents call site).
// stream_start is emitted once, at turn start — so a client that subscribes
// afterwards never sees one, and its buffered events would sit there until the
// next turn. Re-emitting it on subscribe is what unblocks them.
//
// The question MUST be emitted before the stream_start. A late subscriber has
// no question bubble (it missed the live user_message, or that event was
// dropped because it held no subscription at send time), so without this it
// renders the reply with nothing above it — the "assistant message but no user
// message" symptom, repairable only by a full history reload. The frontend's
// queue-panel rebuild is no substitute: it populates the queue panel, not the
// conversation.
//
// No-op when nothing is streaming, or when the run has no question (scheduled
// runs persist their prompt as a user row, but a run with no user row at all
// still gets its stream_start).
func (h *StreamHub) EmitLiveRunStateToClient(clientID, sessionID string) {
	h.mu.RLock()
	stateFn := h.streamStateFn
	h.mu.RUnlock()

	if stateFn == nil {
		return
	}
	msgID, questionID, questionContent := stateFn(sessionID)
	if msgID <= 0 {
		return
	}
	if questionID > 0 {
		h.EmitUserMessageEvent(clientID, sessionID, questionID, questionContent)
	}
	h.EmitStreamStartEvent(clientID, sessionID, msgID)
}

// emitStateEvent sends a single chat_stream state event to a specific client.
func (h *StreamHub) emitStateEvent(clientID, sessionID, eventType string, payload any) {
	msg := ServerMessage{
		Type:  MessageTypeEvent,
		ID:    GenerateEventID(),
		Event: "chat_stream",
		Data: ChatStreamData{
			SessionID: sessionID,
			EventType: eventType,
			Payload:   payload,
		},
	}
	h.mgr.SendToClient(clientID, msg)
}
