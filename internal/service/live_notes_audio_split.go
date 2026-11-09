package service

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
)

// LiveNoteAudioWindowMS is the default chunk length used for re-recognition.
// It matches the order of magnitude of the client's recording slices, so a
// re-recognized line is not coarser than the original transcript.
const LiveNoteAudioWindowMS int64 = 20_000

// liveNoteMergedAudioMaxBytes bounds one merged channel file read by the
// splitter. The segment reader's 32 MiB cap is too small for a long meeting;
// 256 MiB covers a multi-hour Opus recording with a wide margin.
const liveNoteMergedAudioMaxBytes = 256 << 20

// LiveNoteAudioChunk is one self-contained WebM window cut out of a merged
// channel file. OffsetMS is where the window starts inside the merged file; the
// caller adds the file's absolute origin (live_notes_audio.base_at).
type LiveNoteAudioChunk struct {
	OffsetMS   int64
	DurationMS int64
	WebM       []byte
}

// SplitLiveNoteAudio cuts one merged channel file into windows that can be sent
// to an STT provider independently.
//
// The container is rewritten, not transcoded: each output carries the original
// EBML header, Info (with a duration for the window) and Tracks, plus the
// window's clusters with their timecodes shifted to start at zero. That is the
// same surgery MergeLiveNoteAudio does in the other direction.
func SplitLiveNoteAudio(path string, windowMS int64) ([]LiveNoteAudioChunk, error) {
	if windowMS <= 0 {
		windowMS = LiveNoteAudioWindowMS
	}
	file, err := readLiveNoteWebMWithCap(path, liveNoteMergedAudioMaxBytes)
	if err != nil {
		return nil, err
	}
	if len(file.header) == 0 || len(file.info) == 0 || len(file.tracks) == 0 {
		return nil, fmt.Errorf("live notes: %s is not a self-contained WebM recording", filepath.Base(path))
	}
	if len(file.clusters) == 0 {
		return nil, ErrLiveNotesEmpty
	}

	chunks := make([]LiveNoteAudioChunk, 0, len(file.clusters)/4+1)
	window := make([][]byte, 0, 32)
	windowStart := liveNoteTimecodeMS(liveNoteClusterTimecode(file.clusters[0]))
	lastTimecode := windowStart

	flush := func() error {
		if len(window) == 0 {
			return nil
		}
		duration := lastTimecode - windowStart + 1000
		if duration <= 0 {
			duration = windowMS
		}
		data, buildErr := buildLiveNoteAudioChunk(file, window, windowStart, duration)
		if buildErr != nil {
			return buildErr
		}
		chunks = append(chunks, LiveNoteAudioChunk{
			OffsetMS:   windowStart,
			DurationMS: duration,
			WebM:       data,
		})
		return nil
	}

	for _, cluster := range file.clusters {
		timecode := liveNoteTimecodeMS(liveNoteClusterTimecode(cluster))
		if len(window) > 0 && timecode-windowStart >= windowMS {
			if flushErr := flush(); flushErr != nil {
				return nil, flushErr
			}
			window = window[:0]
			windowStart = timecode
		}
		window = append(window, cluster)
		lastTimecode = timecode
	}
	if flushErr := flush(); flushErr != nil {
		return nil, flushErr
	}
	return chunks, nil
}

// liveNoteTimecodeMS converts an EBML timecode to milliseconds without
// wrapping. A value above MaxInt64 is corrupt data; wrapping would turn it into
// a negative offset and move the window before the start of the recording.
func liveNoteTimecodeMS(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// buildLiveNoteAudioChunk assembles one self-contained WebM window.
func buildLiveNoteAudioChunk(file *liveNoteWebM, clusters [][]byte, windowStart, durationMS int64) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(file.header)
	buf.Write(encodeEBMLID(ebmlIDSegment))
	sizePos := buf.Len()
	buf.Write(encodeEBMLSizeFixed(0, 8))
	bodyStart := buf.Len()

	info, err := liveNoteInfoWithDuration(file.info)
	if err != nil {
		return nil, fmt.Errorf("live notes: rebuild chunk info: %w", err)
	}
	if len(info) < 8 {
		return nil, errors.New("live notes: chunk info is too short")
	}
	// liveNoteInfoWithDuration appends the Duration placeholder as the last
	// eight bytes of the Info element.
	durationPos := bodyStart + len(info) - 8
	buf.Write(info)
	buf.Write(file.tracks)

	for _, cluster := range clusters {
		shifted, shiftErr := shiftLiveNoteClusterBy(cluster, -windowStart)
		if shiftErr != nil {
			return nil, shiftErr
		}
		buf.Write(shifted)
	}

	data := buf.Bytes()
	copy(data[sizePos:sizePos+8], encodeEBMLSizeFixed(len(data)-bodyStart, 8))
	binary.BigEndian.PutUint64(data[durationPos:durationPos+8], math.Float64bits(float64(durationMS)))
	return data, nil
}

// shiftLiveNoteClusterBy moves a cluster's Timecode by a signed delta.
//
// The existing shiftLiveNoteCluster only adds (it is used to place segments on
// the meeting timeline); cutting a window needs the opposite direction.
func shiftLiveNoteClusterBy(cluster []byte, deltaMS int64) ([]byte, error) {
	el, err := decodeLiveNoteContainerElement(cluster)
	if err != nil {
		return nil, err
	}
	if el.id != ebmlIDCluster {
		return nil, fmt.Errorf("expected a Cluster, found %#x", el.id)
	}
	payload := cluster[el.header : el.header+el.size]

	timecode, found := int64(0), false
	body := make([]byte, 0, len(payload))
	for pos := 0; pos < len(payload); {
		child, err := decodeLiveNoteElement(payload[pos:])
		if err != nil {
			return nil, err
		}
		start := pos + child.header
		end := start + child.size
		if child.id == ebmlIDTimecode {
			if child.size > 8 {
				return nil, fmt.Errorf("timecode of %d bytes", child.size)
			}
			timecode = liveNoteTimecodeMS(decodeLiveNoteUint(payload[start:end]))
			found = true
			pos = end
			continue
		}
		body = append(body, payload[pos:end]...)
		pos = end
	}
	if !found {
		return nil, errors.New("cluster has no Timecode element")
	}
	next := timecode + deltaMS
	if next < 0 {
		next = 0
	}
	inner := append(encodeLiveNoteElement(encodeEBMLID(ebmlIDTimecode), encodeLiveNoteUint(uint64(next))), body...)
	out := append([]byte{}, cluster[:el.idLen]...)
	out = append(out, encodeEBMLSize(len(inner))...)
	return append(out, inner...), nil
}
