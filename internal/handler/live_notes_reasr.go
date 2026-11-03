package handler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawbench/internal/service"
)

// reasrSource is one channel file to recognize again, with the speaker label
// and absolute origin the transcript needs.
type reasrSource struct {
	Channel string
	Speaker string
	BaseAt  int64
	Path    string
}

// buildTranscriptASR re-runs speech recognition on the meeting recording.
//
// It replaces live_notes_segments only after every chunk succeeded, then renders
// the VTT from the new rows. A render failure leaves pending_asr_at set, so the
// next attempt renders instead of paying for the recognition again.
func (c *artifactChain) buildTranscriptASR(ctx context.Context) error {
	if !c.onDisk {
		return fmt.Errorf("%w: meeting folder unavailable", service.ErrLiveNoteAudioMissing)
	}
	// The merge is idempotent and also fills the audio-origin metadata. prepare
	// already ran it, but this step is also reachable from a chain started
	// before the folder existed.
	merged, mergeErr := service.MergeLiveNoteAudio(c.paths)
	if mergeErr != nil {
		return fmt.Errorf("%w: merge audio: %w", service.ErrLiveNoteAudioMissing, mergeErr)
	}
	recordMergedAudioChannels(c.token, merged)

	channels, err := service.ListLiveNoteAudioChannels(c.token)
	if err != nil {
		return err
	}
	sources, err := c.reasrSources(channels)
	if err != nil {
		return err
	}
	jobs, err := buildASRJobs(sources)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return service.ErrLiveNotesEmpty
	}

	texts, err := c.transcribeChunks(ctx, jobs)
	if err != nil {
		return err
	}

	newSegments := collectASRSegments(jobs, texts)
	if len(newSegments) == 0 {
		return service.ErrLiveNotesEmpty
	}
	if err := service.ReplaceLiveNoteSegments(c.token, c.projectID, newSegments); err != nil {
		return err
	}
	c.refreshSegments()
	return c.renderTranscriptAfterASR()
}

// asrChunkJob is one slice of one channel file, ready to be transcribed.
type asrChunkJob struct {
	source reasrSource
	chunk  service.LiveNoteAudioChunk
}

// buildASRJobs splits every source file into 20-second containers. Splitting is
// container surgery only — the audio payload is untouched.
func buildASRJobs(sources []reasrSource) ([]asrChunkJob, error) {
	jobs := make([]asrChunkJob, 0, 64)
	for _, src := range sources {
		chunks, err := service.SplitLiveNoteAudio(src.Path, service.LiveNoteAudioWindowMS)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", service.ErrLiveNoteAudioMissing, src.Channel, err)
		}
		for _, chunk := range chunks {
			jobs = append(jobs, asrChunkJob{source: src, chunk: chunk})
		}
	}
	return jobs, nil
}

// transcribeChunks runs the chunk queue through the STT worker pool. The first
// failure cancels the rest; the progress heartbeat is best effort because a
// missed beat only delays the interrupted sweep.
func (c *artifactChain) transcribeChunks(ctx context.Context, jobs []asrChunkJob) ([]string, error) {
	total := len(jobs)
	texts := make([]string, total)
	var done atomic.Int32
	var firstErr error
	var errMu sync.Mutex
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := make(chan int)
	var wg sync.WaitGroup
	for range liveNoteSTTConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if workCtx.Err() != nil {
					return
				}
				text, transcribeErr := transcribeLiveNoteSegment(workCtx, jobs[idx].chunk.WebM)
				if transcribeErr != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = transcribeErr
						cancel()
					}
					errMu.Unlock()
					return
				}
				texts[idx] = text
				n := int(done.Add(1))
				if hbErr := service.HeartbeatArtifact(
					c.token, service.ArtifactTranscript, fmt.Sprintf("%d/%d", n, total), 0,
				); hbErr != nil {
					slog.Warn("live notes: asr heartbeat failed", "err", hbErr)
				}
			}
		}()
	}

