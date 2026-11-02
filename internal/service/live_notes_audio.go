// WebM merging for meeting recordings.
//
// A meeting records one channel per participant, and every channel is cut into
// standalone segments so each one can be transcribed on its own. That leaves
// dozens of files in `audio/` — one per ten to twenty seconds of speech — which
// is not what the user wants to find when they open a meeting afterwards.
//
// Merging is a container rewrite, not a transcode. Every segment of one channel
// comes from the same encoder configuration (same sample rate, same Opus
// parameters), so copying its clusters into a single Segment reproduces the
// original audio byte for byte. The only thing that has to change is the
// timeline: each segment restarts its clock at zero, so every cluster's
// timecode is shifted by the segment's offset from the meeting start.
//
// That shift is also what preserves the silences. A channel that was quiet for
// two minutes gets a two-minute gap between timecodes instead of two minutes of
// padded silence — no encoder involved, so there is nothing to pad with.
package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The two names the merge produces beyond the per-channel files.
const (
	// LiveNoteMeetingAudioFile is the mixed track of the whole meeting: every
	// participant plus whatever the publisher's mix bus carried.
	LiveNoteMeetingAudioFile = "meeting.webm"
	// LiveNoteMixChannel is the synthetic channel recorded from the publisher's
	// Web Audio mix bus. It is archive-only — never transcribed, because it has
	// no single speaker — and it is what the whole-meeting file is built from.
	LiveNoteMixChannel = "mix"
)

// liveNoteAudioSegmentMaxBytes bounds one segment read while merging. Segments
// are capped at 4 MiB on upload; anything past this is not a file this package
// wrote.
const liveNoteAudioSegmentMaxBytes = 32 << 20

// liveNoteAudioSegmentPattern matches the names SaveLiveNoteAudio writes:
// `<unix-ms>-<channel-slug>.webm`. The merged outputs (`mic.webm`,
// `meeting.webm`) deliberately do not match, which is what makes a second merge
// pass a no-op.
var liveNoteAudioSegmentPattern = regexp.MustCompile(`^(\d{13})-(.+)\.webm$`)

// LiveNoteAudioMerge reports what one merge pass left behind.
type LiveNoteAudioMerge struct {
	// Meeting is the whole-meeting file ("" when the recording has no mix track,
	// which is every meeting recorded before the mix channel existed).
	Meeting string
	// Channels are the per-channel files, in channel order.
	Channels []string
	// Segments is how many segment files were folded in and removed.
	Segments int
}

// audioSegment is one uploaded segment file awaiting the merge.
type audioSegment struct {
	path      string
	channel   string
	startedAt int64
}

// MergeLiveNoteAudio folds every segment of a meeting into one file per
// channel, plus the whole-meeting mix, and removes the segments afterwards.
//
// It is idempotent: a group whose output already exists is left alone, so a
// retry after a partial run cannot produce a file containing only the segments
// that survived.
func MergeLiveNoteAudio(paths LiveNotesPaths) (LiveNoteAudioMerge, error) {
	var out LiveNoteAudioMerge
	entries, err := os.ReadDir(paths.Audio)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("live notes: list audio: %w", err)
	}

	groups := make(map[string][]audioSegment, 4)
	done := make(map[string]string, 4)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if seg, ok := parseLiveNoteAudioSegmentName(name); ok {
			seg.path = filepath.Join(paths.Audio, name)
			groups[seg.channel] = append(groups[seg.channel], seg)
			continue
		}
		if ch, ok := liveNoteAudioChannelOfFile(name); ok {
			done[ch] = filepath.Join(paths.Audio, name)
		}
	}

	channels := make([]string, 0, len(groups)+len(done))
	for ch := range groups {
		channels = append(channels, ch)
	}
	for ch := range done {
		if _, ok := groups[ch]; !ok {
			channels = append(channels, ch)
		}
	}
	sort.Strings(channels)

	// A meeting with one participant records the same audio twice: the mix bus
	// and that participant's channel. Merging both writes two identical files,
	// which is pure work and disk for a meeting that is listened to as one file.
	// The mix is kept (it is what the detail page plays) and the duplicate's
	// segments are dropped once the mix is safely on disk.
	solo := liveNoteSoloChannel(channels, groups, done)

	for _, ch := range channels {
		if ch == solo {
			continue
		}
		// An existing output means a previous pass already folded this channel:
		// rewriting it would drop whatever that pass had that this one does not
		// (a retry sees fewer segments, never more).
		if target, ok := done[ch]; ok {
			out.record(ch, target, 0)
			continue
		}
		segs := groups[ch]
		sort.Slice(segs, func(i, j int) bool {
			if segs[i].startedAt != segs[j].startedAt {
				return segs[i].startedAt < segs[j].startedAt
			}
			return segs[i].path < segs[j].path
		})
		target := filepath.Join(paths.Audio, liveNoteAudioFileName(ch))
		merged, err := mergeLiveNoteAudioGroup(paths.Audio, target, segs)
		if err != nil {
			return out, err
		}
		out.record(ch, target, merged)
	}
	// Only after the mix landed: a failed mix merge returns above and leaves the
	// channel's segments as the fallback recording.
	if solo != "" && out.Meeting != "" {
		for _, seg := range groups[solo] {
			if err := os.Remove(seg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return out, fmt.Errorf("live notes: remove duplicate segment: %w", err)
			}
		}
		out.Segments += len(groups[solo])
	}
	return out, nil
}

