package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"clawbench/internal/audio"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/summarize"
)

// ttsPCMExt is the cache file extension for raw PCM output. It is deliberately
// distinct from ttsAudioExt so the MP3 and PCM caches never collide.
const ttsPCMExt = ".pcm"

// decodeMP3ToPCM is a seam so handler tests can avoid shipping an MP3 fixture.
var decodeMP3ToPCM = audio.DecodeMP3ToPCMFile

// TTSPCMGenerate handles POST /api/tts/generate/pcm.
//
// It reuses the MP3 pipeline — same summary, same cache key, same file — and
// converts the result to headerless PCM for clients that cannot decode MP3
// (the FoloToy AI Passport writes the stream straight into an I2S codec).
// The response is synchronous: firmware wants bytes, not a job id.
func TTSPCMGenerate(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(ttsMaxBodyBytes))

	var req ttsGenerateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Text == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TextRequired")
		return
	}

	if req.Language == "" {
		req.Language = "zh"
	}

	if req.MessageID > 0 {
		if conclusion := ttsExtractConclusion(req.MessageID); conclusion != "" {
			slog.Info(
				"tts pcm using conclusion from message blocks",
				slog.Int64("message_id", req.MessageID),
				slog.Int("original_len", len([]rune(req.Text))),
				slog.Int("conclusion_len", len([]rune(conclusion))),
			)
			req.Text = conclusion
		}
	}

	if summarize.MaxTextRunes > 0 && len([]rune(req.Text)) > summarize.MaxTextRunes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TextTooLong", map[string]any{"MaxChars": summarize.MaxTextRunes})
		return
	}

	curProvider := GetSpeechProvider()
	curSummarizer := GetSummarizer()

	// Same key as the MP3 endpoint, so a phrase already spoken in the web UI is
	// decoded rather than synthesized twice.
	cacheKey, speakerPrefix := ttsCacheKeyAndPrefix(req.Text, req.SpeakerName, req.Language, ttsProviderCacheIdentity(curProvider))

	mp3RelPath := filepath.Join(".clawbench", "generated", "tts", cacheKey+ttsAudioExt)
	mp3AbsPath, ok := validateAndResolvePath(w, r, projectPath, mp3RelPath)
	if !ok {
		return
	}
	ttsDir := filepath.Dir(mp3AbsPath)

	if pcmPath, format, hit := findCachedPCM(ttsDir, cacheKey); hit {
		slog.Info("tts pcm cache hit", slog.String("cache_key", cacheKey), slog.String("path", pcmPath))
		servePCM(w, r, pcmPath, format)
		return
	}

	if err := ensureMP3(w, r, curProvider, curSummarizer, cacheKey, mp3AbsPath, mp3RelPath, req, speakerPrefix); err != nil {
		return
	}

	pcmPath, format, err := cachedPCMPath(ttsDir, cacheKey, mp3AbsPath)
	if err != nil {
		slog.Error("tts pcm decode failed", slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)
	servePCM(w, r, pcmPath, format)
}

// pcmDecodeCall is one in-flight decode that concurrent callers wait on.
type pcmDecodeCall struct {
	done   chan struct{}
	result pcmDecodeResult
}

type pcmDecodeResult struct {
	path   string
	format audio.Format
	err    error
}

// pcmDecodeGroup collapses concurrent decodes of the SAME cached MP3 into one.
//
// The decoded bytes are written to a temp path derived from cacheKey and then
// renamed onto the final, parameter-carrying name. Without single-flight two
// requests for the same phrase race on that temp path: the second open
// truncates the file the first is still writing (or the two interleave), and
// whichever rename lands last publishes truncated PCM under the final name —
// which findCachedPCM then treats as a valid cache entry forever.
var pcmDecodeGroup struct {
	mu    sync.Mutex
	calls map[string]*pcmDecodeCall
}

// cachedPCMPath returns the decoded PCM for mp3AbsPath, decoding at most once
// across concurrent callers. The first caller does the work; the rest wait for
// its result and serve the same file.
func cachedPCMPath(ttsDir, cacheKey, mp3AbsPath string) (string, audio.Format, error) {
	pcmDecodeGroup.mu.Lock()
	if pcmDecodeGroup.calls == nil {
		pcmDecodeGroup.calls = make(map[string]*pcmDecodeCall, 4)
	}
	if call, ok := pcmDecodeGroup.calls[mp3AbsPath]; ok {
		pcmDecodeGroup.mu.Unlock()
		<-call.done
		return call.result.path, call.result.format, call.result.err
	}
	call := &pcmDecodeCall{done: make(chan struct{})}
	pcmDecodeGroup.calls[mp3AbsPath] = call
	pcmDecodeGroup.mu.Unlock()

	// Deferred so a panicking decoder cannot leave the entry in the map with
	// nobody to close it: every waiter — and every later request for the same
	// phrase — would block forever. The result is published before the waiters
	// are woken, and the entry is dropped first so a later request starts a
	// fresh decode instead of joining a finished one.
	func() {
		defer func() {
			pcmDecodeGroup.mu.Lock()
			delete(pcmDecodeGroup.calls, mp3AbsPath)
			pcmDecodeGroup.mu.Unlock()
			close(call.done)
		}()
		call.result = decodePCM(ttsDir, cacheKey, mp3AbsPath)
	}()

	return call.result.path, call.result.format, call.result.err
}

