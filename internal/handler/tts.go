//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/summarize"
)

const (
	// ttsMaxBodyBytes limits the request body size for TTS endpoint (1MB).
	ttsMaxBodyBytes = 1 << 20

	// ttsSummarizeTimeout is the timeout for the summarization step.
	ttsSummarizeTimeout = 60 * time.Second

	// ttsSynthesizeTimeout is the timeout for the TTS synthesis step.
	ttsSynthesizeTimeout = 120 * time.Second
)

// speechProvider is the global speech provider instance.
// Access is protected by speechProviderMu for hot-reload safety.
var (
	speechProvider   speech.SpeechProvider = speech.NewEdgeTTSProvider()
	speechProviderMu sync.RWMutex
)

// SetSpeechProvider replaces the global speech provider.
// Goroutine-safe: concurrent Synthesize calls are protected by RWMutex.
func SetSpeechProvider(p speech.SpeechProvider) {
	speechProviderMu.Lock()
	speechProvider = p
	speechProviderMu.Unlock()
}

// GetSpeechProvider returns the current speech provider.
// Goroutine-safe for concurrent reads.
func GetSpeechProvider() speech.SpeechProvider {
	speechProviderMu.RLock()
	p := speechProvider
	speechProviderMu.RUnlock()
	return p
}

// summarizer is the global text summarizer instance.
// Access is protected by summarizerMu for hot-reload safety.
var (
	summarizer   summarize.Summarizer = summarize.NewSimple()
	summarizerMu sync.RWMutex
)

// SetSummarizer replaces the global text summarizer.
// Goroutine-safe for concurrent access.
func SetSummarizer(s summarize.Summarizer) {
	summarizerMu.Lock()
	summarizer = s
	summarizerMu.Unlock()
}

// GetSummarizer returns the current text summarizer.
func GetSummarizer() summarize.Summarizer {
	summarizerMu.RLock()
	s := summarizer
	summarizerMu.RUnlock()
	return s
}

// ttsGenerateRequest is the request body for POST /api/tts/generate.
type ttsGenerateRequest struct {
	Text      string `json:"text"`
	Language  string `json:"language"`  // language code, e.g. "zh", "en"; defaults to "zh" if empty
	MessageID int64  `json:"messageId"` // chat_history.id for TTS summary caching
}

// ttsSynthesizeFunc is the signature for TTS synthesis operations (streaming or non-streaming).
type ttsSynthesizeFunc func(ctx context.Context, summary, absAudioPath, language string) error

// ttsRunJob runs the summarize + synthesize pipeline in a background goroutine.
// It unifies the streaming and non-streaming code paths.
func ttsRunJob(
	ctx context.Context,
	job *service.TTSJob,
	cacheKey string,
	projectPath, relAudioPath, absAudioPath string,
	req ttsGenerateRequest,
	curSummarizer summarize.Summarizer,
	errSummarizeFailed, errSynthesizeFailed string,
	isStreaming bool,
	synthesizeFunc ttsSynthesizeFunc,
) {
	go func() {
		defer service.UnregisterTTSJob(cacheKey)
		defer service.CloseTTSJobDone(cacheKey)
		defer job.Cancel()

		// Phase 1: Summarize
		summary, ok := ttsSummarize(ctx, curSummarizer, cacheKey, req.Text, req.Language, req.MessageID, errSummarizeFailed, isStreaming)
		if !ok {
			return
		}

		// Phase 2: Synthesize
		service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "synthesizing", Streaming: isStreaming})

		synthesizeCtx, synthesizeCancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
		err := synthesizeFunc(synthesizeCtx, summary, absAudioPath, req.Language)
		synthesizeCancel()

		// Close AudioCh BEFORE sending result event (streaming only).
		if job.AudioCh != nil {
			close(job.AudioCh)
		}

		if err != nil {
			logMsg := "tts synthesize failed"
			if isStreaming {
				logMsg = "tts stream synthesize failed"
			}
			slog.Error(logMsg, slog.String("error", err.Error()), slog.String("cache_key", cacheKey))
			service.SendTTSEvent(cacheKey, service.TTSEvent{
				Type: "result", SynthesizeFailed: true, SynthesizeError: errSynthesizeFailed, Summary: summary, Streaming: isStreaming,
			})
			return
		}

		logMsg := "tts generate completed"
		if isStreaming {
			logMsg = "tts stream generate completed"
		}
		slog.Info(logMsg, slog.String("cache_key", cacheKey), slog.String("path", relAudioPath))
		service.EvictTTSCache(projectPath, model.TTSMaxCacheFiles)
		service.SendTTSEvent(cacheKey, service.TTSEvent{
			Type: "result", AudioPath: relAudioPath, Summary: summary, Streaming: isStreaming,
		})
	}()
}

