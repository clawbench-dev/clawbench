package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"clawbench/internal/audio"
	"clawbench/internal/speech"
	"clawbench/internal/summarize"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDecoder replaces the real MP3 decoder so handler tests need no audio
// fixture. It writes body to dstPath and reports format.
func stubDecoder(t *testing.T, body []byte, format audio.Format) {
	t.Helper()
	orig := decodeMP3ToPCM
	decodeMP3ToPCM = func(srcPath, dstPath string) (audio.Format, error) {
		if err := os.WriteFile(dstPath, body, 0o644); err != nil {
			return audio.Format{}, err
		}
		return format, nil
	}
	t.Cleanup(func() { decodeMP3ToPCM = orig })
}

// setupPCMTest installs a provider and summarizer and restores them afterwards.
func setupPCMTest(t *testing.T, provider speech.SpeechProvider, sum summarize.Summarizer) (*testEnv, func()) {
	t.Helper()
	env, envTeardown := setupTestEnv(t)

	origProvider := GetSpeechProvider()
	origSummarizer := GetSummarizer()
	SetSpeechProvider(provider)
	SetSummarizer(sum)

	return env, func() {
		SetSpeechProvider(origProvider)
		SetSummarizer(origSummarizer)
		envTeardown()
	}
}

// mp3CacheKey mirrors ttsCacheKeyAndPrefix for a provider without CacheIdentity.
func mp3CacheKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:16]
}

func pcmCachePath(projectDir, text string, format audio.Format) string {
	name := mp3CacheKey(text) + "-r" + strconv.Itoa(format.SampleRate) + "-c" + strconv.Itoa(format.Channels) + "-b" + strconv.Itoa(format.Bits) + ttsPCMExt
	return filepath.Join(projectDir, ".clawbench", "generated", "tts", name)
}

func TestTTSPCMGenerate_MethodNotAllowed(t *testing.T) {
	provider := &mockSpeechProvider{}
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/tts/generate/pcm", http.NoBody)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.False(t, provider.synthesizeCalled)
}

func TestTTSPCMGenerate_NoProjectCookie(t *testing.T) {
	provider := &mockSpeechProvider{}
	_, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "你好"})
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, provider.synthesizeCalled)
}

func TestTTSPCMGenerate_TextRequired(t *testing.T) {
	provider := &mockSpeechProvider{}
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": ""})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, provider.synthesizeCalled)
}

func TestTTSPCMGenerate_Success(t *testing.T) {
	provider := &mockSpeechProvider{}
	sum := &mockSummarizer{result: "朗读用摘要"}
	format := audio.Format{SampleRate: 24000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("fake pcm data"), format)
	env, teardown := setupPCMTest(t, provider, sum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "你好世界"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "audio/L16; rate=24000; channels=1", w.Header().Get("Content-Type"))
	assert.Equal(t, "16", w.Header().Get("X-Audio-Bits"))
	assert.Equal(t, "fake pcm data", w.Body.String())

	assert.True(t, provider.synthesizeCalled, "the MP3 must be synthesized first")
	assert.Equal(t, "朗读用摘要", provider.lastSynthText, "synthesis must speak the summary")
	assert.Equal(t, "zh", provider.lastSynthLang)
	assert.True(t, sum.called)

	// Both the MP3 and the decoded PCM must be cached.
	_, err := os.Stat(filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts", mp3CacheKey("你好世界")+ttsAudioExt))
	assert.NoError(t, err)
	_, err = os.Stat(pcmCachePath(env.ProjectDir, "你好世界", format))
	assert.NoError(t, err)
}

func TestTTSPCMGenerate_CacheHitSkipsSynthesis(t *testing.T) {
	provider := &mockSpeechProvider{}
	sum := &mockSummarizer{}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("should not run"), format)
	env, teardown := setupPCMTest(t, provider, sum)
	defer teardown()

	cached := []byte("cached pcm bytes")
	cachePath := pcmCachePath(env.ProjectDir, "缓存文本", format)
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0o755))
	require.NoError(t, os.WriteFile(cachePath, cached, 0o644))

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "缓存文本"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, string(cached), w.Body.String())
	assert.Equal(t, "audio/L16; rate=16000; channels=1", w.Header().Get("Content-Type"))
	assert.False(t, provider.synthesizeCalled, "cache hit must not synthesize")
	assert.False(t, sum.called, "cache hit must not summarize")
}

func TestTTSPCMGenerate_ReusesCachedMP3(t *testing.T) {
	provider := &mockSpeechProvider{}
	sum := &mockSummarizer{}
	format := audio.Format{SampleRate: 24000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("decoded"), format)
	env, teardown := setupPCMTest(t, provider, sum)
	defer teardown()

	// An MP3 already produced by the web UI path.
	mp3Path := filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts", mp3CacheKey("已有音频")+ttsAudioExt)
	require.NoError(t, os.MkdirAll(filepath.Dir(mp3Path), 0o755))
	require.NoError(t, os.WriteFile(mp3Path, []byte("existing mp3"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "已有音频"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "decoded", w.Body.String())
	assert.False(t, provider.synthesizeCalled, "an existing MP3 must not be re-synthesized")
	assert.False(t, sum.called)
}

func TestTTSPCMGenerate_SynthesizeFailure(t *testing.T) {
	provider := &mockSpeechProvider{synthesizeErr: assert.AnError}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("unused"), format)
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "会失败的文本"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.True(t, provider.synthesizeCalled)
}

func TestTTSPCMGenerate_DecodeFailure(t *testing.T) {
	provider := &mockSpeechProvider{}
	orig := decodeMP3ToPCM
	decodeMP3ToPCM = func(string, string) (audio.Format, error) { return audio.Format{}, assert.AnError }
	t.Cleanup(func() { decodeMP3ToPCM = orig })

	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "解码失败"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTTSPCMGenerate_LanguageDefaultsToZh(t *testing.T) {
	provider := &mockSpeechProvider{}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("pcm"), format)
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "默认语言"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "zh", provider.lastSynthLang)
}
