package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/service"

	"github.com/coder/websocket"
)

const (
	// ttsWSWriteTimeout is the timeout for individual WebSocket writes.
	ttsWSWriteTimeout = 5 * time.Second

	// TTS WS protocol constants (used instead of string literals
	// to satisfy goconst; aligned with existing strError / frpKeyMessage).
	ttsWSType   = "type"
	ttsWSResult = "result"
	ttsWSDone   = "done"
)

// ttsWSStartMessage is the client's initial message to start streaming.
type ttsWSStartMessage struct {
	Type  string `json:"type"`  // must be "start"
	JobID string `json:"jobId"` // cache key from POST /api/tts/generate
}

// TTSAudioWS handles the /api/tts/audio/ws WebSocket endpoint.
// Auth is handled by middleware.Auth before this function is called.
//
// Protocol:
//   - Client sends: {"type":"start", "jobId":"<cacheKey>"}
//   - Server sends text frames: {"type":"phase","phase":"summarizing"}, {"type":"done","audioPath":"..."}, {"type":"error","message":"..."}
//   - Server sends binary frames: raw MP3 audio chunks
func TTSAudioWS(w http.ResponseWriter, r *http.Request) {
	// Extract project path before WS upgrade (for disk-based fallback lookups)
	projectPath := middleware.GetProjectFromCookie(r)

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
		slog.Error("tts audio ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	// Read start message from client
	_, msg, err := conn.Read(r.Context())
	if err != nil {
		return // client disconnected before sending start
	}

	var startMsg ttsWSStartMessage
	if err := json.Unmarshal(msg, &startMsg); err != nil || startMsg.Type != "start" {
		writeTTSAudioWSError(conn, "invalid start message")
		return
	}

	// Validate jobId format (SHA-256 hex prefix)
	jobID := startMsg.JobID
	if !isValidTTSJobID(jobID) {
		writeTTSAudioWSError(conn, "invalid jobId format")
		return
	}

	// Check if job exists
	job, ok := service.GetTTSJob(jobID)
	if !ok {
		handleTTSWSCompletedJob(conn, jobID, projectPath)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Write mutex for concurrent writes (audio chunks + control messages)
	var writeMu sync.Mutex

	// Goroutine: Watch for client disconnect → cancel TTS job
	go func() {
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				service.CancelTTSJob(jobID)
				cancel()
				return
			}
		}
	}()

	// Goroutine: Forward phase/result events from StreamCh → WS text frames
	go ttsWSForwardEvents(job, conn, &writeMu, cancel)

	// Main goroutine: Forward audio chunks from AudioCh → WS binary frames
	for chunk := range job.AudioCh {
		writeMu.Lock()
		writeCtx, writeCancel := context.WithTimeout(context.Background(), ttsWSWriteTimeout)
		writeErr := conn.Write(writeCtx, websocket.MessageBinary, chunk)
		writeCancel()
		writeMu.Unlock()
		if writeErr != nil {
			break // connection dead, stop sending
		}
	}

	// Wait for job completion or context cancellation
	select {
	case <-job.Done:
	case <-ctx.Done():
	}
}

// handleTTSWSCompletedJob handles the case where a WS client connects after
// the TTS job has already completed. Checks disk for the audio file and sends
// a "done" message so the client can fall back to file-based playback.
func handleTTSWSCompletedJob(conn *websocket.Conn, jobID, projectPath string) {
	relAudioPath := filepath.Join(".clawbench", "generated", "tts", jobID+".mp3")
	if projectPath != "" {
		absPath := filepath.Join(projectPath, relAudioPath)
		if info, statErr := os.Stat(absPath); statErr == nil && info.Size() > 0 {
			writeTTSAudioWSEvent(conn, map[string]any{ttsWSType: ttsWSDone, "audioPath": relAudioPath})
		} else {
			writeTTSAudioWSError(conn, "job not found")
		}
	} else {
		writeTTSAudioWSError(conn, "job not found")
	}
}

// ttsWSForwardEvents forwards phase/result events from StreamCh to WS text frames.
// Translates internal TTSEvent types to WS protocol types:
//
//	phase event  → {"type":"phase","phase":"..."}
//	result error → {"type":"error","message":"..."}
//	result ok    → {"type":"done","audioPath":"...","summary":"..."}
func ttsWSForwardEvents(job *service.TTSJob, conn *websocket.Conn, writeMu *sync.Mutex, cancel context.CancelFunc) {
	for event := range job.StreamCh {
		wsEvent := ttsWSEventFromTTSEvent(event)
		writeMu.Lock()
		writeTTSAudioWSEvent(conn, wsEvent)
		writeMu.Unlock()
		if event.Type == ttsWSResult {
			cancel() // signal done to main goroutine
			return
		}
	}
}

// ttsWSEventFromTTSEvent converts an internal TTSEvent to a WS protocol message.
func ttsWSEventFromTTSEvent(event service.TTSEvent) any {
	if event.Type == ttsWSResult {
		if event.SynthesizeFailed {
			return map[string]any{
				ttsWSType:     strError,
				frpKeyMessage: event.SynthesizeError,
			}
		}
		return map[string]any{
			ttsWSType:   ttsWSDone,
			"audioPath": event.AudioPath,
			"summary":   event.Summary,
		}
	}
	return map[string]any{
		ttsWSType: event.Type,
		"phase":   event.Phase,
	}
}

// isValidTTSJobID validates that jobId matches SHA-256 hex prefix format.
func isValidTTSJobID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

// writeTTSAudioWSEvent sends a JSON text frame on the WebSocket connection.
func writeTTSAudioWSEvent(conn *websocket.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), ttsWSWriteTimeout)
	defer writeCancel()
	_ = conn.Write(writeCtx, websocket.MessageText, data)
}

// writeTTSAudioWSError sends an error text frame on the WebSocket connection.
func writeTTSAudioWSError(conn *websocket.Conn, message string) {
	writeTTSAudioWSEvent(conn, map[string]any{ttsWSType: strError, frpKeyMessage: message})
}
