package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/summarize"

	"github.com/stretchr/testify/assert"
)

// mockSummarizer is a test double for summarize.Summarizer.
type mockSummarizer struct {
	result       string
	err          error
	called       bool
	lastText     string
	lastLanguage string
}

func (m *mockSummarizer) Summarize(ctx context.Context, text string, language string) (string, error) {
	m.called = true
	m.lastText = text
	m.lastLanguage = language
	if m.err != nil {
		return "", m.err
	}
	if m.result != "" {
		return m.result, nil
	}
	return "summary of: " + text, nil
}

// mockSpeechProvider is a test double for speech.SpeechProvider.
type mockSpeechProvider struct {
	synthesizeErr    error
	synthesizeCalled bool
	lastSynthText    string
	lastSynthLang    string
	// synthesizeBlock, if non-nil, is closed before Synthesize returns.
	// Set before starting the job to keep the goroutine alive until the
	// stream endpoint has connected (avoids race where the goroutine
	// finishes and unregisters the job before TTSStream can look it up).
	synthesizeBlock chan struct{}
}

func (m *mockSpeechProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error {
	m.synthesizeCalled = true
	m.lastSynthText = text
	m.lastSynthLang = language
	if m.synthesizeErr != nil {
		return m.synthesizeErr
	}
	// Create a dummy audio file at outputPath
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte("fake audio data"), 0o644); err != nil {
		return err
	}
	// Block until the test releases us (if configured)
	if m.synthesizeBlock != nil {
		select {
		case <-m.synthesizeBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// setupTTSTest sets up a test environment with mock provider and summarizer.
// Returns the test env and a teardown function.
func setupTTSTest(t *testing.T, mockProvider *mockSpeechProvider, mockSum *mockSummarizer) (*testEnv, func()) {
	t.Helper()

	// Use the shared test environment (inits DB, project dir, etc.)
	env, envTeardown := setupTestEnv(t)

	// Save and replace the global speech provider and summarizer
	origProvider := GetSpeechProvider()
	origSummarizer := GetSummarizer()
	SetSpeechProvider(mockProvider)
	SetSummarizer(mockSum)

	teardown := func() {
		SetSpeechProvider(origProvider)
		SetSummarizer(origSummarizer)
		envTeardown()
	}

	return env, teardown
}

// --- TTSGenerate: method validation ---

func TestTTSGenerate_MethodNotAllowed(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	_, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/tts/generate", http.NoBody)
	req = withProjectCookie(req, t.TempDir())
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.False(t, mockSum.called)
}

// --- TTSGenerate: empty text ---

func TestTTSGenerate_EmptyText(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": ""})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, mockSum.called)
}

// --- TTSGenerate: text too long ---

func TestTTSGenerate_TextTooLong(t *testing.T) {
	// Temporarily set MaxTextRunes to enforce a limit
	origMaxTextRunes := summarize.MaxTextRunes
	summarize.MaxTextRunes = 100
	defer func() { summarize.MaxTextRunes = origMaxTextRunes }()

	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	longText := strings.Repeat("x", 101)
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": longText})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, mockSum.called)
}

// --- TTSGenerate: successful generation returns jobId ---

func TestTTSGenerate_Success(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "这是核心结论"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Response should be JSON with jobId
	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "jobId")
	assert.NotEmpty(t, resp["jobId"])

	// Wait for the background goroutine to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Verify the mock was called
	assert.True(t, mockSum.called)
	assert.True(t, mockProvider.synthesizeCalled)
}

// --- TTSGenerate: summarize failure returns error, does not synthesize ---

func TestTTSGenerate_SummarizeFailure(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{err: context.DeadlineExceeded}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这是一段需要总结的长文本内容，但由于摘要失败会直接报错。内容足够长以触发摘要流程。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for the background goroutine to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Summarizer was called, but synthesize should NOT be called
	assert.True(t, mockSum.called)
	assert.False(t, mockProvider.synthesizeCalled)
}

// --- TTSGenerate: synthesize failure returns error via SSE stream ---

func TestTTSGenerate_SynthesizeFailure(t *testing.T) {
	mockProvider := &mockSpeechProvider{synthesizeErr: context.DeadlineExceeded}
	mockSum := &mockSummarizer{result: "总结文本"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "测试语音合成失败的场景。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "jobId")

	// Wait for job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	assert.True(t, mockSum.called)
	assert.True(t, mockProvider.synthesizeCalled)
}

// --- TTSGenerate: cache hit returns JSON directly ---

func TestTTSGenerate_CacheHit(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这段文本会被缓存。"
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	relAudioPath := filepath.Join(".clawbench", "generated", "tts", cacheKey+".mp3")
	absAudioPath := filepath.Join(env.ProjectDir, relAudioPath)

	// Pre-create the cached file
	if err := os.MkdirAll(filepath.Dir(absAudioPath), 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := os.WriteFile(absAudioPath, []byte("cached audio"), 0o644); err != nil {
		t.Fatalf("failed to write cache file: %v", err)
	}

	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Cache hit returns JSON directly (not SSE)
	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, true, resp["cached"])
	assert.Equal(t, relAudioPath, resp["audioPath"])

	// Provider should NOT be called on cache hit
	assert.False(t, mockSum.called)
	assert.False(t, mockProvider.synthesizeCalled)
}

// --- TTSGenerate: invalid JSON body ---

func TestTTSGenerate_InvalidJSON(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/tts/generate", strings.NewReader(`{invalid json}`))
	req.Header.Set("Content-Type", "application/json")
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, mockSum.called)
}

// --- TTSGenerate: cache key is deterministic ---

func TestTTSGenerate_CacheKeyDeterministic(t *testing.T) {
	text := "同样的文本应该产生同样的缓存键"
	hash1 := sha256.Sum256([]byte(text))
	key1 := hex.EncodeToString(hash1[:])[:summarize.CacheKeyHexLen]

	hash2 := sha256.Sum256([]byte(text))
	key2 := hex.EncodeToString(hash2[:])[:summarize.CacheKeyHexLen]

	assert.Equal(t, key1, key2)
	assert.Len(t, key1, summarize.CacheKeyHexLen)
}

// --- SetSpeechProvider and SetSummarizer ---

func TestSetSpeechProvider(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	mock := &mockSpeechProvider{}
	SetSpeechProvider(mock)

	// Verify the global was replaced
	assert.Equal(t, GetSpeechProvider(), mock)
}

func TestSetSummarizer(t *testing.T) {
	origSum := GetSummarizer()
	defer SetSummarizer(origSum)

	mock := &mockSummarizer{}
	SetSummarizer(mock)

	// Verify the global was replaced
	assert.Equal(t, GetSummarizer(), mock)
}

// --- ensure mockSummarizer satisfies Summarizer interface ---
var _ summarize.Summarizer = (*mockSummarizer)(nil)

// --- ensure mockSpeechProvider satisfies SpeechProvider interface ---
var _ speech.SpeechProvider = (*mockSpeechProvider)(nil)

// --- GetSpeechProvider / SetSpeechProvider concurrent access ---

func TestGetSpeechProvider_ConcurrentAccess(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	altProvider := &mockSpeechProvider{}
	done := make(chan struct{})

	// Concurrent setter goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			SetSpeechProvider(altProvider)
			SetSpeechProvider(origProvider)
		}
	}()

	// Concurrent reader goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			_ = GetSpeechProvider()
		}
	}()

	// Wait for both goroutines (no data race = test passes under -race)
	<-done
	<-done
}

