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

	"clawbench/internal/i18n"
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

	// ttsSynthesizeTimeout is a hard ceiling on the whole synthesis step.
	//
	// It is NOT the per-request budget: streaming synthesis sends one request
	// per segment (see speech.EdgeTTSProvider) and each of those is bounded on
	// its own, so a hang fails fast there. This only has to be generous enough
	// that reading a long reply in full — minutes of audio, one segment after
	// another — is not cut off halfway.
	ttsSynthesizeTimeout = 10 * time.Minute
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
	// SpeakerName is the group-chat speaker's display name. When set, the spoken
	// audio is prefixed with "<name>说：" / "<name> says: " so a listener can tell
	// concurrent speakers apart. The prefix is applied ONLY at synthesis time —
	// it is deliberately kept out of the summary and the cached summary row, so
	// it never participates in summarization ("不参与摘要").
	SpeakerName string `json:"speakerName"`
	// SummarizeOnly returns the spoken text without synthesizing audio.
	//
	// The Android client can read with the phone's own speech engine, which
	// still needs the same condensed text (a full reply takes minutes to read
	// aloud). Without this flag that client would have to pay for a cloud
	// synthesis whose audio it never plays.
	SummarizeOnly bool `json:"summarizeOnly"`
}

// ttsSynthesizeFunc is the signature for TTS synthesis operations (streaming or non-streaming).
type ttsSynthesizeFunc func(ctx context.Context, summary, absAudioPath, language string) error

// ttsSynthText prepends the group-chat speaker prefix to the summary that will
// be SPOKEN. It is applied here — after summarization and just before synthesis
// — so the prefix is heard but never summarized and never stored: the summary
// returned to the client and the cached tts_summaries row both stay clean
// ("xxx说 不参与摘要"). An empty speakerPrefix (single chats) is a no-op.
func ttsSynthText(speakerPrefix, summary string) string {
	if speakerPrefix == "" {
		return summary
	}
	return speakerPrefix + summary
}

// ttsCacheKeyAndPrefix computes the audio cache key and the spoken speaker
// prefix for a TTS request.
//
// The speaker name is folded into the key so two members who happen to utter
// the SAME words (a common group-chat occurrence: "同意", "好的") do not share
// an audio file — the spoken prefix differs, so the audio must differ too. An
// empty speaker name (single chats) keeps the key identical to the pre-existing
// scheme (sha256 of the text alone).
//
// providerIdentity is the synthesis configuration (engine / voice / model /
// rate) obtained from the provider's CacheIdentity method. It is folded in for
// the same reason: without it, changing the voice would keep serving the audio
// cached under the old voice, which is indistinguishable from a broken setting.
//
// The prefix is built in the SPOKEN language (req.Language), not the request's
// UI locale: the audio is synthesized in that language, so a Chinese sentence
// must not be prefixed with " says: ". It is deliberately kept out of the
// summarizer — it is applied only at synthesis time (see ttsRunJob).
func ttsCacheKeyAndPrefix(text, speakerName, language, providerIdentity string) (cacheKey, speakerPrefix string) {
	hashInput := text
	if speakerName != "" {
		hashInput = speakerName + "\x00" + text
		speakerPrefix = i18n.T(i18n.LocalizerForLocale(language), "TTSSpeakerSaysPrefix", map[string]any{"Name": speakerName})
	}
	if providerIdentity != "" {
		hashInput += "\x00" + providerIdentity
	}
	hash := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(hash[:])[:summarize.CacheKeyHexLen], speakerPrefix
}

// ttsProviderCacheIdentity returns the provider's cache identity, or "" for
// providers that do not implement CacheIdentityProvider (no configuration
// beyond the text affects their output).
func ttsProviderCacheIdentity(p speech.SpeechProvider) string {
	if cip, ok := p.(speech.CacheIdentityProvider); ok {
		return cip.CacheIdentity()
	}
	return ""
}

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
	speakerPrefix string,
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
			// The summarize failure already emitted a terminal result event.
			// Close AudioCh so the WS handler's drain loop can exit; otherwise it
			// blocks forever on an unclosed channel (leaking the connection).
			if job.AudioCh != nil {
				close(job.AudioCh)
			}
			return
		}

		// Phase 2: Synthesize
		service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "synthesizing", Streaming: isStreaming})

		synthesizeCtx, synthesizeCancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
		err := synthesizeFunc(synthesizeCtx, ttsSynthText(speakerPrefix, summary), absAudioPath, req.Language)
		synthesizeCancel()

		// Close AudioCh BEFORE sending result event (streaming only) so the WS
		// handler drains all audio chunks before the terminal done/error frame.
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