// liveNoteSoloChannel returns the single participant channel that duplicates the
// mix, or "" when the meeting had more than one participant.
//
// The conditions are all necessary: exactly two groups (the mix and one channel),
// the mix actually present (older recordings have no mix at all), and no channel
// file from an earlier pass (finished work is never deleted).
func liveNoteSoloChannel(channels []string, groups map[string][]audioSegment, done map[string]string) string {
	if len(channels) != 2 {
		return ""
	}
	solo := ""
	for _, ch := range channels {
		if ch != LiveNoteMixChannel {
			solo = ch
		}
	}
	if solo == "" || done[solo] != "" {
		return ""
	}
	if len(groups[LiveNoteMixChannel]) == 0 && done[LiveNoteMixChannel] == "" {
		return ""
	}
	return solo
}

// record files one group's outcome under the right field.
func (m *LiveNoteAudioMerge) record(channel, target string, segments int) {
	if channel == LiveNoteMixChannel {
		m.Meeting = target
	} else {
		m.Channels = append(m.Channels, target)
	}
	m.Segments += segments
}

// liveNoteAudioFileName maps a channel slug to the file the merge produces.
func liveNoteAudioFileName(channel string) string {
	if channel == LiveNoteMixChannel {
		return LiveNoteMeetingAudioFile
	}
	return channel + ".webm"
}

// liveNoteAudioChannelOfFile is the inverse of liveNoteAudioFileName: it says
// which channel an already-merged file belongs to, so a second merge pass can
// still report what is on disk.
func liveNoteAudioChannelOfFile(name string) (string, bool) {
	if name == LiveNoteMeetingAudioFile {
		return LiveNoteMixChannel, true
	}
	slug, ok := strings.CutSuffix(name, ".webm")
	if !ok || slug == "" {
		return "", false
	}
	return slug, true
}

// parseLiveNoteAudioSegmentName splits `<unix-ms>-<channel>.webm`.
//
// The channel comes back as the file-name slug, not as the original channel
// name: `viewer:2` was written as `viewer-2` and cannot be reversed. The slug is
// all the merge needs — it is what the output file is named after.
func parseLiveNoteAudioSegmentName(name string) (audioSegment, bool) {
	m := liveNoteAudioSegmentPattern.FindStringSubmatch(name)
	if m == nil {
		return audioSegment{}, false
	}
	ms, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || ms <= 0 {
		return audioSegment{}, false
	}
	return audioSegment{path: name, channel: m[2], startedAt: ms}, true
}

