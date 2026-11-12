package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTSPreview_SynthesizesTheSampleWithoutSummarizing(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/preview", map[string]any{
		"text": "您好，这是一段Edge TTS朗读测试。",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPreview(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	path, _ := resp["audioPath"].(string)
	assert.Contains(t, path, "tts-preview")
	assert.True(t, mockProvider.synthesizeCalled)
	assert.Equal(t, "您好，这是一段Edge TTS朗读测试。", mockProvider.lastSynthText)
	// The point is to hear the voice, not a condensation of the sample.
	assert.False(t, mockSum.called, "preview must not summarize")
}

func TestTTSPreview_UsesADefaultSampleWhenTextIsEmpty(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/preview", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPreview(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, mockProvider.lastSynthText)
}

func TestTTSPreview_RejectsOverlongText(t *testing.T) {
	mockProvider := &mockSpeechProvider{}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/preview", map[string]any{
		"text": strings.Repeat("长", ttsPreviewTextMaxRunes+1),
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPreview(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, mockProvider.synthesizeCalled)
}

func TestTTSPreview_ReportsSynthesisFailure(t *testing.T) {
	mockProvider := &mockSpeechProvider{synthesizeErr: assert.AnError}
	mockSum := &mockSummarizer{}
	env, teardown := setupTTSTest(t, mockProvider, mockSum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/preview", map[string]any{"text": "测试"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPreview(w, req)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}
