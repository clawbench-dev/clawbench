package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

	tmpPath := filepath.Join(ttsDir, cacheKey+".decoding")
	format, err := decodeMP3ToPCM(mp3AbsPath, tmpPath)
	if err != nil {
		slog.Error("tts pcm decode failed", slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	pcmPath := pcmPathFor(ttsDir, cacheKey, format)
	if err := os.Rename(tmpPath, pcmPath); err != nil {
		_ = os.Remove(tmpPath)
		slog.Error("tts pcm cache write failed", slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)
	servePCM(w, r, pcmPath, format)
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
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			continue
		}
		return path, audio.Format{SampleRate: rate, Channels: channels, Bits: bits}, true
	}
	return "", audio.Format{}, false
}

// servePCM writes the raw PCM file with a Content-Type carrying the stream
// parameters, so clients do not have to hardcode them.
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

	w.Header().Set("Content-Type", fmt.Sprintf("audio/L16; rate=%d; channels=%d", format.SampleRate, format.Channels))
	w.Header().Set("X-Audio-Bits", strconv.Itoa(format.Bits))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
