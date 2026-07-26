package ws

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"clawbench/internal/ai"
)

// StreamHub manages session-scoped streaming event fan-out via WebSocket.
// It replaces the single-consumer SSE channel with multi-client WS delivery.
// Clients subscribe to specific sessions to receive their streaming events.
type StreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]struct{} // sessionID -> set of clientIDs
	mgr         *Manager
}

// NewStreamHub creates a StreamHub associated with the given Manager.
func NewStreamHub(mgr *Manager) *StreamHub {
	return &StreamHub{
		subscribers: make(map[string]map[string]struct{}),
		mgr:         mgr,
	}
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

// HasSubscribers returns true if any client is subscribed to the session.
func (h *StreamHub) HasSubscribers(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	subs, ok := h.subscribers[sessionID]
	return ok && len(subs) > 0
}

// Emit fans out a streaming event to all subscribed WS clients for a session.
func (h *StreamHub) Emit(sessionID string, event ai.StreamEvent) {
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

	// Convert StreamEvent to WS message
	payload := StreamEventToPayload(event)
	if payload == nil {
		return
	}

	msg := ServerMessage{
		Type:  MessageTypeEvent,
		ID:    GenerateEventID(),
		Event: "chat_stream",
		Data: ChatStreamData{
			SessionID: sessionID,
			EventType: event.Type,
			Payload:   payload,
		},
	}

	// Send to each subscriber
	for _, clientID := range clientIDs {
		h.mgr.SendToClient(clientID, msg)
	}
}

// EmitToSession emits a StreamEvent to all subscribers of a session.
// This is a convenience function that retrieves the Manager and StreamHub,
// checking for nil and subscriber presence. Use this instead of duplicating
// the nil-check pattern at each call site.
func EmitToSession(sessionID string, event ai.StreamEvent) {
	mgr := GetManager()
	if mgr == nil {
		return
	}
	hub := mgr.StreamHub()
	if hub == nil || !hub.HasSubscribers(sessionID) {
		return
	}
	hub.Emit(sessionID, event)
}

// StreamEventToPayload converts an ai.StreamEvent to the payload data
// that was previously written as SSE `data:` fields. The payload format
// is kept identical to the SSE format for frontend compatibility.
func StreamEventToPayload(event ai.StreamEvent) any {
	// Simple empty-payload signal events
	switch event.Type {
	case "thinking_done", "done", "replay_done":
		return map[string]any{}
	case "resume_split":
		return nil
	}

	switch event.Type {
	case "content":
		return map[string]string{"content": event.Content}
	case "thinking":
		return map[string]string{"text": event.Content}
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
	case "queue_drain":
		return queueDrainPayload(event)
	case "queue_cancel":
		return queueCancelPayload(event)
	default:
		return acpStatePayload(event)
	}
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
}

func userMessagePayload(event ai.StreamEvent) any {
	if event.UserMessage == nil {
		return nil
	}
	payload := map[string]any{
		"messageId": event.UserMessage.MessageID,
		"content":   event.UserMessage.Content,
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

func errorPayload(event ai.StreamEvent) any {
	payload := map[string]string{"error": event.Error}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	return payload
}

func warningPayload(event ai.StreamEvent) any {
	payload := map[string]string{"text": event.Content}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	return payload
}

func queueDrainPayload(event ai.StreamEvent) any {
	if event.QueueEvent == nil {
		return nil
	}
	return map[string]any{
		"sessionId": event.QueueEvent.SessionID,
		"queueId":   event.QueueEvent.QueueID,
		"text":      event.QueueEvent.Text,
		"messageId": event.QueueEvent.MessageID,
		"filePaths": event.QueueEvent.FilePaths,
		"files":     event.QueueEvent.Files,
		"queue":     event.QueueEvent.Queue,
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
	} else if ou := ai.GetACPConnManager().GetOrphanedUsageState(sessionID); ou != nil {
		h.emitStateEvent(clientID, sessionID, "usage_update", ou)
		slog.Debug("streamhub: re-emitted orphaned usage on subscribe", "session_id", sessionID, "client_id", clientID)
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

// EmitStreamStartEvent sends a stream_start chat_stream event with the streaming message ID.
func (h *StreamHub) EmitStreamStartEvent(clientID, sessionID string, messageID int64) {
	h.emitStateEvent(clientID, sessionID, "stream_start", map[string]int64{"message_id": messageID})
}

// EmitResumeSplitEvent sends a resume_split chat_stream event to all subscribers.
// The message_id is injected from the caller since it's only available after
// handleResumeSplit creates the new streaming message.
func (h *StreamHub) EmitResumeSplitEvent(sessionID string, messageID int64) {
	h.mu.RLock()
	subs, ok := h.subscribers[sessionID]
	if !ok || len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	clientIDs := make([]string, 0, len(subs))
	for id := range subs {
		clientIDs = append(clientIDs, id)
	}
	h.mu.RUnlock()

	payload := map[string]any{}
	if messageID > 0 {
		payload["message_id"] = messageID
	}

	msg := ServerMessage{
		Type:  MessageTypeEvent,
		ID:    GenerateEventID(),
		Event: "chat_stream",
		Data: ChatStreamData{
			SessionID: sessionID,
			EventType: "resume_split",
			Payload:   payload,
		},
	}

	for _, clientID := range clientIDs {
		h.mgr.SendToClient(clientID, msg)
	}
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
