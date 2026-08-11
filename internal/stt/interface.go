// Package stt provides speech-to-text (ASR) providers.
package stt

import (
	"context"
	"io"
)

// STTProvider abstracts speech recognition. Implementations can be
// swapped (vLLM Whisper, etc.).
type STTProvider interface {
	// Transcribe recognizes speech from audioReader and returns the text.
	// language is a language code (e.g. "zh", "en") — implementations that
	// support language-specific recognition should use it; others may ignore it.
	// Streaming vs non-streaming is controlled by the handler layer; the
	// provider only does "given an audio segment → return text".
	Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error)
}