// ttsAudioExt is the audio file extension of the synthesized output. Every
// engine is asked for MP3 (Edge, OpenAI-compatible, and the three vendor
// engines — see speech.audioSink, which rejects anything else); the extension
// feeds the playback URL, so a mismatch would yield a silent failure.
const ttsAudioExt = ".mp3"

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
	ttsApplyConclusion(&req)

	if summarize.MaxTextRunes > 0 && len([]rune(req.Text)) > summarize.MaxTextRunes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TextTooLong", map[string]any{"MaxChars": summarize.MaxTextRunes})
		return
	}

	// Snapshot current providers for consistency within this request. Taken
	// BEFORE the cache key so the key reflects the provider that will actually
	// synthesize (and so a concurrent hot-reload cannot split the two).
	curProvider := GetSpeechProvider()
	curSummarizer := GetSummarizer()

	// Compute the cache key and the spoken speaker prefix together (see
	// ttsCacheKeyAndPrefix for why the speaker name and the provider identity
	// are folded into the key).
	cacheKey, speakerPrefix := ttsCacheKeyAndPrefix(req.Text, req.SpeakerName, req.Language, ttsProviderCacheIdentity(curProvider))

	// Summarize-only: the client reads the text itself (Android system TTS).
	// Returns the same condensed text the audio path would have spoken, with
	// the speaker prefix applied, but synthesizes nothing and touches no cache.
	if req.SummarizeOnly {
		ttsSummarizeOnlyResponse(w, r, req, curSummarizer, speakerPrefix)
		return
	}

	// Project-relative path (not server DataDir)
	relAudioPath := filepath.Join(".clawbench", "generated", "tts", cacheKey+ttsAudioExt)

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
		ttsRunJob(
			ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, speakerPrefix, true,
			func(sCtx context.Context, summary, outPath, lang string) error {
				return streamingProvider.SynthesizeStream(sCtx, summary, outPath, lang, job.AudioCh)
			},
		)
		writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": true})
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		job := service.RegisterTTSJob(cacheKey, cancel)
		ttsRunJob(
			ctx, job, cacheKey, projectPath, relAudioPath, absAudioPath,
			req, curSummarizer, errSummarizeFailed, errSynthesizeFailed, speakerPrefix, false,
			func(sCtx context.Context, summary, outPath, lang string) error {
				return curProvider.Synthesize(sCtx, summary, outPath, lang)
			},
		)
		writeJSON(w, http.StatusOK, map[string]any{"jobId": cacheKey, "streaming": false})
	}
}

// ttsApplyConclusion replaces the request text with the message's final answer
// when a messageId is present, so intermediate step commentary is not read.
func ttsApplyConclusion(req *ttsGenerateRequest) {
	if req.MessageID <= 0 {
		return
	}
	conclusion := ttsExtractConclusion(req.MessageID)
	if conclusion == "" {
		return
	}
	slog.Info(
		"tts using conclusion from message blocks",
		slog.Int64("message_id", req.MessageID),
		slog.Int("original_len", len([]rune(req.Text))),
		slog.Int("conclusion_len", len([]rune(conclusion))),
	)
	req.Text = conclusion
}

