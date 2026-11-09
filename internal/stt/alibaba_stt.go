package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Alibaba Cloud Model Studio (百炼 / DashScope) speech recognition.
//
// Two different APIs sit behind one provider:
//
//   - Live streaming uses Paraformer over a bidirectional WebSocket. It takes
//     raw PCM frames and returns one result per sentence, so the session
//     accumulates them into the cumulative text the StreamResult contract
//     promises.
//   - Whole-file transcription uses Qwen3-ASR-Flash through the OpenAI-compatible
//     chat endpoint (audio inline as a base64 data URI). It is what the
//     non-streaming upload path, the voice-turn device and the connectivity
//     probe call.
//
// The two APIs also differ in auth: the WebSocket validates the bearer token at
// handshake time, while the chat endpoint answers a normal HTTP error body.
const (
	// AlibabaBaseURL is the China (Beijing) endpoint. The international region
	// is a different host (dashscope-intl.aliyuncs.com) and enterprise accounts
	// may have a workspace-specific one, so the base is configurable.
	AlibabaBaseURL = "https://dashscope.aliyuncs.com"

	// AlibabaDefaultStreamModel is the real-time model. paraformer-realtime-v2
	// accepts any sample rate and supports language hints.
	AlibabaDefaultStreamModel = "paraformer-realtime-v2"

	// alibabaFileModel transcribes a complete upload. It is fixed rather than
	// configurable because it must be a chat-completion model, while the
	// configured model names a real-time one — one field cannot mean both.
	alibabaFileModel = "qwen3-asr-flash"

	alibabaChatPath        = "/compatible-mode/v1/chat/completions"
	alibabaWSInferencePath = "/api-ws/v1/inference"

	// alibabaMaxAudioBytes keeps the base64 payload under the API's 10 MB
	// request cap (encoding inflates by 4/3).
	alibabaMaxAudioBytes = 7 << 20

	// alibabaTaskTimeout bounds the run-task handshake and every write. Without
	// it an unresponsive endpoint blocks the caller until the request context
	// expires, which for a meeting is the whole meeting.
	alibabaTaskTimeout = 15 * time.Second
)

// AlibabaProvider calls Alibaba Cloud Model Studio for speech recognition.
type AlibabaProvider struct {
	APIKey      string
	BaseURL     string // default AlibabaBaseURL; international/workspace hosts go here
	StreamModel string // default AlibabaDefaultStreamModel
	Language    string // default "zh"; empty means "let the model detect it"
	HTTPClient  *http.Client

	// Recognition options shared by both paths. They map onto Paraformer's
	// punctuation_prediction_enabled / inverse_text_normalization_enabled /
	// disfluency_removal_enabled, and onto asr_options for Qwen3-ASR-Flash.
	EnableITN     bool
	EnablePunc    bool
	EnableDDC     bool
	EndWindowSize int // VAD silence threshold in ms (Paraformer max_sentence_silence)
}

// NewAlibabaProvider creates an Alibaba Cloud STT provider.
func NewAlibabaProvider(apiKey string) *AlibabaProvider {
	return &AlibabaProvider{
		APIKey:        apiKey,
		BaseURL:       AlibabaBaseURL,
		StreamModel:   AlibabaDefaultStreamModel,
		Language:      "zh",
		HTTPClient:    &http.Client{Timeout: 120 * time.Second},
		EnableITN:     true,
		EnablePunc:    true,
		EndWindowSize: 800,
	}
}

// alibabaChatRequest is the OpenAI-compatible transcription body. The audio
// travels inline as a data URI because the endpoint accepts no local path and
// we have no public URL to hand it.
type alibabaChatRequest struct {
	Model      string               `json:"model"`
	Messages   []alibabaChatMessage `json:"messages"`
	Stream     bool                 `json:"stream"`
	ASROptions *alibabaASROptions   `json:"asr_options,omitempty"`
}

type alibabaChatMessage struct {
	Role    string               `json:"role"`
	Content []alibabaChatContent `json:"content"`
}

type alibabaChatContent struct {
	Type       string            `json:"type"`
	InputAudio alibabaInputAudio `json:"input_audio"`
}

type alibabaInputAudio struct {
	Data string `json:"data"`
}

type alibabaASROptions struct {
	Language  string `json:"language,omitempty"`
	EnableITN bool   `json:"enable_itn"`
}

