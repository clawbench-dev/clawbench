package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMockBackend implements AIBackend for testing.
// Each call to ExecuteStream returns events from the next TestMockStream entry.
type TestMockBackend struct {
	name      string
	streams   []TestMockStream
	callCount int
	mu        sync.Mutex
}

// TestMockStream defines the events and optional error for a single ExecuteStream call.
type TestMockStream struct {
	events []StreamEvent
	err    error
}

func (m *TestMockBackend) Name() string {
	return m.name
}

func (m *TestMockBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	m.mu.Lock()
	idx := m.callCount
	m.callCount++
	m.mu.Unlock()

	if idx >= len(m.streams) {
		// No more streams configured — return empty closed channel
		ch := make(chan StreamEvent)
		close(ch)
		return ch, nil
	}

	stream := m.streams[idx]
	if stream.err != nil {
		return nil, stream.err
	}

	ch := make(chan StreamEvent, len(stream.events)+1)
	for _, e := range stream.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// --- Tests ---

func TestAutoResume_TransparentPassThrough(t *testing.T) {
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			{
				events: []StreamEvent{
					{Type: "content", Content: "hello "},
					{Type: "content", Content: "world"},
					{Type: "done"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	assert.Equal(t, 3, len(events)) // "hello ", "world", "done" — "done" is now forwarded
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "hello ", events[0].Content)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "world", events[1].Content)
	assert.Equal(t, "done", events[2].Type)
}

func TestAutoResume_ExitPlanModeDetection(t *testing.T) {
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: contains ExitPlanMode
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
				},
			},
			// Second stream (resume)
			{
				events: []StreamEvent{
					{Type: "content", Content: "continuing..."},
					{Type: "done"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{
		SessionID: "test",
		WorkDir:   "/tmp",
	})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expected: content, tool_use(ExitPlanMode), resume_split, content(resume), done
	assert.Equal(t, 5, len(events))
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "planning...", events[0].Content)
	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "ExitPlanMode", events[1].Tool.Name)
	assert.Equal(t, "resume_split", events[2].Type)
	assert.Equal(t, "content", events[3].Type)
	assert.Equal(t, "continuing...", events[3].Content)
	assert.Equal(t, "done", events[4].Type)

	// Verify mock was called twice (original + resume)
	assert.Equal(t, 2, mock.callCount)
}

func TestAutoResume_OuterCancelDuringFirstStream(t *testing.T) {
	// Use a custom backend that returns a blocked channel
	blockedCh := make(chan StreamEvent)
	customBackend := &blockingBackend{
		name: "test",
		ch:   blockedCh,
	}

	wrapper := &AutoResumeBackend{inner: customBackend}
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	// Cancel the outer context
	cancel()

	// The outer channel should close promptly (may receive a "done" event first
	// if the blockingBackend's goroutine races to close before the cancel is detected)
	select {
	case event, ok := <-ch:
		if ok {
			// May receive a "done" event before channel closes
			assert.Equal(t, "done", event.Type, "only 'done' event expected before close")
			// Now drain until closed
			for range ch {
			}
		}
		// Channel is closed — this is the expected outcome
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: outer channel did not close after cancel")
	}
}

func TestAutoResume_OuterCancelDuringResume(t *testing.T) {
	// Use a two-phase backend: first stream has ExitPlanMode, second stream blocks
	firstDone := make(chan struct{})
	backend := &twoPhaseBackend{
		firstEvents: []StreamEvent{
			{Type: "content", Content: "planning..."},
			{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
		},
		secondBlocked: true,
		firstDone:     firstDone,
	}

	wrapper := &AutoResumeBackend{inner: backend}
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	// Drain Phase 1 events (content, tool_use, resume_split) from the channel.
	// Once resume_split is received, the resume stream is active and we can cancel.
	gotResumeSplit := false
	for !gotResumeSplit {
		event, ok := <-ch
		if !ok {
			t.Fatal("channel closed unexpectedly before cancel")
		}
		if event.Type == "resume_split" {
			gotResumeSplit = true
		}
	}

	// Now cancel the outer context during the resume phase
	cancel()

	// The outer channel should close promptly
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after outer cancel during resume")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: outer channel did not close after cancel during resume")
	}
}

func TestAutoResume_ResumeStreamFailure(t *testing.T) {
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: ExitPlanMode
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
				},
			},
			// Second stream: error
			{err: context.DeadlineExceeded},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expected: content, tool_use, resume_split, done (graceful degradation)
	assert.Equal(t, 4, len(events))
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "resume_split", events[2].Type)
	assert.Equal(t, "done", events[3].Type) // fallback done event
}

