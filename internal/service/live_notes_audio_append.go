package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Real-time assembly of a channel's recording.
//
// The old shape was "keep every 20 s segment on disk, then remux the lot when
// the meeting ends". That left the recording unusable until the end, and lost
// everything if the process died before the merge ran.
//
// The remux itself is only a byte copy (same codec parameters, clusters shifted
// onto the meeting timeline), so it can run after EVERY segment instead of once
// at the end: append the new clusters, patch the two sizes in the header. Cost
// per segment is proportional to the new audio, not to the meeting length.
//
// What it costs: the segment is deleted once it is in the channel file, so a
// crash between "clusters written" and "header patched" can lose the tail. The
// header is patched from the file's real length on the next append, so the file
// itself never becomes unreadable — it just ends earlier than the audio did.

// liveNoteAppendState is the sidecar that makes an append resumable.
//
// The header offsets are fixed once the first segment is written, but they are
// not derivable from the merged file without re-parsing EBML, and the segment
// they came from is deleted. Persisting them is cheaper and exact.
type liveNoteAppendState struct {
	// Base is the first segment's startedAt: every cluster is shifted by its
	// distance from this, which is what preserves the pauses between segments.
	Base int64 `json:"base"`
	// Header field offsets, fixed for the life of the file.
	SizePos     int64 `json:"sizePos"`
	BodyStart   int64 `json:"bodyStart"`
	DurationPos int64 `json:"durationPos"`
	// TotalMS is the furthest point on the timeline written so far.
	TotalMS float64 `json:"totalMs"`
	// LastStartedAt is the newest segment folded in. A retry that delivers an
	// older segment must not be appended twice.
	LastStartedAt int64 `json:"lastStartedAt"`
}

// appendLocks serializes appends per target file. The publisher's queue is FIFO
// per channel, but a retry and a fresh segment can overlap.
var appendLocks sync.Map // map[string]*sync.Mutex

func appendLockFor(path string) *sync.Mutex {
	v, _ := appendLocks.LoadOrStore(path, &sync.Mutex{})
	mu, ok := v.(*sync.Mutex)
	if !ok {
		// Unreachable: only this function stores into the map. A fresh mutex
		// would serialize nothing, so return a shared one rather than nil.
		return &fallbackAppendLock
	}
	return mu
}

// fallbackAppendLock is the shared lock for the impossible case above.
var fallbackAppendLock sync.Mutex

// AppendLiveNoteAudio folds one segment into its channel's recording file.
//
// Returns true when the segment has been consumed (appended and deleted), so the
// caller can stop treating it as a leftover. A false result with a nil error
// means "this segment is older than what is already in the file" — a duplicate
// delivery, which is dropped rather than appended out of order.
func AppendLiveNoteAudio(paths LiveNotesPaths, channel string, startedAt int64, segPath string) (bool, error) {
	if segPath == "" {
		return false, nil
	}
	target := filepath.Join(paths.Audio, liveNoteAudioFileName(channel))
	sidecar := filepath.Join(paths.Audio, liveNoteAppendSidecarName(channel))

	mu := appendLockFor(target)
	mu.Lock()
	defer mu.Unlock()

	seg, err := readLiveNoteWebM(segPath)
	if err != nil {
		return false, err
	}
	state, err := loadLiveNoteAppendState(sidecar, target)
	if err != nil {
		return false, err
	}
	if state == nil {
		// No usable sidecar. If a recording is already there, it was written by
		// an earlier run whose bookkeeping did not survive (a crash between the
		// header patch and the sidecar write). Truncating it would throw away
		// every minute recorded so far, and the segments are already gone — so
		// leave this one on disk for the end-of-meeting merge instead.
		if info, statErr := os.Stat(target); statErr == nil && info.Size() > 0 {
			return false, nil
		}
	}
	if state != nil && startedAt <= state.LastStartedAt {
		// Already folded in (or older than the file's head). Dropping it keeps
		// the timeline monotonic, which is the whole point of the shift.
		return true, removeLiveNoteSegment(segPath)
	}

	fresh := state == nil
	if fresh {
		state = &liveNoteAppendState{Base: startedAt}
	}
	f, err := openLiveNoteTarget(target, fresh)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	if err := foldLiveNoteSegment(f, seg, segPath, state, startedAt, fresh); err != nil {
		return false, err
	}
	if err := saveLiveNoteAppendState(sidecar, state); err != nil {
		return false, err
	}
	return true, removeLiveNoteSegment(segPath)
}

