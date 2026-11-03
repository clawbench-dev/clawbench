// Package audio converts synthesized speech between container formats and the
// headerless PCM that embedded clients consume.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// Format describes a headerless PCM stream.
type Format struct {
	SampleRate int
	Channels   int
	Bits       int
}

// decodeBufferBytes is a multiple of 4 (one stereo 16-bit frame) so a buffer
// boundary never splits a frame in the common case.
const decodeBufferBytes = 8192

// DecodeMP3ToPCMFile decodes srcPath (MP3) into dstPath as headerless
// little-endian 16-bit mono PCM.
//
// go-mp3 always yields 16-bit stereo, so mono sources are downmixed by
// averaging the two identical channels. The output sample rate is the source
// rate; no resampling is performed.
//
// The destination is written through a temporary file and renamed on success,
// so a partially written file is never served from the cache.
func DecodeMP3ToPCMFile(srcPath, dstPath string) (Format, error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return Format{}, fmt.Errorf("audio: open mp3: %w", err)
	}
	defer func() { _ = in.Close() }()

	dec, err := mp3.NewDecoder(in)
	if err != nil {
		return Format{}, fmt.Errorf("audio: decode mp3 header: %w", err)
	}

	format := Format{SampleRate: dec.SampleRate(), Channels: 1, Bits: 16}
	if format.SampleRate <= 0 {
		return Format{}, errors.New("audio: mp3 reports an invalid sample rate")
	}

	if mkErr := os.MkdirAll(filepath.Dir(dstPath), 0o755); mkErr != nil {
		return Format{}, fmt.Errorf("audio: create output directory: %w", mkErr)
	}

	// The temp name must be unique per call, not derived from dstPath: two
	// concurrent decodes of the same source (same dstPath) would otherwise
	// truncate each other's file mid-write and publish interleaved bytes under
	// the final name.
	out, err := os.CreateTemp(filepath.Dir(dstPath), filepath.Base(dstPath)+".tmp-*")
	if err != nil {
		return Format{}, fmt.Errorf("audio: create pcm file: %w", err)
	}
	tmpPath := out.Name()

	if err := downmixToMono(dec, out); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return Format{}, err
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return Format{}, fmt.Errorf("audio: close pcm file: %w", err)
	}

	if err := publishTempFile(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return Format{}, fmt.Errorf("audio: rename pcm file: %w", err)
	}

	return format, nil
}

// publishTempFile renames tmp onto dst, retrying a permission failure briefly.
//
// POSIX rename replaces the destination atomically, so any number of concurrent
// publishes to the same path simply end with one of the complete files. Windows
// implements the replace as delete-then-rename, and a second publish landing
// while the first is inside that window fails with "Access is denied" — even
// though both writers only ever produce complete files. The retry is what makes
// the concurrent-decode guarantee hold there; on POSIX it never fires.
func publishTempFile(tmp, dst string) error {
	const attempts = 20
	const backoff = 10 * time.Millisecond
	err := os.Rename(tmp, dst)
	for i := 0; i < attempts && errors.Is(err, fs.ErrPermission); i++ {
		time.Sleep(backoff)
		err = os.Rename(tmp, dst)
	}
	return err
}

// downmixToMono reads stereo s16le frames from r and writes averaged mono
// samples to w. Any trailing bytes of an incomplete frame are carried into the
// next read, so the output is always frame-aligned.
func downmixToMono(r io.Reader, w io.Writer) error {
	buf := make([]byte, decodeBufferBytes)
	out := make([]byte, 0, decodeBufferBytes/2)
	pending := make([]byte, 0, 4)

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			frames := len(pending) / 4
			if frames > 0 {
				out = out[:0]
				for i := 0; i < frames*4; i += 4 {
					left := int16(binary.LittleEndian.Uint16(pending[i : i+2]))    //nolint:gosec // 16-bit PCM reinterpretation
					right := int16(binary.LittleEndian.Uint16(pending[i+2 : i+4])) //nolint:gosec // 16-bit PCM reinterpretation
					mono := int16((int32(left) + int32(right)) / 2)                //nolint:gosec // average of two int16 stays in range
					out = append(out, byte(mono), byte(mono>>8))                   //nolint:gosec // little-endian split of int16
				}
				if _, err := w.Write(out); err != nil {
					return fmt.Errorf("audio: write pcm: %w", err)
				}
				pending = append(pending[:0], pending[frames*4:]...)
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("audio: read mp3: %w", readErr)
		}
	}
}