// mergeLiveNoteAudioGroup writes one channel's file and removes its segments.
//
// A single segment is already a complete WebM, so it is renamed rather than
// rewritten — the common case of a channel that spoke once must not pay for a
// remux.
func mergeLiveNoteAudioGroup(dir, target string, segs []audioSegment) (int, error) {
	if len(segs) == 1 {
		// A single segment is renamed rather than remuxed, so nothing else in
		// this path would ever look at its contents. It still has to be a WebM:
		// the client silently falls back to whatever MediaRecorder supports
		// (Safari records audio/mp4), and publishing that as `mic.webm` would
		// hide the problem until somebody tries to play the recording.
		if err := validateLiveNoteWebMContainer(segs[0].path); err != nil {
			return 0, err
		}
		if err := os.Rename(segs[0].path, target); err != nil {
			return 0, fmt.Errorf("live notes: rename single segment: %w", err)
		}
		return 0, nil
	}

	tmp, err := os.CreateTemp(dir, ".merge-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("live notes: create merge temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := remuxLiveNoteAudio(tmp, segs); err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return 0, fmt.Errorf("live notes: sync merged audio: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return 0, fmt.Errorf("live notes: close merged audio: %w", err)
	}
	// CreateTemp is 0600; the segments it replaces were 0644, and the user opens
	// these files outside the app.
	if err := os.Chmod(tmpName, 0o644); err != nil { //nolint:gosec // G302: recording is a user artifact, not a secret
		_ = os.Remove(tmpName)
		return 0, fmt.Errorf("live notes: chmod merged audio: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return 0, fmt.Errorf("live notes: publish merged audio: %w", err)
	}
	// Only now may the segments go: the merged file is in place, so a failure
	// here leaves extra files rather than a hole in the recording.
	for _, seg := range segs {
		if err := os.Remove(seg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return len(segs), fmt.Errorf("live notes: remove merged segment: %w", err)
		}
	}
	return len(segs), nil
}

// remuxLiveNoteAudio writes one WebM holding every segment's clusters.
func remuxLiveNoteAudio(f *os.File, segs []audioSegment) error {
	first, err := readLiveNoteWebM(segs[0].path)
	if err != nil {
		return err
	}
	if len(first.header) == 0 || len(first.tracks) == 0 {
		// Without the Tracks element the result would be a file no decoder can
		// open; say which file is at fault instead of writing it.
		return fmt.Errorf("live notes: %s is not a self-contained WebM segment", filepath.Base(segs[0].path))
	}

	spans, err := writeLiveNoteHeader(f, first)
	if err != nil {
		return err
	}
	totalMS, err := writeLiveNoteClusters(f, first, segs)
	if err != nil {
		return err
	}
	end, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("live notes: tell merged length: %w", err)
	}
	return patchLiveNoteHeader(f, spans, end-spans.bodyStart, totalMS)
}

// liveNoteHeaderSpans are the two sizes the header reserves and patches later.
type liveNoteHeaderSpans struct {
	sizePos     int64
	bodyStart   int64
	durationPos int64
}

// writeLiveNoteHeader writes the EBML header, an empty Segment and the first
// segment's Info/Tracks, leaving the two sizes it cannot know yet as
// placeholders.
func writeLiveNoteHeader(f *os.File, first *liveNoteWebM) (liveNoteHeaderSpans, error) {
	var spans liveNoteHeaderSpans
	start, seekErr := f.Seek(0, io.SeekCurrent)
	if seekErr != nil {
		return spans, fmt.Errorf("live notes: tell header offset: %w", seekErr)
	}
	written := start
	track := func(p []byte) error {
		if _, werr := f.Write(p); werr != nil {
			return werr
		}
		written += int64(len(p))
		return nil
	}

	if err := track(first.header); err != nil {
		return spans, fmt.Errorf("live notes: write header: %w", err)
	}
	if err := track(encodeEBMLID(ebmlIDSegment)); err != nil {
		return spans, fmt.Errorf("live notes: write segment: %w", err)
	}
	// The Segment size is only known once everything is written; reserve a
	// fixed-width field and patch it at the end.
	spans.sizePos = written
	if err := track(encodeEBMLSizeFixed(0, 8)); err != nil {
		return spans, fmt.Errorf("live notes: write segment size: %w", err)
	}
	spans.bodyStart = written
	// The Duration is only known once every cluster is in place; the element is
	// written as an 8-byte zero placeholder and patched at the end. It has to be
	// there: without one a player reports an unknown length for the whole file
	// and the seek bar of a one-hour meeting stops working.
	info, err := liveNoteInfoWithDuration(first.info)
	if err != nil {
		return spans, fmt.Errorf("live notes: rewrite info: %w", err)
	}
	if err := track(info); err != nil {
		return spans, fmt.Errorf("live notes: write info: %w", err)
	}
	spans.durationPos = written - 8
	if err := track(first.tracks); err != nil {
		return spans, fmt.Errorf("live notes: write tracks: %w", err)
	}
	return spans, nil
}

// patchLiveNoteHeader fills in the two placeholders once the length is known.
// `segmentSize` is the number of bytes that follow the Segment size field.
func patchLiveNoteHeader(f *os.File, spans liveNoteHeaderSpans, segmentSize int64, totalMS float64) error {
	if _, err := f.Seek(spans.sizePos, io.SeekStart); err != nil {
		return fmt.Errorf("live notes: seek segment size: %w", err)
	}
	if _, err := f.Write(encodeEBMLSizeFixed(int(segmentSize), 8)); err != nil {
		return fmt.Errorf("live notes: patch segment size: %w", err)
	}
	if _, err := f.Seek(spans.durationPos, io.SeekStart); err != nil {
		return fmt.Errorf("live notes: seek duration: %w", err)
	}
	var duration [8]byte
	binary.BigEndian.PutUint64(duration[:], math.Float64bits(totalMS))
	if _, err := f.Write(duration[:]); err != nil {
		return fmt.Errorf("live notes: patch duration: %w", err)
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("live notes: restore write position: %w", err)
	}
	return nil
}

// writeLiveNoteClusters copies every segment's clusters onto the shared
// timeline and reports how long the result is, in milliseconds.
func writeLiveNoteClusters(f *os.File, first *liveNoteWebM, segs []audioSegment) (float64, error) {
	base := segs[0].startedAt
	var totalMS float64
	for i, seg := range segs {
		src := first
		if i > 0 {
			var err error
			src, err = readLiveNoteWebM(seg.path)
			if err != nil {
				return 0, err
			}
		}
		// Sorted ascending, so the difference is never negative.
		shift := uint64(seg.startedAt - base) //nolint:gosec // G115: sorted timestamps, never negative
		if end := float64(shift) + liveNoteSegmentEndMS(src); end > totalMS {
			totalMS = end
		}
		for _, cluster := range src.clusters {
			shifted, err := shiftLiveNoteCluster(cluster, shift)
			if err != nil {
				return 0, fmt.Errorf("live notes: %s: %w", filepath.Base(seg.path), err)
			}
			if _, err := f.Write(shifted); err != nil {
				return 0, fmt.Errorf("live notes: write cluster: %w", err)
			}
		}
	}
	return totalMS, nil
}

// liveNoteWebM is the parts of one segment file the merge needs.
type liveNoteWebM struct {
	header   []byte
	info     []byte
	tracks   []byte
	clusters [][]byte
	// durationMS is the segment's own Duration, in milliseconds (0 when the
	// element is missing).
	durationMS float64
}

// validateLiveNoteWebMContainer checks the four-byte EBML magic at the head of
// a file.
//
// This is the cheap gate for the single-segment path, which renames instead of
// remuxing and therefore never parses the file: without it a recording in
// another container (MediaRecorder falls back to audio/mp4 on Safari) is
// published under a .webm name and only fails later, when somebody plays it.
func validateLiveNoteWebMContainer(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("live notes: open segment: %w", err)
	}
	defer func() { _ = f.Close() }()
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("live notes: %s is not a WebM recording (no EBML header)", filepath.Base(path))
	}
	if binary.BigEndian.Uint32(head[:]) != ebmlIDHeader {
		return fmt.Errorf("live notes: %s is not a WebM recording (no EBML header)", filepath.Base(path))
	}
	return nil
}