// alibabaChatResponse is the non-streaming answer. Only the transcript is used;
// annotations (language, emotion) and usage are deliberately ignored.
type alibabaChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Transcribe recognizes a complete audio file through Qwen3-ASR-Flash.
func (p *AlibabaProvider) Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return "", errors.New("stt: alibaba API key is required")
	}

	audio, err := io.ReadAll(audioReader)
	if err != nil {
		return "", fmt.Errorf("stt: read audio: %w", err)
	}
	if len(audio) == 0 {
		return "", errors.New("stt: empty audio")
	}
	if len(audio) > alibabaMaxAudioBytes {
		return "", fmt.Errorf("stt: audio is %d bytes; alibaba sync transcription accepts at most %d", len(audio), alibabaMaxAudioBytes)
	}

	format, err := detectAudioFormat(audio)
	if err != nil {
		return "", err
	}

	lang := p.Language
	if language != "" {
		lang = language
	}

	body, err := json.Marshal(alibabaChatRequest{
		Model: alibabaFileModel,
		Messages: []alibabaChatMessage{{
			Role: "user",
			Content: []alibabaChatContent{{
				Type:       "input_audio",
				InputAudio: alibabaInputAudio{Data: "data:" + audioMIMEType(format) + ";base64," + base64.StdEncoding.EncodeToString(audio)},
			}},
		}},
		Stream:     false,
		ASROptions: &alibabaASROptions{Language: alibabaLanguageCode(lang), EnableITN: p.EnableITN},
	})
	if err != nil {
		return "", fmt.Errorf("stt: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, alibabaChatURL(p.BaseURL), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("stt: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Body: string(respBody)}
	}

	var parsed alibabaChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("stt: decode response (HTTP %d): %s", resp.StatusCode, truncate(respBody))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("stt: alibaba returned no choices: %s", truncate(respBody))
	}
	return parsed.Choices[0].Message.Content, nil
}

// audioMIMEType maps a detected container to the MIME the API expects.
func audioMIMEType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "webm":
		return "audio/webm"
	}
	return "application/octet-stream"
}

// alibabaLanguageCode normalizes the configured language into the single code
// the chat endpoint accepts ("zh", "en", ...). Anything unrecognized is dropped
// so the model auto-detects instead of being told something invalid.
func alibabaLanguageCode(language string) string {
	code := strings.ToLower(strings.TrimSpace(language))
	if i := strings.IndexAny(code, ",; "); i >= 0 {
		code = code[:i]
	}
	switch code {
	case "zh", "en", "ja", "ko", "yue", "de", "fr", "ru", "pt", "es", "it", "ar", "hi", "id", "th", "tr", "uk", "vi", "cs", "da", "fil", "fi", "is", "ms", "no", "pl", "sv":
		return code
	}
	return ""
}

// alibabaRealtimeLanguages are the codes Paraformer's language_hints accepts.
// The file endpoint understands more (Portuguese, Spanish, …), but a hint the
// real-time model does not know is rejected rather than ignored, so an
// out-of-set language must become "no hint" (auto-detect) instead.
var alibabaRealtimeLanguages = map[string]bool{
	"zh": true, "en": true, "ja": true, "ko": true, "yue": true, "de": true, "fr": true, "ru": true,
}

// alibabaLanguageHints turns the configured language into Paraformer's
// language_hints list. An empty result means "detect it".
func alibabaLanguageHints(language string) []string {
	code := alibabaLanguageCode(language)
	if code == "" || !alibabaRealtimeLanguages[code] {
		return nil
	}
	return []string{code}
}

// alibabaBaseURL normalizes a configured base so either URL builder can start
// from the host alone. Users paste whatever the console showed them, which may
// already include the chat path, the /compatible-mode/v1 prefix or the full
// WebSocket endpoint.
func alibabaBaseURL(base string) string {
	u := strings.TrimRight(strings.TrimSpace(base), "/")
	if u == "" {
		return AlibabaBaseURL
	}
	for _, suffix := range []string{alibabaChatPath, "/compatible-mode/v1", alibabaWSInferencePath} {
		if strings.HasSuffix(u, suffix) {
			u = strings.TrimRight(strings.TrimSuffix(u, suffix), "/")
			break
		}
	}
	return u
}

// alibabaChatURL builds the transcription endpoint.
func alibabaChatURL(base string) string {
	u := alibabaBaseURL(base)
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	return u + alibabaChatPath
}

// alibabaWSURL builds the real-time inference URL from the same base.
func alibabaWSURL(base string) string {
	u := alibabaBaseURL(base)
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u + alibabaWSInferencePath
}

// alibabaTaskHeader is the envelope every client instruction shares.
type alibabaTaskHeader struct {
	Action    string `json:"action"`
	TaskID    string `json:"task_id"`
	Streaming string `json:"streaming"`
}

type alibabaTaskRequest struct {
	Header  alibabaTaskHeader  `json:"header"`
	Payload alibabaTaskPayload `json:"payload"`
}

