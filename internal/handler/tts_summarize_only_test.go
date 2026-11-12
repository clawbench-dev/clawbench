package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTTSGenerate_SummarizeOnly_ReturnsTextWithoutSynthesis pins the contract
// the Android client relies on: it reads the text with the phone's own speech
// engine, so it needs the condensed text but must not pay for (or wait on) a
// cloud synthesis whose audio it would throw away.
func TestTTSGenerate_SummarizeOnly_ReturnsTextWithoutSynthesis(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "这是核心结论"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{
		"text":          "这是一段较长的AI回复内容，需要被总结为语音。",
		"summarizeOnly": true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "这是核心结论", resp["summary"])
	assert.Equal(t, "这是核心结论", resp["speakText"])
	assert.NotContains(t, resp, "jobId", "summarize-only must not start a synthesis job")
	assert.NotContains(t, resp, "audioPath")

	assert.True(t, mockSum.called)
	assert.False(t, mockProvider.synthesizeCalled, "no audio should be synthesized")
}

// TestTTSGenerate_SummarizeOnly_AppliesSpeakerPrefix covers group chat: the
// spoken text carries "<name>说：" while the summary (shown in the UI) stays
// clean, exactly like the audio path.
func TestTTSGenerate_SummarizeOnly_AppliesSpeakerPrefix(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{result: "同意这个方案"}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{
		"text":          "我同意这个方案，可以开始实施。",
		"summarizeOnly": true,
		"speakerName":   "小明",
		"language":      "zh",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "同意这个方案", resp["summary"], "summary must not contain the prefix")
	assert.Equal(t, "小明说：同意这个方案", resp["speakText"])
}

func TestTTSGenerate_SummarizeOnly_RequiresText(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate", map[string]any{
		"text":          "",
		"summarizeOnly": true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSGenerate(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
