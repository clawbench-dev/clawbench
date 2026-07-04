package summarize

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
)

// --- NewAIBackendSummarizer ---

func TestNewAIBackendSummarizer_UnsupportedBackend(t *testing.T) {
	_, err := NewAIBackendSummarizer("nonexistent_backend_type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create AI backend")
}

// --- AIBackendSummarizer.Summarize with mock backend ---

// mockAIBackend implements ai.AIBackend for testing
type mockAIBackend struct {
	name          string
	streamCh      chan ai.StreamEvent
	executeErr    error
	executeCalled bool
}

func (m *mockAIBackend) Name() string { return m.name }

func (m *mockAIBackend) ExecuteStream(ctx context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	m.executeCalled = true
	if m.executeErr != nil {
		return nil, m.executeErr
	}
	return m.streamCh, nil
}

func TestAIBackendSummarizer_Summarize_ShortText(t *testing.T) {
	// Short text should bypass the AI backend entirely
	s := &AIBackendSummarizer{
		backend: &mockAIBackend{name: "test"},
		gs:      NewTTSPipeline(func(ctx context.Context, text, systemPrompt string, pass int) (string, error) { return "", nil }),
	}

	result, err := s.Summarize(context.Background(), "短文本", "zh")
	assert.NoError(t, err)
	assert.Equal(t, "短文本", result)
}

func TestAIBackendSummarizer_doSummarizePass_Success(t *testing.T) {
	ch := make(chan ai.StreamEvent, 3)
	ch <- ai.StreamEvent{Type: "content", Content: "This is "}
	ch <- ai.StreamEvent{Type: "content", Content: "a summary."}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	result, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.NoError(t, err)
	assert.Equal(t, "This is a summary.", result)
	assert.True(t, mock.executeCalled)
}

func TestAIBackendSummarizer_doSummarizePass_ExecuteError(t *testing.T) {
	mock := &mockAIBackend{
		name:       "mock-backend",
		executeErr: fmt.Errorf("CLI not available"),
	}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	_, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start")
	assert.Contains(t, err.Error(), "CLI not available")
}

func TestAIBackendSummarizer_doSummarizePass_StreamError(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "error", Error: "out of tokens"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	_, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of tokens")
}

func TestAIBackendSummarizer_doSummarizePass_EmptyOutput(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	_, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty output")
}

func TestAIBackendSummarizer_doSummarizePass_WhitespaceOnlyOutput(t *testing.T) {
	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "content", Content: "   \n\t  "}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	_, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty output")
}

func TestAIBackendSummarizer_doSummarizePass_MultipleContentEvents(t *testing.T) {
	ch := make(chan ai.StreamEvent, 5)
	ch <- ai.StreamEvent{Type: "content", Content: "First "}
	ch <- ai.StreamEvent{Type: "content", Content: "Second "}
	ch <- ai.StreamEvent{Type: "thinking", Content: "should be ignored"}
	ch <- ai.StreamEvent{Type: "content", Content: "Third"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	result, err := s.DoSummarizePass(context.Background(), "long text", "system prompt", 1)
	assert.NoError(t, err)
	assert.Equal(t, "First Second Third", result)
}

func TestAIBackendSummarizer_doSummarizePass_CancelledContext(t *testing.T) {
	mock := &mockAIBackend{
		name:       "mock-backend",
		executeErr: context.Canceled,
	}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.DoSummarizePass(ctx, "long text", "system prompt", 1)
	assert.Error(t, err)
}

func TestAIBackendSummarizer_ModelOverride(t *testing.T) {
	s := &AIBackendSummarizer{
		backend: &mockAIBackend{name: "test"},
		Model:   "custom-model-v2",
	}
	assert.Equal(t, "custom-model-v2", s.Model)
}

func TestAIBackendSummarizer_doSummarizePass_PassNumber(t *testing.T) {
	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "content", Content: "pass result"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
	}

	// Pass 2
	result, err := s.DoSummarizePass(context.Background(), "text", "prompt", 2)
	assert.NoError(t, err)
	assert.Equal(t, "pass result", result)
}

// --- Full Summarize pipeline test ---

func TestAIBackendSummarizer_Summarize_LongText_WithMockBackend(t *testing.T) {
	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "content", Content: "这是一个总结结果。"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	mock := &mockAIBackend{name: "mock-backend", streamCh: ch}
	s := &AIBackendSummarizer{
		backend: mock,
		gs:      NewTTSPipeline((&AIBackendSummarizer{backend: mock}).DoSummarizePass),
	}

	// Use ttsPipeline directly with a pass function
	s.gs = NewTTSPipeline(s.DoSummarizePass)

	longText := strings.Repeat("这是一段很长的AI回复内容，用于测试总结功能。", 20)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	assert.Equal(t, "这是一个总结结果。", result)
}