// readLiveNoteWebM parses one segment file.
//
// Segments are small (tens of KiB), so the whole file is read and walked in
// memory. An element with an unknown size is rejected rather than guessed at:
// this package wrote every file it reads, and a size it cannot parse means the
// file is not one of ours.
func readLiveNoteWebM(path string) (*liveNoteWebM, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("live notes: stat segment: %w", err)
	}
	if info.Size() > liveNoteAudioSegmentMaxBytes {
		return nil, fmt.Errorf("live notes: %s is larger than one segment can be", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("live notes: read segment: %w", err)
	}

	out := &liveNoteWebM{}
	sawSegment := false
	for pos := 0; pos < len(data); {
		el, err := decodeLiveNoteElement(data[pos:])
		if err != nil {
			return nil, fmt.Errorf("live notes: %s: %w", filepath.Base(path), err)
		}
		end := pos + el.header + el.size
		switch el.id {
		case ebmlIDHeader:
			out.header = data[pos:end]
		case ebmlIDSegment:
			sawSegment = true
			if err := readLiveNoteSegmentChildren(data[pos+el.header:end], out); err != nil {
				return nil, fmt.Errorf("live notes: %s: %w", filepath.Base(path), err)
			}
		}
		pos = end
	}
	if !sawSegment {
		return nil, fmt.Errorf("live notes: %s: no Segment element", filepath.Base(path))
	}
	return out, nil
}

