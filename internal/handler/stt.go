package handler

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/stt"

	"github.com/coder/websocket"
)

//go:embed assets/audio_stt_probe.mp3
var sttProbeFiles embed.FS

// sttProbeAudio is the embedded "你好" voice clip used as a real-audio probe
// for STT connectivity tests (replaces the old silence WAV, which could not
// verify actual speech recognition).
var sttProbeAudio = mustLoadSTTProbe()

// mustLoadSTTProbe reads the embedded mp3 probe, panicking if it is missing so
// misconfiguration is caught at startup.
func mustLoadSTTProbe() []byte {
	data, err := sttProbeFiles.ReadFile("assets/audio_stt_probe.mp3")
	if err != nil {
		panic("stt: embedded probe audio missing: " + err.Error())
	}
	return data
}

// sttProvider is the global STT provider instance.
// Access is protected by sttProviderMu for hot-reload safety.
var (
	sttProvider   stt.STTProvider = stt.NewVLLMProvider("http://localhost:8000/v1", "openai/whisper-large-v3", "", "zh")
	sttProviderMu sync.RWMutex
)

// SetSTTProvider replaces the global STT provider.
// Goroutine-safe: concurrent Transcribe calls are protected by RWMutex.
func SetSTTProvider(p stt.STTProvider) {
	sttProviderMu.Lock()
	sttProvider = p
	sttProviderMu.Unlock()
}

// GetSTTProvider returns the current STT provider.
// Goroutine-safe for concurrent reads.
func GetSTTProvider() stt.STTProvider {
	sttProviderMu.RLock()
	p := sttProvider
	sttProviderMu.RUnlock()
	return p
}

// sttMaxBodyBytes limits the STT request body size (10MB).
const sttMaxBodyBytes = 10 << 20

// STTTranscribe handles POST /api/stt/transcribe (non-streaming).
// Multipart fields: file (audio), language (optional).
func STTTranscribe(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sttMaxBodyBytes)

	language := r.FormValue("language")
	file, _, err := r.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{strReqError: "audio file too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "missing audio file"})
		return
	}
	defer func() { _ = file.Close() }()

	provider := GetSTTProvider()
	text, err := provider.Transcribe(r.Context(), file, language)
	if err != nil {
		slog.Error("stt: transcribe failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: "transcription failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{sttWSText: text})
}

// sttWSWriteTimeout is the timeout for individual WebSocket writes.
const sttWSWriteTimeout = 5 * time.Second

// sttWSMaxAudioBytes caps total streamed audio per WS connection (20MB).
const sttWSMaxAudioBytes = 20 << 20

// sttWS type constants (satisfy goconst, aligned with existing WS constants).
const (
	sttWSType   = "type"
	sttWSText   = "text"
	sttWSDone   = "done"
	sttWSError  = "error"
	sttWSEndCtl = "end"
)

// errSTTWSEnd is the sentinel returned by handleSTTWSMessage when the
// client sends the "end" control frame, signaling a clean stream completion.
var errSTTWSEnd = errors.New("stt ws: end")

// sttWSControl is a client text control message over the streaming WS.
type sttWSControl struct {
	Type string `json:"type"` // "end"
}

// sttWSServerMsg is a server message over the streaming WS.
type sttWSServerMsg struct {
	Type  string `json:"type"`            // "text" (incremental) or "done" (final)
	Text  string `json:"text,omitempty"`  // incremental text for "text"
	Final string `json:"final,omitempty"` // final full text for "done"
}

// sttStreamState holds the mutable streaming state, protected by mu to avoid
// data races between the incremental goroutine and the read goroutine.
type sttStreamState struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	offset int
	done   bool
}

