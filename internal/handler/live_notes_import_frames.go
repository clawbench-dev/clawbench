package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoder for the frames ffmpeg writes
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/service"
)

const (
	// liveNoteImportFrameMax bounds how many frames an import keeps. The live
	// recorder stops at 80 by hand; an automatic pass over a two-hour video
	// would otherwise pick hundreds.
	liveNoteImportFrameMax = 60
	// liveNoteImportFrameMinGapMS is the smallest distance between two kept
	// frames. Scene detection fires on every slide transition; without a gap a
	// screen recording turns into a flip book.
	liveNoteImportFrameMinGapMS = 15_000
	// liveNoteImportSceneThreshold is ffmpeg's scene-change score (0–1) above
	// which a frame counts as "the picture changed". 0.3 catches a slide or a
	// window switch without reacting to a cursor moving.
	liveNoteImportSceneThreshold = "0.3"
	// liveNoteImportFrameMaxWidth keeps a frame readable while capping its size.
	// A 4K desktop frame is otherwise several hundred kilobytes.
	liveNoteImportFrameMaxWidth = 1280
)

// importFrame is one kept frame, offset from the meeting's start.
type importFrame struct {
	OffsetMS int64
	Path     string
}

// extractImportFrames picks the frames where the picture changes.
//
// Scene detection runs inside ffmpeg: it decodes the video once, emits only the
// frames above the threshold, and prints each one's timestamp to stdout so the
// files can be paired with their position on the meeting timeline. A file with
// no video track (an audio-only import) has no frames and is not an error.
func extractImportFrames(ctx context.Context, video, dir string, limit int) ([]importFrame, error) {
	if limit <= 0 {
		return nil, nil
	}
	bin, err := lookPath("ffmpeg")
	if err != nil {
		return nil, ErrLiveNoteImportUnavailable
	}
	if !hasVideoStream(ctx, video) {
		return nil, nil
	}

	// The opening frame is always kept: a talking-head recording has no scene
	// changes at all, and "no pictures" would leave the transcript without the
	// one frame that says what the meeting looked like. The commas inside the
	// filter expressions are escaped because ffmpeg's filter parser uses them
	// as chain separators.
	filter := fmt.Sprintf(
		`select='eq(n\,0)+gt(scene\,%s)',metadata=print:file=-,scale='min(%d,iw)':-2`,
		liveNoteImportSceneThreshold, liveNoteImportFrameMaxWidth,
	)
	cmdOut, stderr, runErr := runMediaCommand(ctx, bin,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", video,
		"-vf", filter,
		"-vsync", "vfr", "-q:v", "4",
		filepath.Join(dir, "frame-%04d.jpg"),
	)
	if runErr != nil {
		return nil, fmt.Errorf("%w: %s", service.ErrLiveNoteImport, lastLogLine(string(stderr), runErr))
	}

	times := parseSceneTimes(string(cmdOut))
	files, err := filepath.Glob(filepath.Join(dir, "frame-*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("import: list frames: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, nil
	}
	// The pairing is positional — ffmpeg writes one file per frame it selects,
	// and prints one line per frame it passes through. A mismatch means the
	// filter chain changed shape, and guessing would attach wrong timestamps.
	if len(times) != len(files) {
		return nil, fmt.Errorf("%w: %d frames but %d timestamps", service.ErrLiveNoteImport, len(files), len(times))
	}
	picked := pickSceneFrames(times, limit)
	out := make([]importFrame, 0, len(picked))
	for _, i := range picked {
		out = append(out, importFrame{OffsetMS: times[i], Path: files[i]})
	}
	return out, nil
}

// hasVideoStream reports whether the file carries a picture at all.
//
// Checked up front so an audio-only import skips frame extraction instead of
// surfacing ffmpeg's "does not contain any stream" as an error.
func hasVideoStream(ctx context.Context, path string) bool {
	bin, err := lookPath("ffprobe")
	if err != nil {
		// Without ffprobe we cannot tell; let ffmpeg decide and treat its
		// failure as "no frames".
		return true
	}
	out, _, err := runMediaCommand(ctx, bin,
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", path,
	)
	if err != nil {
		return true
	}
	return strings.Contains(string(out), "video")
}

// parseSceneTimes reads the timestamps `metadata=print` wrote to stdout.
//
// Lines look like `frame:0    pts:61440   pts_time:6`; the second line of each
// pair carries the scene score and is ignored.
func parseSceneTimes(raw string) []int64 {
	out := make([]int64, 0, 32)
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "frame:") {
			continue
		}
		index := strings.Index(line, "pts_time:")
		if index < 0 {
			continue
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(line[index+len("pts_time:"):]), 64)
		if err != nil || seconds < 0 || math.IsInf(seconds, 0) {
			continue
		}
		out = append(out, int64(math.Round(seconds*1000)))
	}
	return out
}

// pickSceneFrames thins the detected frames down to what the meeting keeps.
//
// Two rules, in order: frames closer than liveNoteImportFrameMinGapMS to the
// previous kept one are dropped (a slide deck would otherwise become a flip
// book), and if more than limit survive they are spread EVENLY over the whole
// recording. Taking the first N instead would fill a two-hour meeting with
// pictures of its first twenty minutes.
func pickSceneFrames(times []int64, limit int) []int {
	if limit <= 0 {
		return nil
	}
	kept := make([]int, 0, min(limit, len(times)))
	var last int64
	for i, at := range times {
		if len(kept) > 0 && at-last < liveNoteImportFrameMinGapMS {
			continue
		}
		kept = append(kept, i)
		last = at
	}
	if len(kept) <= limit {
		return kept
	}
	if limit == 1 {
		return kept[:1]
	}
	out := make([]int, 0, limit)
	for i := range limit {
		index := int(math.Round(float64(i) * float64(len(kept)-1) / float64(limit-1)))
		if len(out) > 0 && out[len(out)-1] == kept[index] {
			continue
		}
		out = append(out, kept[index])
	}
	return out
}