// ttsSummarizeOnlyResponse writes the condensed text without synthesizing it.
// Split out of TTSGenerate to keep that handler's branch count down; the
// response shape (summary + speakText) is the contract the Android client
// reads, and speakText is what must actually be spoken (prefix included).
func ttsSummarizeOnlyResponse(w http.ResponseWriter, r *http.Request, req ttsGenerateRequest, curSummarizer summarize.Summarizer, speakerPrefix string) {
	summary, err := ttsComputeSummary(r.Context(), curSummarizer, req.Text, req.Language, req.MessageID, nil)
	if err != nil {
		slog.Error("tts summarize-only failed", slog.String("error", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "SummarizeFailed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary":   summary,
		"speakText": ttsSynthText(speakerPrefix, summary),
	})
}

// ttsSummarize performs the summarize phase for TTS generation.
// Returns (summary, true) on success, or ("", false) if summarization failed
// and a result event has already been sent.
func ttsSummarize(ctx context.Context, curSummarizer summarize.Summarizer, cacheKey, text, language string, messageID int64, errSummarizeFailed string, streaming bool) (string, bool) {
	summary, err := ttsComputeSummary(ctx, curSummarizer, text, language, messageID, func() {
		service.SendTTSEvent(cacheKey, service.TTSEvent{Type: "phase", Phase: "summarizing", Streaming: streaming})
	})
	if err != nil {
		service.SendTTSEvent(cacheKey, service.TTSEvent{
			Type:             "result",
			SynthesizeFailed: true,
			SynthesizeError:  errSummarizeFailed,
			Streaming:        streaming,
		})
		return "", false
	}
	return summary, true
}

// ttsComputeSummary produces the text that will be spoken.
//
// onStart runs only when a model call is actually about to happen — a cached
// summary returns immediately and must not re-emit the "summarizing" phase
// (callers pass nil when there is no event stream to feed).
//
// On model failure it falls back to reading the whole reply, cleaned of
// markdown. A cap here is what once made playback start in the middle of a
// long answer: the listener cannot tell a fragment from a complete reading.
func ttsComputeSummary(ctx context.Context, curSummarizer summarize.Summarizer, text, language string, messageID int64, onStart func()) (string, error) {
	if messageID > 0 {
		if cachedSummary, found := service.GetTTSSummaryByMessageID(messageID); found && cachedSummary != "" {
			slog.Info(
				"tts summary cache hit, skipping summarization",
				slog.Int64("message_id", messageID),
			)
			return cachedSummary, nil
		}
	}

	if onStart != nil {
		onStart()
	}

	summarizeCtx, summarizeCancel := context.WithTimeout(ctx, ttsSummarizeTimeout)
	summary, err := curSummarizer.Summarize(summarizeCtx, text, language)
	summarizeCancel()
	degraded := false
	if err != nil {
		slog.Warn(
			"tts summarize failed, reading the full text instead",
			slog.String("error", err.Error()),
			slog.Int("text_len", len([]rune(text))),
		)
		fallbackSummary, fallbackErr := summarize.NewSimple().Summarize(context.Background(), text, language)
		if fallbackErr != nil || fallbackSummary == "" {
			return "", err
		}
		summary = fallbackSummary
		degraded = true
	}

	slog.Info(
		"tts summarize completed",
		slog.Int("original_len", len([]rune(text))),
		slog.Int("summary_len", len([]rune(summary))),
	)

	// Cache only a real condensation. The fallback is the raw text, not a
	// summary: caching it would pin a transient model failure to that message
	// forever, so every later read would replay the long version instead of
	// retrying the model.
	if messageID > 0 && !degraded {
		if err := service.SaveTTSSummaryByMessageID(messageID, summary); err != nil {
			slog.Warn(
				"tts failed to cache summary to DB",
				slog.String("error", err.Error()),
			)
		}
	}

	return summary, nil
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

	// Extract the final answer using the same logic as chat summary.
	// Group-chat protocol tags are stripped only for group sessions (the only
	// place they can carry a private note); a single chat's prose is read
	// verbatim, so a reply that merely DISCUSSES the tag syntax is not truncated.
	// The frontend already strips for the group case, but this path OVERRIDES
	// the frontend text with the DB content when a messageId is present, so the
	// policy must be applied here too.
	conclusion := service.AssistantConclusion(msg.SessionID, blocks)

	// Append AskUserQuestion text (questions + options) so TTS reads them
	var aqParts []string
	for _, b := range blocks {
		if b.Type != strToolUse || b.Name != "AskUserQuestion" {
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
