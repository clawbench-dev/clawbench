package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"clawbench/internal/stt"
)

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

	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}