// --- GetSummarizer / SetSummarizer concurrent access ---

func TestGetSummarizer_ConcurrentAccess(t *testing.T) {
	origSum := GetSummarizer()
	defer SetSummarizer(origSum)

	altSum := &mockSummarizer{}
	done := make(chan struct{})

	// Concurrent setter goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			SetSummarizer(altSum)
			SetSummarizer(origSum)
		}
	}()

	// Concurrent reader goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			_ = GetSummarizer()
		}
	}()

	// Wait for both goroutines (no data race = test passes under -race)
	<-done
	<-done
}

// --- TTSGenerate: language propagation ---

func TestTTSGenerate_LanguageDefaultToZh(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "核心结论"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	// Send request WITHOUT language field
	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	if job, ok := service.GetTTSJob(cacheKey); ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Language should default to "zh"
	assert.Equal(t, "zh", mockSum.lastLanguage)
	assert.Equal(t, "zh", mockProvider.lastSynthLang)
}

func TestTTSGenerate_LanguageEn(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "Core conclusion"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	// Send request WITH language="en"
	text := "This is a long AI response that contains detailed analysis and code examples, requiring core point extraction."
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text, "language": "en"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	if job, ok := service.GetTTSJob(cacheKey); ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	assert.Equal(t, "en", mockSum.lastLanguage)
	assert.Equal(t, "en", mockProvider.lastSynthLang)
}

