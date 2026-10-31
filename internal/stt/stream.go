package stt

import "context"

// StreamOptions describes the live audio a caller is about to send.
type StreamOptions struct {
	// SampleRate/Channels/Bits describe the PCM the caller will Write.
	SampleRate int
	Channels   int
	Bits       int
	// Context is background text (e.g. what the user already typed) that helps
	// the model recognize the continuation. Empty means "no context".
	Context string
}

// StreamResult is one recognition update. Text is the whole transcript so far,
// not a delta, so callers can replace what they display instead of stitching
// fragments together.
type StreamResult struct {
	Text  string
	Final bool
}

// StreamingSTTProvider is implemented by engines that accept audio while the
// user is still speaking. Transcribe remains available for whole-file requests.
type StreamingSTTProvider interface {
	STTProvider
	OpenStream(ctx context.Context, opts StreamOptions) (StreamSession, error)
}

// StreamSession is one live recognition session.
//
// Write may block while the engine drains. Finish signals end-of-audio and lets
// the engine emit its final result. Results is closed once the session ends.
type StreamSession interface {
	Write(pcm []byte) error
	Finish() error
	Results() <-chan StreamResult
	Close()
}
