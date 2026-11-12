package speech

import (
	"fmt"
	"io"
)

// mp3Format is the audio format every provider asks the server for. It is one
// constant because the whole pipeline depends on it (cache extension, browser
// playback, device PCM decoding).
const mp3Format = "mp3"

// audioSink validates that the bytes a provider writes are actually MP3 before
// any of them reach the destination, and counts what was written.
//
// Every TTS engine in this package is asked for MP3 because the rest of the
// pipeline assumes it: the cache file is named .mp3, the browser plays
// audio/mpeg, and the device path decodes with a go-mp3 decoder. A server that
// ignores the requested format and answers with WAV would otherwise be cached
// under an .mp3 name and fail silently at playback time.
//
// The sniff window is small (mp3SniffPrefixBytes), so at most a handful of
// bytes are held back before the decision.
type audioSink struct {
	w         io.Writer
	name      string // provider prefix for error messages, e.g. "alibaba-tts"
	total     int64
	prefix    []byte
	validated bool
	err       error
}

func newAudioSink(w io.Writer, name string) *audioSink {
	return &audioSink{w: w, name: name}
}

func (s *audioSink) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.validated {
		n, err := s.w.Write(p)
		s.total += int64(n)
		if err != nil {
			s.err = fmt.Errorf("%s: failed to write audio: %w", s.name, err)
			return n, s.err
		}
		return n, nil
	}

	s.prefix = append(s.prefix, p...)
	if len(s.prefix) < mp3SniffPrefixBytes {
		// Buffered, not written: report success so the caller keeps sending.
		return len(p), nil
	}
	if !isMP3(s.prefix) {
		s.err = fmt.Errorf("%s: response is not mp3 (%s); the request asked for mp3", s.name, describeAudioFormat(s.prefix))
		return 0, s.err
	}
	s.validated = true
	buffered := s.prefix
	s.prefix = nil
	n, err := s.w.Write(buffered)
	s.total += int64(n)
	if err != nil {
		s.err = fmt.Errorf("%s: failed to write audio: %w", s.name, err)
		return 0, s.err
	}
	if n < len(buffered) {
		s.err = fmt.Errorf("%s: failed to write audio: %w", s.name, io.ErrShortWrite)
		return 0, s.err
	}
	return len(p), nil
}

// finish flushes a stream shorter than the sniff window. A short but valid MP3
// frame is accepted; anything else is reported as a format error.
func (s *audioSink) finish() error {
	if s.err != nil {
		return s.err
	}
	if s.validated || len(s.prefix) == 0 {
		return nil
	}
	if !isMP3(s.prefix) {
		return fmt.Errorf("%s: response is not mp3 (%s); the request asked for mp3", s.name, describeAudioFormat(s.prefix))
	}
	n, err := s.w.Write(s.prefix)
	s.total += int64(n)
	s.prefix = nil
	s.validated = true
	if err != nil {
		return fmt.Errorf("%s: failed to write audio: %w", s.name, err)
	}
	return nil
}
