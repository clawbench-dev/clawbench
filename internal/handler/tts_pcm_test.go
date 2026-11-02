package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	// 14 bytes = 7 mono 16-bit frames. The decoder seam must produce a
	// frame-aligned stream — see pcmFileValid.
	stubDecoder(t, []byte("fake pcm data!"), format)
	env, teardown := setupPCMTest(t, provider, sum)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "你好世界"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "audio/L16; rate=24000; channels=1; endianness=little", w.Header().Get("Content-Type"))
	assert.Equal(t, "16", w.Header().Get("X-Audio-Bits"))
	assert.Equal(t, "fake pcm data!", w.Body.String())

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
	assert.Equal(t, "audio/L16; rate=16000; channels=1; endianness=little", w.Header().Get("Content-Type"))
	assert.False(t, provider.synthesizeCalled, "cache hit must not synthesize")
	assert.False(t, sum.called, "cache hit must not summarize")
}

func TestTTSPCMGenerate_ReusesCachedMP3(t *testing.T) {
	provider := &mockSpeechProvider{}
	sum := &mockSummarizer{}
	format := audio.Format{SampleRate: 24000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("decoded!"), format)
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
	assert.Equal(t, "decoded!", w.Body.String())
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
	assertNoPCMTempFiles(t, filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts"))
}

// assertNoPCMTempFiles fails when a decode left its intermediate file behind.
func assertNoPCMTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		assert.NotContains(t, name, ".decoding", "a decode must not leave its temp file behind")
		assert.NotContains(t, name, ".tmp", "a decode must not leave its temp file behind")
	}
}

func TestTTSPCMGenerate_LanguageDefaultsToZh(t *testing.T) {
	provider := &mockSpeechProvider{}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("pcm!"), format)
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "默认语言"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "zh", provider.lastSynthLang)
}

// noopProvider / noopSummarizer are stateless doubles: the shared mocks in
// tts_test.go record their calls in plain fields, which the race detector
// flags once several requests run in parallel.
type noopProvider struct{}

func (noopProvider) Synthesize(_ context.Context, _, outputPath, _ string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte("mp3 bytes"), 0o644)
}

type noopSummarizer struct{}

func (noopSummarizer) Summarize(_ context.Context, text, _ string) (string, error) {
	return "summary of: " + text, nil
}

// TestTTSPCMGenerate_ConcurrentSameKey is the regression guard for the torn PCM
// cache. The decoded bytes were written to a temp path derived only from
// cacheKey and then renamed onto the final name, with no lock and no
// single-flight: two requests for the same phrase truncated each other's temp
// file (or interleaved into it) and the last rename published a corrupt stream
// under the final name — which findCachedPCM then served as a cache hit
// forever.
//
// The distinguishing assertion is the decode count: one decode for eight
// concurrent requests. A per-request decode (the old behavior) reports 8 even
// though the bytes happen to match.
func TestTTSPCMGenerate_ConcurrentSameKey(t *testing.T) {
	format := audio.Format{SampleRate: 24000, Channels: 1, Bits: 16}
	body := bytes.Repeat([]byte{0x11, 0x22}, 4096) // 8192 bytes, frame aligned

	var decodes atomic.Int32
	orig := decodeMP3ToPCM
	decodeMP3ToPCM = func(_, dstPath string) (audio.Format, error) {
		decodes.Add(1)
		// Long enough that an unprotected implementation really overlaps: the
		// second caller opens and truncates the file the first is writing.
		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(dstPath, body, 0o644); err != nil {
			return audio.Format{}, err
		}
		return format, nil
	}
	t.Cleanup(func() { decodeMP3ToPCM = orig })

	env, teardown := setupPCMTest(t, noopProvider{}, noopSummarizer{})
	defer teardown()

	const requests = 8
	reqs := make([]*http.Request, requests)
	for i := range reqs {
		reqs[i] = withProjectCookie(
			newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "并发同一句话"}),
			env.ProjectDir,
		)
	}

	var wg sync.WaitGroup
	recorders := make([]*httptest.ResponseRecorder, requests)
	start := make(chan struct{})
	for i := range reqs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			TTSPCMGenerate(w, reqs[i])
			recorders[i] = w
		}(i)
	}
	close(start)
	wg.Wait()

	for i, w := range recorders {
		require.NotNil(t, w, "request %d never completed", i)
		require.Equal(t, http.StatusOK, w.Code, "request %d failed: %s", i, w.Body.String())
		assert.Equal(t, body, w.Body.Bytes(), "request %d served a torn stream", i)
		assert.Equal(t, "audio/L16; rate=24000; channels=1; endianness=little", w.Header().Get("Content-Type"))
	}

	assert.Equal(t, int32(1), decodes.Load(),
		"concurrent requests for the same phrase must share one decode")

	ttsDir := filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts")
	assertNoPCMTempFiles(t, ttsDir)

	// Exactly one PCM cache entry, and it is a complete stream.
	matches, err := filepath.Glob(filepath.Join(ttsDir, mp3CacheKey("并发同一句话")+"-r*"+ttsPCMExt))
	require.NoError(t, err)
	require.Len(t, matches, 1, "one decoded stream must be published")
	onDisk, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	assert.Equal(t, body, onDisk)
}

// TestTTSPCMGenerate_TruncatedCacheIsNotAHit guards the validity test: a cache
// file left half-written by an earlier crash has a size that is not a whole
// number of PCM frames. Serving it replays noise and, worse, refreshes its
// mtime so eviction never reclaims it — it must be decoded again instead.
func TestTTSPCMGenerate_TruncatedCacheIsNotAHit(t *testing.T) {
	provider := &mockSpeechProvider{}
	sum := &mockSummarizer{}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("recovered!"), format) // 10 bytes = 5 frames
	env, teardown := setupPCMTest(t, provider, sum)
	defer teardown()

	cachePath := pcmCachePath(env.ProjectDir, "半截缓存", format)
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0o755))
	// 5 bytes: non-zero, so the old Size()>0 test accepted it, but an odd
	// number of bytes cannot be a mono 16-bit stream.
	require.NoError(t, os.WriteFile(cachePath, []byte("trunc"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "半截缓存"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "recovered!", w.Body.String(), "a torn cache entry must be re-decoded, not served")
	assert.True(t, provider.synthesizeCalled, "the cache entry is unusable, so the pipeline runs again")

	onDisk, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Equal(t, "recovered!", string(onDisk), "the torn file must be replaced by a complete one")
}

// TestTTSPCMGenerate_RejectsNonFrameAlignedDecode guards the write side: a
// decoder that returns a stream which is not a whole number of frames must not
// have its output published under the cache name.
func TestTTSPCMGenerate_RejectsNonFrameAlignedDecode(t *testing.T) {
	provider := &mockSpeechProvider{}
	format := audio.Format{SampleRate: 16000, Channels: 1, Bits: 16}
	stubDecoder(t, []byte("odd"), format) // 3 bytes, mono 16-bit frames
	env, teardown := setupPCMTest(t, provider, &mockSummarizer{})
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/tts/generate/pcm", map[string]any{"text": "半截解码"})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	TTSPCMGenerate(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assertNoPCMTempFiles(t, filepath.Join(env.ProjectDir, ".clawbench", "generated", "tts"))
	matches, err := filepath.Glob(pcmCachePath(env.ProjectDir, "半截解码", format))
	require.NoError(t, err)
	assert.Empty(t, matches, "a truncated stream must not be published as a cache entry")
}