feed:
	for i := range jobs {
		select {
		case work <- i:
		case <-workCtx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return nil, fmt.Errorf("%w: %w", service.ErrLiveNoteSTT, firstErr)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return texts, nil
}

// collectASRSegments turns per-chunk text into rows, dropping silence.
func collectASRSegments(jobs []asrChunkJob, texts []string) []service.LiveNoteSegment {
	out := make([]service.LiveNoteSegment, 0, len(jobs))
	for i, job := range jobs {
		text := strings.TrimSpace(texts[i])
		if text == "" {
			continue
		}
		out = append(out, service.LiveNoteSegment{
			Channel:    job.source.Channel,
			Speaker:    job.source.Speaker,
			StartedAt:  job.source.BaseAt + job.chunk.OffsetMS,
			DurationMS: job.chunk.DurationMS,
			Text:       text,
		})
	}
	return out
}

// renderTranscriptAfterASR is the second half of the transcript step: the
// segments are already replaced, so this only writes the VTT and settles the
// artifact. A failure here keeps pending_asr_at, which is what makes the retry
// cheap.
func (c *artifactChain) renderTranscriptAfterASR() error {
	raw := service.BuildLiveNotesTranscript(c.segments)
	if strings.TrimSpace(raw) == "" {
		return service.ErrLiveNotesEmpty
	}
	c.raw = raw
	changed := true
	if c.onDisk {
		body := service.BuildLiveNotesVTT(c.segments, c.files, c.screens)
		if old, err := os.ReadFile(c.paths.Transcript); err == nil && string(old) == body {
			changed = false
		}
		if _, err := service.WriteLiveNotesRawTranscript(c.paths, c.segments, c.files, c.screens); err != nil {
			return fmt.Errorf("write transcript: %w", err)
		}
	}
	contentAt := int64(0)
	if changed && c.onDisk {
		contentAt = time.Now().UnixMilli()
	}
	return service.SetArtifactSucceeded(c.token, service.ArtifactTranscript, contentAt, 0)
}

// reasrSources decides which files to recognize and who is speaking in each.
//
// Per-channel files win; the mix is the fallback for a solo meeting, whose only
// channel was folded into meeting.webm by the merge. A channel that produced
// transcript text but has no file is an error: silently dropping that speaker
// would make the new transcript worse than the old one.
func (c *artifactChain) reasrSources(metadata []service.LiveNoteAudioChannel) ([]reasrSource, error) {
	metaByChannel := make(map[string]service.LiveNoteAudioChannel, len(metadata))
	for _, ch := range metadata {
		metaByChannel[ch.Channel] = ch
	}
	segmentChannels := liveNoteSegmentChannels(c.segments)
	meetingStart, err := c.meetingStart()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(c.paths.Audio)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", service.ErrLiveNoteAudioMissing, err)
	}
	perChannel := make([]reasrSource, 0, len(entries))
	haveChannel := make(map[string]bool, len(entries))
	haveMix := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".webm") {
			continue
		}
		slug, ok := service.LiveNoteAudioFileChannelSlug(entry.Name())
		if !ok {
			continue
		}
		if slug == service.LiveNoteMixChannel {
			haveMix = true
			continue
		}
		src := c.perChannelSource(entry.Name(), slug, segmentChannels, metaByChannel, meetingStart)
		haveChannel[src.Channel] = true
		perChannel = append(perChannel, src)
	}

	if len(perChannel) > 0 {
		for _, channel := range segmentChannels {
			if channel == "system" {
				continue
			}
			if !haveChannel[channel] {
				return nil, fmt.Errorf("%w: channel %s has no recording", service.ErrLiveNoteAudioMissing, channel)
			}
		}
		return perChannel, nil
	}

	if !haveMix {
		return nil, fmt.Errorf("%w: no channel files", service.ErrLiveNoteAudioMissing)
	}
	return []reasrSource{c.mixSource(metaByChannel, meetingStart)}, nil
}

// meetingStart is the meeting's wall-clock start, used as the last-resort audio
// origin when neither the merge metadata nor the old segments know it.
func (c *artifactChain) meetingStart() (int64, error) {
	note, ok, err := service.GetLiveNote(c.token)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return note.StartedAt, nil
}

// perChannelSource describes one per-channel file: who speaks in it and where
// its 0ms sits on the meeting timeline.
func (c *artifactChain) perChannelSource(
	fileName, slug string,
	segmentChannels []string,
	meta map[string]service.LiveNoteAudioChannel,
	meetingStart int64,
) reasrSource {
	channel := service.LiveNoteAudioChannelForSlug(slug, segmentChannels)
	if channel == "" {
		channel = slug
	}
	base := meta[channel].BaseAt
	if base <= 0 {
		base = liveNoteChannelBase(c.segments, channel)
	}
	if base <= 0 {
		base = meetingStart
	}
	speaker := liveNoteTopSpeaker(c.segments, channel)
	if speaker == "" {
		speaker = channel
	}
	return reasrSource{
		Channel: channel,
		Speaker: speaker,
		BaseAt:  base,
		Path:    filepath.Join(c.paths.Audio, fileName),
	}
}

