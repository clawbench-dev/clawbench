package ai

import (
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// toolCallDebouncer batches rapid ToolCallUpdate events for the same tool ID
// to reduce the number of SSE events sent to the frontend. ACP agents emit
// ToolCallUpdate deltas every ~30ms during tool input streaming, but the
// frontend only needs the final accumulated input for rendering.
//
// Batching strategy:
//   - On the first ToolCallUpdate for a tool ID, start a 50ms timer.
//   - Subsequent updates for the same tool ID are merged (input/output overwritten).
//   - When the timer fires, the merged event is forwarded to the stream channel.
//   - Terminal events (completed/failed) flush immediately without waiting.
//   - ToolCall start events (from update.ToolCall) flush any pending batch first.
type toolCallDebouncer struct {
	mu      sync.Mutex
	pending map[string]*pendingToolUpdate // toolCallID → accumulated update
	ch      chan<- StreamEvent
	conn    *ACPConn
}

type pendingToolUpdate struct {
	event    StreamEvent
	timer    *time.Timer
	rawInput any // latest RawInput from ACP (for merging)
}

const toolDebounceInterval = 50 * time.Millisecond

// newToolCallDebouncer creates a debouncer that forwards merged events to ch.
func newToolCallDebouncer(ch chan<- StreamEvent, conn *ACPConn) *toolCallDebouncer {
	return &toolCallDebouncer{
		pending: make(map[string]*pendingToolUpdate),
		ch:      ch,
		conn:    conn,
	}
}

// handleToolCallUpdate processes a ToolCallUpdate through the debouncer.
// Returns true if the event was buffered (caller should NOT forward it directly).
func (d *toolCallDebouncer) handleToolCallUpdate(tcu acp.SessionToolCallUpdate) bool {
	toolCallID := string(tcu.ToolCallId)
	backendID := ""
	if d.conn != nil {
		backendID = d.conn.BackendID()
	}
	event := mapACPToolCallUpdate(tcu, backendID)

	// Terminal events (completed/failed) flush immediately.
	if event.Tool != nil && event.Tool.Done {
		d.flushToolID(toolCallID)
		forwardACPEvent(d.ch, event)
		return true
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.pending[toolCallID]; ok {
		// Merge: overwrite with latest data.
		existing.event = event
		if tcu.RawInput != nil {
			existing.rawInput = tcu.RawInput
		}
		// Timer is already running — just wait for it.
		return true
	}

	// First update for this tool ID — start debounce timer.
	pending := &pendingToolUpdate{
		event: event,
	}
	if tcu.RawInput != nil {
		pending.rawInput = tcu.RawInput
	}
	d.pending[toolCallID] = pending

	pending.timer = time.AfterFunc(toolDebounceInterval, func() {
		d.flushToolID(toolCallID)
	})
	return true
}

// handleToolCall processes an initial ToolCall event. It flushes any pending
// batch for the same tool ID (shouldn't happen normally, but be safe).
func (d *toolCallDebouncer) handleToolCall(tc acp.SessionUpdateToolCall) {
	d.flushToolID(string(tc.ToolCallId))
}

// flushToolID sends the pending merged event for a tool ID and removes it.
func (d *toolCallDebouncer) flushToolID(toolCallID string) {
	d.mu.Lock()
	pending, ok := d.pending[toolCallID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.pending, toolCallID)
	d.mu.Unlock()

	if pending.timer != nil {
		pending.timer.Stop()
	}
	forwardACPEvent(d.ch, pending.event)
}

// flushAll sends all pending events immediately. Called when the session ends.
func (d *toolCallDebouncer) flushAll() {
	d.mu.Lock()
	pendings := make(map[string]*pendingToolUpdate, len(d.pending))
	for k, v := range d.pending {
		pendings[k] = v
	}
	d.pending = make(map[string]*pendingToolUpdate)
	d.mu.Unlock()

	for _, pending := range pendings {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		forwardACPEvent(d.ch, pending.event)
	}
}