// readLiveNoteSegmentChildren keeps the first Info/Tracks and every Cluster.
func readLiveNoteSegmentChildren(b []byte, out *liveNoteWebM) error {
	for pos := 0; pos < len(b); {
		el, err := decodeLiveNoteElement(b[pos:])
		if err != nil {
			return err
		}
		end := pos + el.header + el.size
		switch el.id {
		case ebmlIDInfo:
			if out.info == nil {
				element := b[pos:end]
				stripped, stripErr := stripLiveNoteDuration(element)
				if stripErr != nil {
					return stripErr
				}
				out.info = stripped
				out.durationMS = liveNoteInfoDuration(element)
			}
		case ebmlIDTracks:
			if out.tracks == nil {
				out.tracks = b[pos:end]
			}
		case ebmlIDCluster:
			out.clusters = append(out.clusters, b[pos:end])
		}
		pos = end
	}
	return nil
}

// liveNoteInfoDuration reads the Duration element of an Info element, in
// milliseconds. MediaRecorder writes it as a float; a file without one is not
// an error — the merge falls back to the last cluster timecode.
func liveNoteInfoDuration(info []byte) float64 {
	el, err := decodeLiveNoteElement(info)
	if err != nil {
		return 0
	}
	payload := info[el.header : el.header+el.size]
	for pos := 0; pos < len(payload); {
		child, err := decodeLiveNoteElement(payload[pos:])
		if err != nil {
			return 0
		}
		start := pos + child.header
		end := start + child.size
		if child.id == ebmlIDDuration {
			switch child.size {
			case 4:
				return float64(math.Float32frombits(binary.BigEndian.Uint32(payload[start:end])))
			case 8:
				return math.Float64frombits(binary.BigEndian.Uint64(payload[start:end]))
			}
			return 0
		}
		pos = end
	}
	return 0
}

// stripLiveNoteDuration drops the Duration element from an Info element.
//
// The first segment's Duration describes that segment, not the meeting; keeping
// it would make every player report a few seconds for a one-hour recording.
func stripLiveNoteDuration(element []byte) ([]byte, error) {
	el, err := decodeLiveNoteElement(element)
	if err != nil {
		return nil, err
	}
	payload := element[el.header : el.header+el.size]

	body := make([]byte, 0, len(payload))
	for pos := 0; pos < len(payload); {
		child, err := decodeLiveNoteElement(payload[pos:])
		if err != nil {
			return nil, err
		}
		end := pos + child.header + child.size
		if child.id != ebmlIDDuration {
			body = append(body, payload[pos:end]...)
		}
		pos = end
	}
	return encodeLiveNoteElement(element[:el.idLen], body), nil
}

// liveNoteInfoWithDuration rebuilds an Info element with an 8-byte Duration
// placeholder appended to its children. The caller patches the placeholder once
// the total length is known.
func liveNoteInfoWithDuration(info []byte) ([]byte, error) {
	el, err := decodeLiveNoteElement(info)
	if err != nil {
		return nil, err
	}
	payload := info[el.header : el.header+el.size]
	body := make([]byte, 0, len(payload)+11)
	body = append(body, payload...)
	body = append(body, encodeLiveNoteElement(encodeEBMLID(ebmlIDDuration), make([]byte, 8))...)
	return encodeLiveNoteElement(info[:el.idLen], body), nil
}

