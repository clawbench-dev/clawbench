package speech

import "context"

// SpeechProvider abstracts audio synthesis.
// Implementations can be swapped (Edge TTS, Piper, Kokoro, MOSS-Nano, etc.)
type SpeechProvider interface {
	// Synthesize generates an audio file at outputPath from the given text.
	// language is a language code (e.g. "zh", "en") — implementations that
	// support language-specific synthesis should use it; others may ignore it.
	// Returns an error if synthesis fails.
	Synthesize(ctx context.Context, text string, outputPath string, language string) error
}

// StreamingSpeechProvider extends SpeechProvider with streaming audio output.
// Implementations send audio chunks to chunkCh while simultaneously writing to
// the output file for caching. If chunkCh is nil, behaves like Synthesize (file-only).
type StreamingSpeechProvider interface {
	SpeechProvider
	// SynthesizeStream generates audio and streams chunks to chunkCh while also
	// writing the complete file to outputPath. The caller is responsible for
	// closing chunkCh after SynthesizeStream returns.
	SynthesizeStream(ctx context.Context, text string, outputPath string, language string, chunkCh chan<- []byte) error
}