// mixSource is the solo-meeting fallback: the only channel was folded into
// meeting.webm, so the speaker label comes from the old transcript as a whole.
func (c *artifactChain) mixSource(meta map[string]service.LiveNoteAudioChannel, meetingStart int64) reasrSource {
	base := meta[service.LiveNoteMixChannel].BaseAt
	if base <= 0 {
		base = liveNoteEarliestStart(c.segments)
	}
	if base <= 0 {
		base = meetingStart
	}
	speaker := liveNoteTopSpeaker(c.segments, "")
	if speaker == "" {
		speaker = service.LiveNoteUnknownSpeaker()
	}
	return reasrSource{
		Channel: service.LiveNoteMixChannel,
		Speaker: speaker,
		BaseAt:  base,
		Path:    filepath.Join(c.paths.Audio, service.LiveNoteMeetingAudioFile),
	}
}

// liveNoteSegmentChannels returns the distinct channels the old transcript used.
func liveNoteSegmentChannels(segments []service.LiveNoteSegment) []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	for _, seg := range segments {
		if seg.Channel == "" || seen[seg.Channel] {
			continue
		}
		seen[seg.Channel] = true
		out = append(out, seg.Channel)
	}
	return out
}

// liveNoteChannelBase is the absolute time of a channel's first transcribed
// segment. Used when the audio-origin metadata is missing (old meetings).
func liveNoteChannelBase(segments []service.LiveNoteSegment, channel string) int64 {
	var base int64
	for _, seg := range segments {
		if seg.Channel != channel || seg.StartedAt <= 0 {
			continue
		}
		if base == 0 || seg.StartedAt < base {
			base = seg.StartedAt
		}
	}
	return base
}

func liveNoteEarliestStart(segments []service.LiveNoteSegment) int64 {
	var earliest int64
	for _, seg := range segments {
		if seg.StartedAt <= 0 {
			continue
		}
		if earliest == 0 || seg.StartedAt < earliest {
			earliest = seg.StartedAt
		}
	}
	return earliest
}

// liveNoteTopSpeaker returns the most frequent non-empty speaker label for a
// channel ("" = across all channels). Ties go to the most recent one.
func liveNoteTopSpeaker(segments []service.LiveNoteSegment, channel string) string {
	counts := make(map[string]int, 4)
	latest := make(map[string]int64, 4)
	for _, seg := range segments {
		if channel != "" && seg.Channel != channel {
			continue
		}
		name := strings.TrimSpace(seg.Speaker)
		if name == "" {
			continue
		}
		counts[name]++
		if seg.StartedAt > latest[name] {
			latest[name] = seg.StartedAt
		}
	}
	best, bestCount, bestAt := "", 0, int64(0)
	for name, count := range counts {
		if count > bestCount || (count == bestCount && latest[name] > bestAt) {
			best, bestCount, bestAt = name, count, latest[name]
		}
	}
	return best
}

// recordMergedAudioChannels persists the origins the merge just computed,
// mapping the file-name slug back to the channel the transcript used.
func recordMergedAudioChannels(token string, merged service.LiveNoteAudioMerge) {
	if len(merged.Origins) == 0 {
		return
	}
	segments, err := service.ListLiveNoteSegments(token, 0)
	if err != nil {
		slog.Warn("live notes: list segments for audio metadata failed", "err", err)
		segments = nil
	}
	channels := liveNoteSegmentChannels(segments)
	out := make([]service.LiveNoteAudioChannel, 0, len(merged.Origins))
	for slug, base := range merged.Origins {
		channel := service.LiveNoteAudioChannelForSlug(slug, channels)
		if channel == "" {
			channel = slug
		}
		out = append(out, service.LiveNoteAudioChannel{
			Channel:    channel,
			File:       merged.Files[slug],
			BaseAt:     base,
			DurationMS: merged.Durations[slug],
		})
	}
	if err := service.RecordLiveNoteAudioChannels(token, out); err != nil {
		slog.Warn("live notes: record audio channels failed", "err", err)
	}
}