type alibabaTaskPayload struct {
	TaskGroup  string                `json:"task_group"`
	Task       string                `json:"task"`
	Function   string                `json:"function"`
	Model      string                `json:"model"`
	Parameters alibabaTaskParameters `json:"parameters"`
	Input      map[string]any        `json:"input"`
}

type alibabaTaskParameters struct {
	Format        string   `json:"format"`
	SampleRate    int      `json:"sample_rate"`
	LanguageHints []string `json:"language_hints,omitempty"`
	// Semantic punctuation off = VAD segmentation on, which is what lets
	// max_sentence_silence decide where a sentence ends. Interaction (voice
	// input) wants that; semantic segmentation adds latency.
	SemanticPunctuationEnabled      bool `json:"semantic_punctuation_enabled"`
	MaxSentenceSilence              int  `json:"max_sentence_silence,omitempty"`
	PunctuationPredictionEnabled    bool `json:"punctuation_prediction_enabled"`
	InverseTextNormalizationEnabled bool `json:"inverse_text_normalization_enabled"`
	DisfluencyRemovalEnabled        bool `json:"disfluency_removal_enabled"`
	// Heartbeat keeps the task alive while the caller sends silence. A meeting
	// can sit quiet for a long time and the default (false) drops the socket.
	Heartbeat bool `json:"heartbeat"`
}

type alibabaFinishRequest struct {
	Header  alibabaTaskHeader    `json:"header"`
	Payload alibabaFinishPayload `json:"payload"`
}

type alibabaFinishPayload struct {
	Input map[string]any `json:"input"`
}