// shiftLiveNoteCluster moves one cluster onto the meeting timeline.
//
// Only the cluster's own Timecode changes: the blocks inside are relative to it,
// so their offsets stay correct and no audio data is touched.
func shiftLiveNoteCluster(cluster []byte, shiftMS uint64) ([]byte, error) {
	el, err := decodeLiveNoteElement(cluster)
	if err != nil {
		return nil, err
	}
	if el.id != ebmlIDCluster {
		return nil, fmt.Errorf("expected a Cluster, found %#x", el.id)
	}
	payload := cluster[el.header : el.header+el.size]

	timecode, found := uint64(0), false
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
			timecode, found = decodeLiveNoteUint(payload[start:end]), true
			pos = end
			continue
		}
		body = append(body, payload[pos:end]...)
		pos = end
	}
	if !found {
		// Every Cluster must carry a Timecode; without it there is nothing to
		// shift and the result would silently play at the wrong time.
		return nil, errors.New("cluster has no Timecode element")
	}

	inner := append(encodeLiveNoteElement(encodeEBMLID(ebmlIDTimecode), encodeLiveNoteUint(timecode+shiftMS)), body...)
	out := append([]byte{}, cluster[:el.idLen]...)
	out = append(out, encodeEBMLSize(len(inner))...)
	return append(out, inner...), nil
}

// liveNoteSegmentEndMS is how far into its own timeline a segment reaches.
//
// The Duration element is authoritative; the last cluster's timecode is the
// fallback for a file that lost it, and is slightly short (a cluster's own
// blocks can run past its timecode).
func liveNoteSegmentEndMS(file *liveNoteWebM) float64 {
	if file.durationMS > 0 {
		return file.durationMS
	}
	if len(file.clusters) == 0 {
		return 0
	}
	return float64(liveNoteClusterTimecode(file.clusters[len(file.clusters)-1]))
}

// liveNoteClusterTimecode returns the Timecode of one Cluster element (0 when
// the element is malformed; the caller only uses it as an estimate).
func liveNoteClusterTimecode(cluster []byte) uint64 {
	el, err := decodeLiveNoteElement(cluster)
	if err != nil {
		return 0
	}
	payload := cluster[el.header : el.header+el.size]
	for pos := 0; pos < len(payload); {
		child, err := decodeLiveNoteElement(payload[pos:])
		if err != nil {
			return 0
		}
		start := pos + child.header
		end := start + child.size
		if child.id == ebmlIDTimecode {
			return decodeLiveNoteUint(payload[start:end])
		}
		pos = end
	}
	return 0
}

// ── EBML primitives ─────────────────────────────────────────────────────────

// Element IDs, with their marker bits, exactly as they appear on the wire.
const (
	ebmlIDHeader        = 0x1A45DFA3
	ebmlIDSegment       = 0x18538067
	ebmlIDInfo          = 0x1549A966
	ebmlIDTracks        = 0x1654AE6B
	ebmlIDCluster       = 0x1F43B675
	ebmlIDTimecode      = 0xE7
	ebmlIDDuration      = 0x4489
	ebmlIDTimecodeScale = 0x2AD7B1
	ebmlIDMuxingApp     = 0x4D80
	ebmlIDSimpleBlock   = 0xA3
)

// ebmlElement is one parsed element header. The payload is the `size` bytes
// that follow the `header` bytes.
type ebmlElement struct {
	id     uint32
	idLen  int
	size   int
	header int
}