func TestTTSGenerate_LanguageJa(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "核心の結論"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "これは長いAIの応答内容であり、音声に要約する必要があります。詳細な分析とコード例が含まれています。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text, "language": "ja"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	if job, ok := service.GetTTSJob(cacheKey); ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	assert.Equal(t, "ja", mockSum.lastLanguage)
	assert.Equal(t, "ja", mockProvider.lastSynthLang)
}

// --- TTSStream: SSE streaming via EventSource-compatible format ---

func TestTTSStream_Success(t *testing.T) {
	block := make(chan struct{})
	mockProvider := &mockSpeechProvider{synthesizeBlock: block}
	mockSum := &mockSummarizer{result: "这是核心结论"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这是用于测试SSE流的文本内容，包含足够的文字以触发摘要流程。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]string{"text": text})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var genResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &genResp)
	jobID, _ := genResp["jobId"].(string)

	// Wait for the summarizing phase to be sent (ensures the goroutine is running)
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if !ok {
		t.Fatal("TTS job not found after TTSGenerate returned")
	}

	// Connect to stream endpoint while the job is still alive.
	// The mock synthesize blocks on `block`, so the goroutine won't finish
	// and unregister the job before we connect.
	streamReq := httptest.NewRequest(http.MethodGet, "/api/tts/stream/"+jobID, http.NoBody)
	streamReq = withProjectCookie(streamReq, env.ProjectDir)
	streamW := httptest.NewRecorder()

	// Release the mock so the job can finish after we've connected.
	close(block)

	TTSStream(streamW, streamReq)

	// Wait for the job to complete so events are fully flushed.
	select {
	case <-job.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("TTS job did not complete in time")
	}

	body := streamW.Body.String()
	assert.Contains(t, body, "event: phase")
	assert.Contains(t, body, "summarizing")
	assert.Contains(t, body, "synthesizing")
	assert.Contains(t, body, "event: result")
}