func TestAutoResume_RawOutputHandling(t *testing.T) {
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: ExitPlanMode with raw_output
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
					{Type: "raw_output", RawOutput: "first-raw"},
				},
			},
			// Second stream: has raw_output that should be suppressed
			{
				events: []StreamEvent{
					{Type: "raw_output", RawOutput: "second-raw"},
					{Type: "content", Content: "continued"},
					{Type: "done"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Only first stream's raw_output should be present
	hasFirstRaw := false
	hasSecondRaw := false
	for _, e := range events {
		if e.Type == "raw_output" {
			if e.RawOutput == "first-raw" {
				hasFirstRaw = true
			}
			if e.RawOutput == "second-raw" {
				hasSecondRaw = true
			}
		}
	}
	assert.True(t, hasFirstRaw, "first stream raw_output should be forwarded")
	assert.False(t, hasSecondRaw, "second stream raw_output should be suppressed")
}

func TestAutoResume_NoNestedExitPlanMode(t *testing.T) {
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: ExitPlanMode
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
				},
			},
			// Resume stream: also has ExitPlanMode (should be forwarded, not trigger another resume)
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning again..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "2", Done: true}},
					{Type: "done"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Should only have 2 calls (no nested resume), and second ExitPlanMode
	// should be forwarded as a normal event. "done" from resume stream is also forwarded.
	assert.Equal(t, 2, mock.callCount, "should not trigger nested resume")

	// Find second ExitPlanMode tool_use
	var secondExitPlanMode *StreamEvent
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "tool_use" && events[i].Tool != nil && events[i].Tool.Name == "ExitPlanMode" && events[i].Tool.ID == "2" {
			secondExitPlanMode = &events[i]
			break
		}
	}
	assert.NotNil(t, secondExitPlanMode, "second ExitPlanMode should be forwarded as-is")
}

// --- Helpers ---

// blockingBackend returns a channel that never sends events (for cancel tests).
type blockingBackend struct {
	name string
	ch   chan StreamEvent
}

func (b *blockingBackend) Name() string { return b.name }

func (b *blockingBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	outCh := make(chan StreamEvent)
	go func() {
		defer close(outCh)
		select {
		case <-ctx.Done():
		case e, ok := <-b.ch:
			if ok {
				outCh <- e
			}
		}
	}()
	return outCh, nil
}

// twoPhaseBackend returns a first stream with specific events (e.g. ExitPlanMode),
// and a second stream that blocks until context cancellation.
type twoPhaseBackend struct {
	name          string
	firstEvents   []StreamEvent
	secondBlocked bool
	firstDone     chan struct{} // closed after first ExecuteStream returns its events
	callCount     int
	mu            sync.Mutex
}

func (b *twoPhaseBackend) Name() string { return b.name }

func (b *twoPhaseBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	b.mu.Lock()
	idx := b.callCount
	b.callCount++
	b.mu.Unlock()

	outCh := make(chan StreamEvent)

	switch {
	case idx == 0:
		// First stream: send events and close
		go func() {
			defer close(outCh)
			for _, e := range b.firstEvents {
				outCh <- e
			}
			close(b.firstDone)
		}()
	case b.secondBlocked:
		// Second stream: block until context cancelled
		go func() {
			defer close(outCh)
			<-ctx.Done()
		}()
	default:
		close(outCh)
	}

	return outCh, nil
}

func TestAutoResume_FirstCallError(t *testing.T) {
	// When the first ExecuteStream call returns an error, the wrapper
	// should propagate the error and close the outer channel.
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			{err: context.DeadlineExceeded},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.Error(t, err, "first call error should be propagated")
	assert.Nil(t, ch, "channel should be nil on error")
}

func TestAutoResume_ResumeChannelClosedWithoutDone(t *testing.T) {
	// When the resume stream closes without emitting a "done" event,
	// the wrapper should emit a synthetic "done" so downstream can finalize.
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: ExitPlanMode
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
				},
			},
			// Second stream: empty (no events, just closed)
			{
				events: []StreamEvent{},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expected: content, tool_use, resume_split, done (synthetic)
	assert.Equal(t, 4, len(events))
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "ExitPlanMode", events[1].Tool.Name)
	assert.Equal(t, "resume_split", events[2].Type)
	assert.Equal(t, "done", events[3].Type, "synthetic 'done' should be emitted when resume channel closes without done")
}

