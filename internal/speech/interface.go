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

// CacheIdentityProvider is implemented by providers whose audio output depends
// on configuration beyond the input text — voice, model, rate, and so on.
//
// The TTS cache key is derived from the spoken text alone, so without this the
// same sentence would replay audio synthesized with a *previous* voice after
// the user changes one. Folding the identity into the key makes a
// configuration change produce a different cache entry, which is what the
// user expects ("I changed the voice and it sounds the same" is otherwise
// indistinguishable from a broken setting).
//
// The returned string must cover every field that changes the audio, and must
// NOT include secrets — it is hashed into an on-disk file name.
type CacheIdentityProvider interface {
	// CacheIdentity returns a stable description of the settings that affect
	// synthesis. An empty string means "no identity beyond the text".
	CacheIdentity() string
}