func TestTTSStream_JobNotFound(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/tts/stream/nonexistent", http.NoBody)
	req = withProjectCookie(req, t.TempDir())
	w := httptest.NewRecorder()

	TTSStream(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTTSStream_MissingJobID(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/tts/stream/", http.NoBody)
	req = withProjectCookie(req, t.TempDir())
	w := httptest.NewRecorder()

	TTSStream(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- TTSGenerate: MessageID-based summary lookup ---

func TestTTSGenerate_WithMessageID_CachedAudio(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "核心总结"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"

	// First request: generate and cache audio (with messageId for SaveTTSSummaryByMessageID)
	req1 := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": text, "messageId": 100})
	req1 = withProjectCookie(req1, env.ProjectDir)
	w1 := httptest.NewRecorder()
	TTSGenerate(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Wait for TTS job to complete so audio is cached and SaveTTSSummaryByMessageID is called
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Verify the summary was saved by the goroutine (covers SaveTTSSummaryByMessageID)
	savedSummary, found := service.GetTTSSummaryByMessageID(100)
	assert.True(t, found, "TTS summary should be saved for message ID 100")
	assert.Equal(t, "核心总结", savedSummary)

	// Second request with same text + messageId: should hit audio cache
	// and look up summary by message ID instead of cache key
	req2 := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": text, "messageId": 100})
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := httptest.NewRecorder()
	TTSGenerate(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp map[string]any
	err := json.Unmarshal(w2.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, true, resp["cached"])
}

func TestTTSGenerate_WithMessageID_SummaryCacheHit(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "核心总结"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	// Pre-save a TTS summary for message ID 200
	err := service.SaveTTSSummaryByMessageID(200, "预设的TTS总结")
	assert.NoError(t, err)

	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": text, "messageId": 200})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for TTS job to complete
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// The mock summarizer should NOT have been called since we had a cached summary
	assert.False(t, mockSum.called)
}

func TestTTSGenerate_WithZeroMessageID_SkipsMessageIDLookup(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "总结"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"
	// messageId = 0 means no message-based lookup
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": text, "messageId": 0})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "jobId")

	// Wait for completion
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Summarizer should have been called (no cache hit for messageId=0)
	assert.True(t, mockSum.called)
}

func TestTTSGenerate_SaveTTSSummaryByMessageID_DBError(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "核心总结"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	// Drop the tts_summaries table to force SaveTTSSummaryByMessageID to fail
	_, _ = service.UnsafeDBForTest().Exec("DROP TABLE tts_summaries")

	text := "这是一段较长的AI回复内容，需要被总结为语音。包含了详细的分析和代码示例，需要提取核心要点。"
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": text, "messageId": 300})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for the TTS job to complete (should still succeed despite DB error)
	hash := sha256.Sum256([]byte(text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	job, ok := service.GetTTSJob(cacheKey)
	if ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// Verify the job completed successfully despite the DB save error
	assert.True(t, mockSum.called)
}

// --- ttsExtractConclusion: extracts final answer from message blocks ---

func TestTTSExtractConclusion_LastAnswerAfterToolUse(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	// Insert a message with: text(intro) → tool_use → text(conclusion)
	content := `{"blocks":[{"type":"text","text":"Let me check the code."},{"type":"tool_use","name":"Read","id":"1","input":{"file_path":"/foo.go"}},{"type":"text","text":"The fix is to add a null check on line 42."}]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-tts-1', 'claude', 0)",
		content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	conclusion := ttsExtractConclusion(msgID)
	assert.Equal(t, "The fix is to add a null check on line 42.", conclusion)
}

func TestTTSExtractConclusion_NoToolUse_ReturnsLongestText(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	// Message with only text blocks (no tool_use) → longest text block
	content := `{"blocks":[{"type":"text","text":"Short intro."},{"type":"text","text":"This is the main answer with much more detail and explanation about the fix."}]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-tts-2', 'claude', 0)",
		content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	conclusion := ttsExtractConclusion(msgID)
	assert.Equal(t, "This is the main answer with much more detail and explanation about the fix.", conclusion)
}

func TestTTSExtractConclusion_WithAskUserQuestion(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	// Message: text(conclusion) → AskUserQuestion
	content := `{"blocks":[{"type":"text","text":"The fix is done."},{"type":"tool_use","name":"AskUserQuestion","id":"2","input":{"questions":[{"question":"Which approach?","header":"Approach","options":[{"label":"Option A","description":"Fast"},{"label":"Option B","description":"Safe"}]}]}}]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-tts-3', 'claude', 0)",
		content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	conclusion := ttsExtractConclusion(msgID)
	assert.Contains(t, conclusion, "The fix is done.")
	assert.Contains(t, conclusion, "Which approach?")
	assert.Contains(t, conclusion, "Option A — Fast")
	assert.Contains(t, conclusion, "Option B — Safe")
}

func TestTTSExtractConclusion_OnlyAskUserQuestion(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	// Message with only AskUserQuestion (no text blocks)
	content := `{"blocks":[{"type":"tool_use","name":"AskUserQuestion","id":"3","input":{"questions":[{"question":"Proceed?","options":["Yes","No"]}]}}]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-tts-4', 'claude', 0)",
		content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	conclusion := ttsExtractConclusion(msgID)
	assert.Contains(t, conclusion, "Proceed?")
	assert.Contains(t, conclusion, "Yes, No")
}

func TestTTSExtractConclusion_MessageNotFound(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	conclusion := ttsExtractConclusion(999999)
	assert.Equal(t, "", conclusion)
}

func TestTTSExtractConclusion_EmptyBlocks(t *testing.T) {
	_, teardown := setupTTSTest(t, &mockSpeechProvider{}, &mockSummarizer{})
	defer teardown()

	content := `{"blocks":[]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-tts-5', 'claude', 0)",
		content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	conclusion := ttsExtractConclusion(msgID)
	assert.Equal(t, "", conclusion)
}

// --- TTSGenerate: messageId conclusion extraction replaces full text ---

func TestTTSGenerate_WithMessageID_UsesConclusionNotFullText(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "精简结论"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	// Insert a message with intro text + tool_use + conclusion text
	content := `{"blocks":[{"type":"text","text":"Let me check the file."},{"type":"tool_use","name":"Read","id":"10","input":{"file_path":"/foo.go"}},{"type":"text","text":"The answer is 42."}]}`
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, 'sess-tts-conc', 'claude', 0)",
		env.ProjectDir, content,
	)
	assert.NoError(t, err)
	msgID, _ := result.LastInsertId()

	// Send TTS request with full text (as frontend does) + messageId
	fullText := "Let me check the file.\nThe answer is 42."
	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{"text": fullText, "messageId": msgID, "language": "zh"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for job to complete
	// The cache key is computed from the conclusion text (after extraction), not the full text
	conclusionText := "The answer is 42."
	hash := sha256.Sum256([]byte(conclusionText))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]
	if job, ok := service.GetTTSJob(cacheKey); ok {
		select {
		case <-job.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("TTS job did not complete in time")
		}
	}

	// The summarizer should have received the conclusion text, not the full text
	assert.True(t, mockSum.called)
	assert.Equal(t, conclusionText, mockSum.lastText)
}