func TestAutoResume_FirstStreamClosedWithoutDone(t *testing.T) {
	// When the first stream closes normally (no ExitPlanMode, no "done"),
	// the wrapper should emit a synthetic "done" event.
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: just content, no "done"
			{
				events: []StreamEvent{
					{Type: "content", Content: "hello"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expected: content, done (synthetic since first stream closed without "done")
	assert.Equal(t, 2, len(events))
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "done", events[1].Type, "synthetic 'done' should be emitted when first stream closes without done")
}

// --- forwardEvent tests ---

func TestAutoResume_OuterCancelDuringDrain(t *testing.T) {
	// ISS-296: After cancelling inner CLI on ExitPlanMode, the drain loop
	// must respect ctx.Done() instead of blocking indefinitely on innerCh.
	// Use a slow-draining backend: after ExitPlanMode, the inner channel
	// blocks (simulating an unresponsive CLI process that doesn't react to context cancellation).
	slowDrainCh := make(chan StreamEvent)
	backend := &slowDrainBackend{
		firstEvents: []StreamEvent{
			{Type: "content", Content: "planning..."},
			{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
		},
		drainCh: slowDrainCh,
	}

	wrapper := &AutoResumeBackend{inner: backend}
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	// Drain Phase 1 events until resume_split
	gotResumeSplit := false
	for !gotResumeSplit {
		event, ok := <-ch
		if !ok {
			t.Fatal("channel closed unexpectedly before drain")
		}
		if event.Type == "resume_split" {
			gotResumeSplit = true
		}
	}

	// Now the drain loop is running. The inner CLI is unresponsive (slowDrainBackend
	// ignores inner context cancellation). Cancel the outer context.
	// Before ISS-296 fix, this would block indefinitely because the drain
	// loop used `range innerCh` without checking ctx.Done().
	cancel()

	// The outer channel should close promptly (within 2s)
	// May receive a "done" event before closing due to goroutine scheduling
	select {
	case event, ok := <-ch:
		if ok {
			// May receive a "done" event before channel closes
			assert.Equal(t, "done", event.Type, "only 'done' event expected before close during drain cancel")
			// Drain until closed
			for range ch {
			}
		}
		// Channel is closed — this is the expected outcome
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: outer channel did not close after cancel during drain — drain loop may be blocking on innerCh without ctx.Done() check (ISS-296)")
	}

	// Clean up the slowDrainBackend goroutine by closing its drainCh
	close(slowDrainCh)
}

func TestAutoResume_DrainForwardsRawOutputThenResumes(t *testing.T) {
	// Verify that the drain loop (after ExitPlanMode cancel) forwards raw_output
	// events, and that the drain exits cleanly when the inner channel closes,
	// allowing Phase 2 (resume) to proceed normally.
	mock := &TestMockBackend{
		name: "test",
		streams: []TestMockStream{
			// First stream: ExitPlanMode + raw_output after it
			{
				events: []StreamEvent{
					{Type: "content", Content: "planning..."},
					{Type: "tool_use", Tool: &ToolCall{Name: "ExitPlanMode", ID: "1", Done: true}},
					{Type: "raw_output", RawOutput: "drain-raw-1"},
					{Type: "raw_output", RawOutput: "drain-raw-2"},
				},
			},
			// Resume stream
			{
				events: []StreamEvent{
					{Type: "content", Content: "resumed..."},
					{Type: "done"},
				},
			},
		},
	}

	wrapper := &AutoResumeBackend{inner: mock}
	ctx := context.Background()

	ch, err := wrapper.ExecuteStream(ctx, ChatRequest{SessionID: "test"})
	assert.NoError(t, err)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expected: content, tool_use(ExitPlanMode), resume_split,
	// raw_output(drain-raw-1), raw_output(drain-raw-2),
	// content(resume), done
	assert.Equal(t, 7, len(events))
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "resume_split", events[2].Type)
	assert.Equal(t, "raw_output", events[3].Type)
	assert.Equal(t, "drain-raw-1", events[3].RawOutput)
	assert.Equal(t, "raw_output", events[4].Type)
	assert.Equal(t, "drain-raw-2", events[4].RawOutput)
	assert.Equal(t, "content", events[5].Type)
	assert.Equal(t, "resumed...", events[5].Content)
	assert.Equal(t, "done", events[6].Type)
}

// slowDrainBackend returns firstEvents in the first stream, then keeps the channel
// open with a separate drainCh for simulating slow/unresponsive CLI after ExitPlanMode.
type slowDrainBackend struct {
	name        string
	firstEvents []StreamEvent
	drainCh     chan StreamEvent
	callCount   int
	mu          sync.Mutex
}

func (b *slowDrainBackend) Name() string { return b.name }

func (b *slowDrainBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	b.mu.Lock()
	idx := b.callCount
	b.callCount++
	b.mu.Unlock()

	outCh := make(chan StreamEvent)

	if idx == 0 {
		go func() {
			defer close(outCh)
			// Send first events (including ExitPlanMode)
			for _, e := range b.firstEvents {
				outCh <- e
			}
			// Block on drainCh (simulates unresponsive CLI).
			// IMPORTANT: We do NOT check ctx.Done() here because the real CLI process
			// may not respond to context cancellation promptly. The drain loop in
			// mergeStreams must use the outer ctx.Done() to exit when the CLI is stuck.
			// Closing drainCh simulates the CLI eventually closing.
			for e := range b.drainCh {
				outCh <- e
			}
		}()
	} else {
		// Resume stream: just close immediately
		close(outCh)
	}

	return outCh, nil
}

func TestForwardEvent_ChannelFull(t *testing.T) {
	// Create a channel with buffer size 1 and fill it
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "first"}

	// ForwardEvent should not block when channel is full (drops the event)
	forwardEvent(ch, StreamEvent{Type: "content", Content: "dropped"})

	assert.Equal(t, 1, len(ch))
	ev := <-ch
	assert.Equal(t, "first", ev.Content)
}

func TestForwardEvent_ChannelEmpty(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	forwardEvent(ch, StreamEvent{Type: "content", Content: "hello"})

	assert.Equal(t, 1, len(ch))
	ev := <-ch
	assert.Equal(t, "hello", ev.Content)
}