// audioExtForProvider returns the audio file extension for the given provider.
// Piper, Kokoro, and MOSS-Nano produce WAV; all others produce MP3.
func audioExtForProvider(p speech.SpeechProvider) string {
	switch p.(type) {
	case *speech.PiperProvider, *speech.KokoroProvider, *speech.MossNanoProvider:
		return ".wav"
	default:
		return ".mp3"
	}
}

// TTSGenerate handles POST /api/tts/generate.
// It validates input, checks cache, and either returns cached audio immediately
// or starts an async TTS job and returns a jobId for SSE streaming.
func TTSGenerate(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, int64(ttsMaxBodyBytes))

	var req ttsGenerateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Text == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TextRequired")
		return
	}

	// Default language to "zh" if not provided
	if req.Language == "" {
		req.Language = "zh"
	}

	// When messageId is provided, extract the final conclusion from the message's
	// content blocks instead of using the raw full text from the frontend.
	// This uses ExtractLastAnswerFromBlocks (same logic as chat summary) to
	// capture only the AI's final answer, skipping intermediate step commentary.
	if req.MessageID > 0 {
		if conclusion := ttsExtractConclusion(req.MessageID); conclusion != "" {
			slog.Info(
				"tts using conclusion from message blocks",
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

	// Compute cache key from text content
	hash := sha256.Sum256([]byte(req.Text))
	cacheKey := hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen]

	// Snapshot current providers for consistency within this request
	curProvider := GetSpeechProvider()
	curSummarizer := GetSummarizer()

	// Determine audio file extension based on TTS engine
	audioExt := audioExtForProvider(curProvider)
	// Project-relative path (not server DataDir)
	relAudioPath := filepath.Join(".clawbench", "generated", "tts", cacheKey+audioExt)

	// Validate the output path (defense-in-depth)
	absAudioPath, ok := validateAndResolvePath(w, r, projectPath, relAudioPath)
	if !ok {
		return
	}

	// Check cache: if audio file already exists, return immediately as JSON
	if info, err := os.Stat(absAudioPath); err == nil && info.Size() > 0 {
		slog.Info(
			"tts cache hit",
			slog.String("cache_key", cacheKey),
			slog.String("path", relAudioPath),
		)
		// Try DB for cached summary
		var summary string
		if req.MessageID > 0 {
			summary, _ = service.GetTTSSummaryByMessageID(req.MessageID)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"cached":    true,
			"audioPath": relAudioPath,
			"summary":   summary,
		})
		return
	}

	// Cache miss — start async TTS job
	// Detect streaming provider for Edge TTS
	streamingProvider, isStreaming := curProvider.(speech.StreamingSpeechProvider)

	// Pre-compute translations before starting goroutines — T(r, ...) reads from
	// r.Context() which may be canceled after the handler returns.
	errSummarizeFailed := T(r, "SummarizeFailed")
	errSynthesizeFailed := T(r, "SynthesizeFailed")

	if isStreaming {
		ctx, cancel := context.WithCancel(context.Background())
		job := service.RegisterStreamingTTSJob(cacheKey, cancel)
		ttsRunJob(ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, true,
			func(sCtx context.Context, summary, outPath, lang string) error {
				return streamingProvider.SynthesizeStream(sCtx, summary, outPath, lang, job.AudioCh)
			},
		)
		writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": true})
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		job := service.RegisterTTSJob(cacheKey, cancel)
		ttsRunJob(ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, false,
			func(sCtx context.Context, summary, outPath, lang string) error {
				return curProvider.Synthesize(sCtx, summary, outPath, lang)
			},
		)
		writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": false})
	}
}

// ttsSummarize performs the summarize phase for TTS generation.
// Returns (summary, true) on success, or ("", false) if summarization failed
// and a result event has already been sent.
func ttsSummarize(ctx context.Context, curSummarizer summarize.Summarizer, cacheKey, text, language string, messageID int64, errSummarizeFailed string, streaming bool) (string, bool) {
	var summary string
	cachedSummary, found := service.GetTTSSummaryByMessageID(messageID)
	if found && cachedSummary != "" && messageID > 0 {
		slog.Info(
			"tts summary cache hit, skipping summarization",
			slog.String("cache_key", cacheKey),
		)
		return cachedSummary, true
	}

	service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "summarizing", Streaming: streaming})

	summarizeCtx, summarizeCancel := context.WithTimeout(ctx, ttsSummarizeTimeout)
	var err error
	summary, err = curSummarizer.Summarize(summarizeCtx, text, language)
	summarizeCancel()
	if err != nil {
		slog.Warn(
			"tts summarize failed, falling back to simple summarizer",
			slog.String("error", err.Error()),
		)
		fallbackSummary, fallbackErr := summarize.NewSimple().Summarize(context.Background(), text, language)
		if fallbackErr != nil || fallbackSummary == "" {
			service.SendTTSEvent(cacheKey, service.TTSEvent{
				Type:             "result",
				SynthesizeFailed: true,
				SynthesizeError:  errSummarizeFailed,
				Streaming:        streaming,
			})
			return "", false
		}
		summary = fallbackSummary
	}

	slog.Info(
		"tts summarize completed",
		slog.String("cache_key", cacheKey),
		slog.Int("original_len", len([]rune(text))),
		slog.Int("summary_len", len([]rune(summary))),
	)

	if messageID > 0 {
		if err := service.SaveTTSSummaryByMessageID(messageID, summary); err != nil {
			slog.Warn(
				"tts failed to cache summary to DB",
				slog.String("error", err.Error()),
			)
		}
	}

	return summary, true
}