// decodePCM decodes the cached MP3 into its final PCM path. Every failure path
// removes the temp file, so a failed decode never leaves a half-written file
// behind for a later run to serve.
func decodePCM(ttsDir, cacheKey, mp3AbsPath string) pcmDecodeResult {
	// A caller that lost the race for the MP3 may find the decode already done
	// by the winner (synthesis is the slow half of the request).
	if path, format, hit := findCachedPCM(ttsDir, cacheKey); hit {
		return pcmDecodeResult{path: path, format: format}
	}

	tmpPath := filepath.Join(ttsDir, cacheKey+".decoding")
	format, err := decodeMP3ToPCM(mp3AbsPath, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return pcmDecodeResult{err: err}
	}
	if format.SampleRate <= 0 || format.Channels <= 0 || format.Bits <= 0 {
		_ = os.Remove(tmpPath)
		return pcmDecodeResult{err: fmt.Errorf("tts pcm: decoder reported an invalid format %+v", format)}
	}
	if !pcmFileValid(tmpPath, format) {
		_ = os.Remove(tmpPath)
		return pcmDecodeResult{err: errors.New("tts pcm: decoder produced a truncated stream")}
	}

	pcmPath := pcmPathFor(ttsDir, cacheKey, format)
	if err := os.Rename(tmpPath, pcmPath); err != nil {
		_ = os.Remove(tmpPath)
		return pcmDecodeResult{err: err}
	}
	return pcmDecodeResult{path: pcmPath, format: format}
}

// ensureMP3 synthesizes the MP3 into mp3AbsPath unless it is already cached.
// On failure it writes a localized error response and returns the error.
func ensureMP3(
	w http.ResponseWriter,
	r *http.Request,
	curProvider speech.SpeechProvider,
	curSummarizer summarize.Summarizer,
	cacheKey, mp3AbsPath, mp3RelPath string,
	req ttsGenerateRequest,
	speakerPrefix string,
) error {
	if info, err := os.Stat(mp3AbsPath); err == nil && info.Size() > 0 {
		return nil
	}

	summary, ok := ttsSummarize(
		context.Background(), curSummarizer, cacheKey, req.Text, req.Language, req.MessageID,
		T(r, "SummarizeFailed"), false,
	)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusBadGateway, "SummarizeFailed")
		return fmt.Errorf("tts pcm: summarize failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), ttsSynthesizeTimeout)
	err := curProvider.Synthesize(ctx, ttsSynthText(speakerPrefix, summary), mp3AbsPath, req.Language)
	cancel()
	if err != nil {
		slog.Error("tts pcm synthesize failed", slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
		writeLocalizedErrorf(w, r, http.StatusBadGateway, "SynthesizeFailed")
		return err
	}

	slog.Info("tts pcm synthesized mp3", slog.String("cache_key", cacheKey), slog.String("path", mp3RelPath))
	return nil
}

// pcmPathFor builds a self-describing PCM cache name: the stream parameters are
// part of the filename so a cache hit needs no sidecar file.
func pcmPathFor(dir, cacheKey string, format audio.Format) string {
	name := fmt.Sprintf("%s-r%d-c%d-b%d%s", cacheKey, format.SampleRate, format.Channels, format.Bits, ttsPCMExt)
	return filepath.Join(dir, name)
}

// findCachedPCM looks for a previously decoded stream for cacheKey.
//
// A non-zero size is not proof of a usable file: a decode that was cut short
// (crash, interleaved write, full disk) leaves a file whose size is not a whole
// number of PCM frames. Serving it would play truncated noise and, worse,
// refresh its mtime so eviction never picks it. Such a file is skipped here, so
// the caller decodes again and renames a complete file over it.
func findCachedPCM(dir, cacheKey string) (string, audio.Format, bool) {
	matches, err := filepath.Glob(filepath.Join(dir, cacheKey+"-r*"+ttsPCMExt))
	if err != nil {
		return "", audio.Format{}, false
	}
	for _, path := range matches {
		name := filepath.Base(path)
		rest := strings.TrimPrefix(name, cacheKey+"-")
		var rate, channels, bits int
		if _, err := fmt.Sscanf(rest, "r%d-c%d-b%d"+ttsPCMExt, &rate, &channels, &bits); err != nil {
			continue
		}
		if rate <= 0 || channels <= 0 || bits <= 0 {
			continue
		}
		format := audio.Format{SampleRate: rate, Channels: channels, Bits: bits}
		if !pcmFileValid(path, format) {
			continue
		}
		return path, format, true
	}
	return "", audio.Format{}, false
}

// pcmFileValid reports whether path holds at least one whole PCM frame.
func pcmFileValid(path string, format audio.Format) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 {
		return false
	}
	frameBytes := int64(format.Channels) * int64(format.Bits) / 8
	if frameBytes <= 0 {
		return true
	}
	return info.Size()%frameBytes == 0
}

// servePCM writes the raw PCM file with a Content-Type carrying the stream
// parameters, so clients do not have to hardcode them.
//
// endianness=little is explicit because RFC 3551 defines L16 as BIG endian
// while the payload this endpoint produces is little endian (see
// audio.DecodeMP3ToPCMFile). A client that trusts the bare media type decodes
// the stream as full-scale noise.
func servePCM(w http.ResponseWriter, r *http.Request, absPath string, format audio.Format) {
	f, err := os.Open(absPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("audio/L16; rate=%d; channels=%d; endianness=little", format.SampleRate, format.Channels))
	w.Header().Set("X-Audio-Bits", strconv.Itoa(format.Bits))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