// alibabaServerEvent is one server push. Only the fields the session acts on
// are decoded; timestamps and per-word data are not needed.
type alibabaServerEvent struct {
	Header struct {
		TaskID       string `json:"task_id"`
		Event        string `json:"event"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				Text        string `json:"text"`
				Heartbeat   bool   `json:"heartbeat"`
				SentenceEnd bool   `json:"sentence_end"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"payload"`
}

// OpenStream starts a Paraformer real-time session.
//
// It waits for task-started before returning: the protocol forbids audio before
// that event, and the caller starts writing the moment this returns.
func (p *AlibabaProvider) OpenStream(ctx context.Context, opts StreamOptions) (StreamSession, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errors.New("stt: alibaba API key is required")
	}

	rate := opts.SampleRate
	if rate <= 0 {
		rate = 16000
	}
	channels := opts.Channels
	if channels <= 0 {
		channels = 1
	}
	if channels != 1 {
		return nil, fmt.Errorf("stt: alibaba stream accepts mono audio only (got %d channels)", channels)
	}
	bits := opts.Bits
	if bits <= 0 {
		bits = 16
	}
	if bits != 16 {
		return nil, fmt.Errorf("stt: alibaba stream accepts 16-bit PCM only (got %d-bit)", bits)
	}

	model := p.StreamModel
	if model == "" {
		model = AlibabaDefaultStreamModel
	}

	conn, resp, err := websocket.Dial(ctx, alibabaWSURL(p.BaseURL), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + p.APIKey},
			"User-Agent":    {"clawbench"},
		},
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("stt: alibaba stream dial: %w", err)
	}
	conn.SetReadLimit(1 << 20)

	taskID := uuid.NewString()
	// opts.Context is ignored on purpose: Paraformer takes hotwords through
	// vocabulary_id, not free-form background text.
	payload, err := json.Marshal(alibabaTaskRequest{
		Header: alibabaTaskHeader{Action: "run-task", TaskID: taskID, Streaming: "duplex"},
		Payload: alibabaTaskPayload{
			TaskGroup: "audio",
			Task:      "asr",
			Function:  "recognition",
			Model:     model,
			Parameters: alibabaTaskParameters{
				Format:                          "pcm",
				SampleRate:                      rate,
				LanguageHints:                   alibabaLanguageHints(p.Language),
				SemanticPunctuationEnabled:      false,
				MaxSentenceSilence:              p.EndWindowSize,
				PunctuationPredictionEnabled:    p.EnablePunc,
				InverseTextNormalizationEnabled: p.EnableITN,
				DisfluencyRemovalEnabled:        p.EnableDDC,
				Heartbeat:                       true,
			},
			Input: map[string]any{},
		},
	})
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "encode request")
		return nil, fmt.Errorf("stt: encode stream request: %w", err)
	}
	if err := alibabaWriteText(ctx, conn, payload); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "send request")
		return nil, fmt.Errorf("stt: send stream request: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	if err := alibabaWaitStarted(sessionCtx, conn); err != nil {
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}

	s := &alibabaStreamSession{
		conn:    conn,
		results: make(chan StreamResult, 8),
		done:    make(chan struct{}),
		ctx:     sessionCtx,
		cancel:  cancel,
		taskID:  taskID,
	}
	go s.readLoop(sessionCtx)
	return s, nil
}

// alibabaWaitStarted reads frames until the task is confirmed. A failure here
// is the engine rejecting the request (bad key, unknown model, quota), so it
// must surface as an open error rather than an empty session.
//
// volcConn (defined with the Doubao protocol) is just the slice of
// *websocket.Conn a session drives; both engines need the same three methods.
func alibabaWaitStarted(ctx context.Context, conn volcConn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("stt: alibaba stream handshake: %w", err)
		}
		var ev alibabaServerEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		switch ev.Header.Event {
		case "task-started":
			return nil
		case "task-failed":
			return alibabaEventError(&ev)
		}
	}
}

// alibabaEventError turns a task-failed header into a Go error, keeping the
// engine's code and message.
func alibabaEventError(ev *alibabaServerEvent) error {
	parts := make([]string, 0, 2)
	if ev.Header.ErrorCode != "" {
		parts = append(parts, "code "+ev.Header.ErrorCode)
	}
	if ev.Header.ErrorMessage != "" {
		parts = append(parts, ev.Header.ErrorMessage)
	}
	if len(parts) == 0 {
		return errors.New("stt: alibaba stream failed without a reason")
	}
	return fmt.Errorf("stt: alibaba stream failed: %s", strings.Join(parts, ": "))
}

// alibabaWriteText sends one JSON instruction with a bounded write timeout.
func alibabaWriteText(ctx context.Context, conn volcConn, payload []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, alibabaTaskTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

// alibabaStreamSession is one live recognition session.
type alibabaStreamSession struct {
	conn    volcConn
	results chan StreamResult
	done    chan struct{}
	taskID  string

	ctx    context.Context
	cancel context.CancelFunc

	writeMu sync.Mutex

	mu       sync.Mutex
	finished bool
	closed   bool
}

// Write sends one chunk of PCM audio.
func (s *alibabaStreamSession) Write(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.closed || s.finished {
		s.mu.Unlock()
		return errors.New("stt: stream already finished")
	}
	s.mu.Unlock()

	writeCtx, cancel := context.WithTimeout(s.ctx, alibabaTaskTimeout)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageBinary, pcm)
}

// Finish tells the engine that no more audio will arrive.
func (s *alibabaStreamSession) Finish() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.finished || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	taskID := s.taskID
	s.mu.Unlock()

	payload, err := json.Marshal(alibabaFinishRequest{
		Header:  alibabaTaskHeader{Action: "finish-task", TaskID: taskID, Streaming: "duplex"},
		Payload: alibabaFinishPayload{Input: map[string]any{}},
	})
	if err != nil {
		return fmt.Errorf("stt: encode finish request: %w", err)
	}
	return alibabaWriteText(s.ctx, s.conn, payload)
}

// Results streams recognition updates until the session ends.
func (s *alibabaStreamSession) Results() <-chan StreamResult { return s.results }

// Close releases the connection. Safe to call more than once.
//
// It does not take writeMu on purpose: cancelling the session context aborts a
// write stuck on an unresponsive upstream, and closing the socket unblocks it
// too. Waiting for the writer would hold the caller for the full write timeout.
func (s *alibabaStreamSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	close(s.done)
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

// readLoop accumulates per-sentence results into cumulative text.
//
// Paraformer reports one sentence at a time (sentence_end=false while it is
// still revising, true once the sentence is final), whereas StreamResult.Text
// is the whole transcript so far. Finished sentences are therefore kept as a
// prefix and the sentence in progress is appended to it.
func (s *alibabaStreamSession) readLoop(ctx context.Context) {
	defer close(s.results)

	var committed, current, last string
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			break
		}
		var ev alibabaServerEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		switch ev.Header.Event {
		case "task-failed":
			s.emit(StreamResult{Err: alibabaEventError(&ev)})
			return
		case "task-finished":
			s.emit(StreamResult{Text: last, Final: true})
			return
		case "result-generated":
			sentence := ev.Payload.Output.Sentence
			// Heartbeat results are keep-alive packets, not speech.
			if sentence.Heartbeat {
				continue
			}
			if sentence.SentenceEnd {
				committed += sentence.Text
				current = ""
			} else {
				current = sentence.Text
			}
			text := committed + current
			if text == last {
				continue
			}
			last = text
			if !s.emit(StreamResult{Text: last}) {
				return
			}
		}
	}

	s.emit(StreamResult{Text: last, Final: true})
}

// emit delivers a result unless the session has been closed.
func (s *alibabaStreamSession) emit(r StreamResult) bool {
	select {
	case s.results <- r:
		return true
	case <-s.done:
		return false
	}
}