// ttsExtractConclusion loads a message by ID, parses its content blocks,
// and extracts the final conclusion text suitable for TTS.
// It uses ExtractLastAnswerFromBlocks to get the AI's final answer (skipping
// intermediate step commentary), then appends AskUserQuestion text so TTS
// can read the question and options to the user.
// Returns empty string if the message cannot be loaded or has no speakable text.
func ttsExtractConclusion(messageID int64) string { //nolint:gocyclo,gocognit // multi-branch AskUserQuestion extraction
	msg, err := service.GetMessageByID(messageID)
	if err != nil || msg == nil {
		return ""
	}

	var content struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &content); err != nil || len(content.Blocks) == 0 {
		return ""
	}

	blocks := content.Blocks

	// Extract the final answer using the same logic as chat summary
	conclusion := summarize.ExtractLastAnswerFromBlocks(blocks)

	// Append AskUserQuestion text (questions + options) so TTS reads them
	var aqParts []string
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name != "AskUserQuestion" {
			continue
		}
		questions, ok := b.Input["questions"]
		if !ok {
			continue
		}
		qList, ok := questions.([]any)
		if !ok {
			continue
		}
		for _, q := range qList {
			qMap, ok := q.(map[string]any)
			if !ok {
				continue
			}
			s, _ := qMap["question"].(string)
			if header, ok := qMap["header"].(string); ok && header != "" {
				s += " (" + header + ")"
			}
			opts, _ := qMap["options"].([]any)
			if len(opts) > 0 {
				var optStrs []string
				for _, o := range opts {
					switch v := o.(type) {
					case string:
						optStrs = append(optStrs, v)
					case map[string]any:
						label, _ := v["label"].(string)
						desc, _ := v["description"].(string)
						if desc != "" && desc != label {
							optStrs = append(optStrs, label+" — "+desc)
						} else {
							optStrs = append(optStrs, label)
						}
					}
				}
				s += ": " + strings.Join(optStrs, ", ")
			}
			if s != "" {
				aqParts = append(aqParts, s)
			}
		}
	}

	if len(aqParts) > 0 {
		aqText := strings.Join(aqParts, "\n")
		if conclusion != "" {
			return conclusion + "\n" + aqText
		}
		return aqText
	}

	return conclusion
}

// TTSStream handles GET /api/tts/stream/{jobId}.
// It streams SSE events for a TTS job using typed event format,
// compatible with browser EventSource API.
func TTSStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	_, ok := requireProject(w, r)
	if !ok {
		return
	}

	// Extract jobId from URL path: /api/tts/stream/{jobId}
	jobID := strings.TrimPrefix(r.URL.Path, "/api/tts/stream/")
	if jobID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "JobIdRequired")
		return
	}

	job, ok := service.GetTTSJob(jobID)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "JobNotFound")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	// Send cached events that may have already been produced before we connected.
	// Read from channel until it's empty, then enter the live streaming loop.
	for {
		select {
		case event, ok := <-job.StreamCh:
			if !ok {
				// Channel closed — job finished
				return
			}
			writeTTSSSE(w, event, canFlush, flusher)
			// If this is a result event, we're done
			if event.Type == "result" {
				return
			}
		default:
			// No cached events — enter live streaming
			goto liveStream
		}
	}

liveStream:
	for {
		select {
		case event, ok := <-job.StreamCh:
			if !ok {
				// Channel closed — job finished
				return
			}
			writeTTSSSE(w, event, canFlush, flusher)
			if event.Type == "result" {
				return
			}
		case <-r.Context().Done():
			slog.Info(
				"tts sse client disconnected, cancelling job",
				slog.String("job_id", jobID),
			)
			service.CancelTTSJob(jobID)
			return
		}
	}
}

// writeTTSSSE writes a single TTS event as a typed SSE message and flushes.
func writeTTSSSE(w http.ResponseWriter, event service.TTSEvent, canFlush bool, flusher http.Flusher) {
	data, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	if canFlush {
		flusher.Flush()
	}
}