// foldLiveNoteSegment appends one segment's clusters and re-patches the header.
//
// On the first segment it also writes the header (and records where its two
// patchable fields live); every later one only appends.
func foldLiveNoteSegment(
	f *os.File, seg *liveNoteWebM, segPath string, state *liveNoteAppendState, startedAt int64, fresh bool,
) error {
	if fresh {
		if len(seg.header) == 0 || len(seg.tracks) == 0 {
			return fmt.Errorf("live notes: %s is not a self-contained WebM segment", filepath.Base(segPath))
		}
		spans, err := writeLiveNoteHeader(f, seg)
		if err != nil {
			return err
		}
		state.SizePos, state.BodyStart, state.DurationPos = spans.sizePos, spans.bodyStart, spans.durationPos
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("live notes: seek end: %w", err)
	}
	shift := uint64(0)
	if startedAt > state.Base {
		shift = uint64(startedAt - state.Base) //nolint:gosec // guarded by the comparison above
	}
	for _, cluster := range seg.clusters {
		shifted, err := shiftLiveNoteCluster(cluster, shift)
		if err != nil {
			return fmt.Errorf("live notes: %s: %w", filepath.Base(segPath), err)
		}
		if _, err := f.Write(shifted); err != nil {
			return fmt.Errorf("live notes: append cluster: %w", err)
		}
	}
	if endAt := float64(shift) + liveNoteSegmentEndMS(seg); endAt > state.TotalMS {
		state.TotalMS = endAt
	}
	written, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("live notes: tell end: %w", err)
	}
	spans := liveNoteHeaderSpans{
		sizePos:     state.SizePos,
		bodyStart:   state.BodyStart,
		durationPos: state.DurationPos,
	}
	if err := patchLiveNoteHeader(f, spans, written-state.BodyStart, state.TotalMS); err != nil {
		return err
	}
	state.LastStartedAt = startedAt
	return nil
}

// removeLiveNoteSegment deletes a segment that is now inside the channel file.
func removeLiveNoteSegment(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("live notes: remove appended segment: %w", err)
	}
	return nil
}

// openLiveNoteTarget opens the channel file for appending, or creates it.
//
// A sidecar without a usable file means the two disagreed (a deleted recording,
// or a create that never landed). Rebuilding from this segment is the only
// answer that does not leave a permanently empty channel.
func openLiveNoteTarget(target string, fresh bool) (*os.File, error) {
	if fresh {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("live notes: create audio dir: %w", err)
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644) //nolint:gosec // G302: the recording is a user artifact, not a secret
		if err != nil {
			return nil, fmt.Errorf("live notes: create channel audio: %w", err)
		}
		return f, nil
	}
	f, err := os.OpenFile(target, os.O_RDWR, 0o644) //nolint:gosec // G302: the recording is a user artifact, not a secret
	if err != nil {
		return nil, fmt.Errorf("live notes: open channel audio: %w", err)
	}
	return f, nil
}

// loadLiveNoteAppendState reads the sidecar, treating "no sidecar" (or one whose
// recording file is gone) as "start a new file".
func loadLiveNoteAppendState(sidecar, target string) (*liveNoteAppendState, error) {
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	data, err := os.ReadFile(sidecar)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("live notes: read append state: %w", err)
	}
	var state liveNoteAppendState
	if err := json.Unmarshal(data, &state); err != nil {
		// A corrupt sidecar would otherwise wedge this channel forever; rebuild
		// from the next segment instead of failing every upload.
		return nil, nil //nolint:nilerr // intentional: a bad sidecar means "start over", not "fail the upload"
	}
	if state.BodyStart <= state.SizePos || state.SizePos < 0 {
		return nil, nil
	}
	return &state, nil
}

func saveLiveNoteAppendState(sidecar string, state *liveNoteAppendState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("live notes: encode append state: %w", err)
	}
	tmp := sidecar + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("live notes: write append state: %w", err)
	}
	if err := os.Rename(tmp, sidecar); err != nil {
		return fmt.Errorf("live notes: replace append state: %w", err)
	}
	return nil
}

// liveNoteAppendSidecarName keeps the bookkeeping next to the file it describes
// without ever matching a channel or segment name.
func liveNoteAppendSidecarName(channel string) string {
	return strings.TrimSuffix(liveNoteAudioFileName(channel), ".webm") + ".append.json"
}