// saveImportFrames writes the kept frames as the meeting's key frames.
//
// Each one is stored with the `change` reason — that IS what triggered it, and
// the detail view already labels that reason 画面变化. Failures are per frame:
// one unreadable image must not cost the rest.
func saveImportFrames(
	ctx context.Context,
	token string,
	projectID int64,
	paths service.LiveNotesPaths,
	startedAt int64,
	frames []importFrame,
) {
	for i, frame := range frames {
		data, err := os.ReadFile(frame.Path)
		if err != nil {
			slog.Warn("live notes: read extracted frame failed", "err", err)
			continue
		}
		at := startedAt + frame.OffsetMS
		name, err := service.SaveLiveNoteShot(paths, at, service.LiveNoteShotReasonChange, "jpg", data)
		if err != nil {
			slog.Warn("live notes: save extracted frame failed", "err", err)
			continue
		}
		width, height := imageSize(data)
		sum := sha256.Sum256(data)
		_, err = service.AppendLiveNoteShot(ctx, token, projectID, service.LiveNoteShot{
			At:       at,
			Reason:   service.LiveNoteShotReasonChange,
			FileName: name,
			Width:    width,
			Height:   height,
			Bytes:    int64(len(data)),
		}, fmt.Sprintf("import-%d", i), hex.EncodeToString(sum[:8]))
		if err != nil {
			slog.Warn("live notes: record extracted frame failed", "err", err)
			_ = service.DeleteLiveNoteShotFile(paths, name)
		}
	}
}

// imageSize reads an image's pixel size. Zero values are fine: the field is a
// hint for the aspect ratio while the picture loads.
func imageSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// storeImportSource moves the uploaded file into the meeting folder.
//
// The recording stays a resource of the meeting — it is what the user dragged
// in, and the converted audio is a derivative, not a replacement. Moving (not
// copying) keeps a two-hour video from occupying the disk twice; the upload is
// staged in the OS temp dir, which may sit on another filesystem, so a copy is
// the fallback.
func storeImportSource(dir, stagedPath, fileName string) (string, error) {
	target := filepath.Join(dir, liveNoteImportSourceName(fileName))
	if err := os.Rename(stagedPath, target); err == nil {
		return target, nil
	}
	if err := copyFile(stagedPath, target); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("import: keep recording: %w", err)
	}
	_ = os.Remove(stagedPath)
	return target, nil
}

// liveNoteImportSourceName makes the client's file name safe to use as a name
// inside the meeting folder.
//
// It is the base name only (a client could send `../../etc/passwd`), stripped of
// control characters and the characters Windows refuses, and never allowed to
// shadow an artifact the meeting owns.
func liveNoteImportSourceName(fileName string) string {
	base := filepath.Base(filepath.FromSlash(fileName))
	base = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		}
		return r
	}, base)
	base = strings.TrimSpace(base)
	// filepath.Ext(".") is "." and filepath.Ext("") is "", so the extension is
	// taken from the last dot that actually has a name in front of it.
	ext := ""
	if dot := strings.LastIndex(base, "."); dot > 0 {
		ext = strings.ToLower(base[dot:])
	}
	if base == "" || base == "." || base == ".." || strings.HasPrefix(base, ".") {
		base = "source" + ext
	}
	switch strings.ToLower(base) {
	case "minutes.md", "transcript.vtt", "transcript-revised.md", "chat.md":
		base = "source-" + base
	}
	return truncateFileName(base, 120)
}

// truncateFileName shortens a name to maxRunes while keeping its extension.
func truncateFileName(name string, maxRunes int) string {
	runes := []rune(name)
	if len(runes) <= maxRunes {
		return name
	}
	ext := filepath.Ext(name)
	keep := maxRunes - len([]rune(ext))
	if keep < 1 {
		return string(runes[:maxRunes])
	}
	stem := []rune(strings.TrimSuffix(name, ext))
	if len(stem) > keep {
		stem = stem[:keep]
	}
	return string(stem) + ext
}

// frameExtractionBudget bounds the video decode. It is separate from the
// transcode budget because the two run side by side and either may be the slow
// one.
const frameExtractionBudget = 20 * time.Minute

// extractAndSaveFrames is the best-effort half of an import: the transcript is
// the product, the frames are an extra. A failure is logged, never surfaced as
// a failed meeting.
func extractAndSaveFrames(
	parent context.Context,
	token string,
	projectID int64,
	paths service.LiveNotesPaths,
	startedAt int64,
	video string,
) {
	ctx, cancel := context.WithTimeout(parent, frameExtractionBudget)
	defer cancel()
	dir, err := os.MkdirTemp("", "clawbench-frames-*")
	if err != nil {
		slog.Warn("live notes: frame dir failed", "err", err)
		return
	}
	// The extracted files live here until they are copied into the meeting
	// folder, so the caller — not the extractor — owns the cleanup.
	defer func() { _ = os.RemoveAll(dir) }()

	frames, err := extractImportFrames(ctx, video, dir, liveNoteImportFrameMax)
	if err != nil {
		if errors.Is(err, ErrLiveNoteImportUnavailable) {
			return
		}
		slog.Warn("live notes: frame extraction failed", "err", err)
		return
	}
	if len(frames) == 0 {
		return
	}
	saveImportFrames(ctx, token, projectID, paths, startedAt, frames)
}