// STTTranscribeWS handles WS /api/stt/transcribe/ws (streaming).
//
// Client → server:
//   - binary frames: raw audio bytes (appended to running buffer)
//   - text frame: {"type":"end"} signals recording stopped → final full transcription
//
// Server → client:
//   - {"type":"text","text":"<incremental>"} — transcribed new segment
//   - {"type":"done","final":"<full text>"} — final result after "end"
//   - {"type":"error","message":"..."} — failure
//
// The server accumulates audio. At each ChunkMs tick it transcribes only the
// newly-appended portion and appends the incremental text. After "end", it
// re-transcribes the full buffer and sends the final result.
func STTTranscribeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"http://" + r.Host,
			"https://" + r.Host,
			"http://localhost:*",
			"https://localhost:*",
			"http://127.0.0.1:*",
			"https://127.0.0.1:*",
		},
	})
	if err != nil {
		slog.Error("stt ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	cfg := model.ConfigInstance
	chunkMs := cfg.STT.ChunkMs
	if chunkMs <= 0 {
		chunkMs = 1000
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	state := &sttStreamState{}
	provider := GetSTTProvider()
	newAudio := make(chan struct{}, 1)
	readErr := make(chan error, 1)

	// Incremental goroutine: on each tick (when new audio is available),
	// transcribe the newly appended segment and send incremental text.
	go func() {
		ticker := time.NewTicker(time.Duration(chunkMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-newAudio:
			}
			if stop, abort := streamIncrementalChunk(ctx, state, provider, conn, cfg.STT.Language); abort {
				cancel()
				return
			} else if stop {
				return
			}
		}
	}()

	// Read goroutine: collect audio frames and watch for "end".
	go func() {
		for {
			mt, msg, rerr := conn.Read(ctx)
			if rerr != nil {
				readErr <- rerr
				return
			}
			if rerr = handleSTTWSMessage(state, newAudio, conn, mt, msg); rerr != nil {
				readErr <- rerr
				return
			}
		}
	}()

	<-readErr // client ended (end control) or connection error

	// Final full transcription of the whole buffer.
	state.mu.Lock()
	full := append([]byte(nil), state.buffer.Bytes()...)
	state.mu.Unlock()

	finalText := ""
	if len(full) > 0 {
		text, terr := provider.Transcribe(ctx, bytes.NewReader(full), cfg.STT.Language)
		if terr == nil {
			finalText = text
		}
	}

	msg := sttWSServerMsg{Type: sttWSDone, Final: finalText}
	data, _ := json.Marshal(msg)
	_ = writeSTTWSText(conn, data)
}

// streamIncrementalChunk transcribes the newly-appended audio segment and
// sends the incremental text. It returns (stop, abort): stop=true when the
// stream has finished and the goroutine should exit cleanly; abort=true when a
// write error occurred and the whole connection should be cancelled.
func streamIncrementalChunk(ctx context.Context, state *sttStreamState, provider stt.STTProvider, conn *websocket.Conn, language string) (bool, bool) {
	state.mu.Lock()
	done := state.done
	start := state.offset
	seg := append([]byte(nil), state.buffer.Bytes()[start:]...)
	state.offset = state.buffer.Len()
	state.mu.Unlock()

	if done || len(seg) == 0 {
		return true, false
	}

	text, terr := provider.Transcribe(ctx, bytes.NewReader(seg), language)
	if terr != nil {
		slog.Debug("stt ws: incremental transcribe failed", slog.String("error", terr.Error()))
		return false, false
	}
	if text == "" {
		return false, false
	}
	msg := sttWSServerMsg{Type: sttWSText, Text: text}
	data, _ := json.Marshal(msg)

	state.mu.Lock()
	doneNow := state.done
	state.mu.Unlock()
	if doneNow {
		return false, false
	}

	if err := writeSTTWSText(conn, data); err != nil {
		return false, true
	}
	return false, false
}

// handleSTTWSMessage processes a single STT WebSocket frame. Binary frames
// append audio to the streaming buffer (bounded); a text "end" control frame
// marks the stream done. Returns a non-nil error to terminate the read loop.
func handleSTTWSMessage(state *sttStreamState, newAudio chan struct{}, conn *websocket.Conn, mt websocket.MessageType, msg []byte) error {
	switch mt {
	case websocket.MessageBinary:
		state.mu.Lock()
		if state.buffer.Len()+len(msg) > sttWSMaxAudioBytes {
			state.mu.Unlock()
			errData, _ := json.Marshal(sttWSServerMsg{Type: sttWSError})
			_ = writeSTTWSText(conn, errData)
			return errors.New("stt ws: audio exceeds limit")
		}
		state.buffer.Write(msg)
		state.mu.Unlock()
		select {
		case newAudio <- struct{}{}:
		default:
		}
	case websocket.MessageText:
		var ctl sttWSControl
		if json.Unmarshal(msg, &ctl) == nil && ctl.Type == sttWSEndCtl {
			state.mu.Lock()
			state.done = true
			state.mu.Unlock()
			return errSTTWSEnd
		}
	}
	return nil
}

// writeSTTWSText sends a text message to the client with a write timeout.
func writeSTTWSText(conn *websocket.Conn, data []byte) error {
	writeCtx, cancel := context.WithTimeout(context.Background(), sttWSWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