// decodeLiveNoteElement reads one element header from the front of b.
//
// The size is validated against the remaining bytes here, so callers can slice
// without a bounds check of their own: an element that claims more bytes than
// the file has is rejected rather than read past its end. Sizes are int because
// every size this package handles is a file offset; a value that does not fit
// (or a live muxer's all-ones "unknown") is refused.
func decodeLiveNoteElement(b []byte) (ebmlElement, error) {
	id, idLen, err := readLiveNoteID(b)
	if err != nil {
		return ebmlElement{}, err
	}
	size, sizeLen, unknown, err := readLiveNoteSize(b[idLen:])
	if err != nil {
		return ebmlElement{}, err
	}
	if unknown {
		// All-ones means "unknown size" for a live muxer. Nothing this package
		// writes uses it, so it is a corrupt file rather than a case to guess at.
		return ebmlElement{}, fmt.Errorf("element %#x has an unknown size", id)
	}
	if available := len(b) - idLen - sizeLen; size < 0 || size > available {
		return ebmlElement{}, fmt.Errorf("element %#x claims %d bytes, %d available", id, size, available)
	}
	return ebmlElement{id: id, idLen: idLen, size: size, header: idLen + sizeLen}, nil
}

// readLiveNoteID decodes an element ID, marker bits included.
func readLiveNoteID(b []byte) (uint32, int, error) {
	if len(b) == 0 {
		return 0, 0, errors.New("empty element")
	}
	n := 1
	for mask := byte(0x80); mask > 0 && b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 4 || len(b) < n {
		return 0, 0, fmt.Errorf("bad element id %#x", b[0])
	}
	var id uint32
	for i := range n {
		id = id<<8 | uint32(b[i])
	}
	return id, n, nil
}

// readLiveNoteSize decodes a variable-length element size. `unknown` marks the
// all-ones value a live muxer writes for a stream it cannot measure yet.
func readLiveNoteSize(b []byte) (size int, n int, unknown bool, err error) {
	if len(b) == 0 {
		return 0, 0, false, errors.New("missing element size")
	}
	n = 1
	for mask := byte(0x80); mask > 0 && b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(b) < n {
		return 0, 0, false, fmt.Errorf("bad element size %#x", b[0])
	}
	size = int(b[0] & (0xFF >> uint(n)))
	for i := 1; i < n; i++ {
		size = size<<8 | int(b[i])
	}
	if size == (1<<(7*uint(n)))-1 {
		return 0, n, true, nil
	}
	return size, n, false, nil
}

// encodeLiveNoteElement rebuilds an element from its raw ID bytes and payload.
func encodeLiveNoteElement(id []byte, payload []byte) []byte {
	out := make([]byte, 0, len(id)+8+len(payload))
	out = append(out, id...)
	out = append(out, encodeEBMLSize(len(payload))...)
	return append(out, payload...)
}

// encodeEBMLID returns the wire form of a multi-byte element ID.
func encodeEBMLID(id uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], id)
	switch {
	case id <= 0xFF:
		return buf[3:]
	case id <= 0xFFFF:
		return buf[2:]
	case id <= 0xFFFFFF:
		return buf[1:]
	default:
		return buf[:]
	}
}

// encodeEBMLSize writes the shortest variable-length size that fits.
func encodeEBMLSize(size int) []byte {
	n := 1
	for n < 8 && size >= (1<<(7*uint(n)))-1 {
		n++
	}
	return encodeEBMLSizeFixed(size, n)
}

// encodeEBMLSizeFixed writes a size in exactly n bytes, zero padded.
//
// EBML allows a value to be encoded in more bytes than it needs, which is what
// makes patching a size field in place possible.
func encodeEBMLSizeFixed(size int, n int) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(size)) //nolint:gosec // G115: a segment size is never negative
	out := make([]byte, n)
	copy(out, buf[8-n:])
	out[0] |= 0x80 >> uint(n-1)
	return out
}

// encodeLiveNoteUint writes an unsigned EBML integer (minimal, at least 1 byte).
func encodeLiveNoteUint(v uint64) []byte {
	n := 1
	for n < 8 && v >= uint64(1)<<(8*uint(n)) {
		n++
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	out := make([]byte, n)
	copy(out, buf[8-n:])
	return out
}

// decodeLiveNoteUint reads a big-endian unsigned EBML integer of up to 8 bytes.
func decodeLiveNoteUint(b []byte) uint64 {
	if len(b) > 8 {
		return 0
	}
	var buf [8]byte
	copy(buf[8-len(b):], b)
	return binary.BigEndian.Uint64(buf[:])
}
