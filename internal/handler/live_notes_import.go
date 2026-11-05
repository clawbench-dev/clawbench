// Import an existing recording (or video) as a meeting.
//
// The meeting pipeline is built around WebM/Opus, because that is what the
// browser recorder produces and what the merge/split primitives understand. A
// dropped file can be anything — an m4a from a phone, an mp4 screen recording,
// a wav from a voice recorder — so the import path normalizes it to a mono
// 16 kHz Opus track first and then hands the meeting to the ordinary chain.
//
// The converted file lands as `audio/meeting.webm`, which is exactly the shape a
// solo meeting has after the merge. Everything downstream (chunked ASR, the
// detail page's player, transcript regeneration) therefore works unchanged and
// has no idea the meeting was imported rather than recorded.
//
// Decoding is delegated to ffmpeg rather than reimplemented: covering "any
// audio or video file" means covering every container and codec a user can
// produce, which is precisely what ffmpeg is. When it is missing the endpoint
// says so instead of half-working.
package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

const (
	// liveNoteImportMaxBytes bounds one imported file. A two-hour 1080p
	// recording is well under this; the cap only stops a hostile or mistaken
	// upload from filling the disk before ffmpeg ever sees it.
	liveNoteImportMaxBytes = int64(4) << 30
	// liveNoteImportTimeout bounds the conversion. Audio-only transcoding runs
	// far faster than realtime, so this is generous even for a feature film.
	liveNoteImportTimeout = 30 * time.Minute
	// liveNoteImportFieldMax bounds one plain form field.
	liveNoteImportFieldMax = 4 << 10
	// liveNoteImportTempPrefix names the scratch copy of the upload. It lives in
	// the OS temp dir, NOT in the meeting folder: a `.webm` next to the meeting's
	// audio would be mistaken for a channel recording by the merge and by the
	// re-recognition source scan.
	liveNoteImportTempPrefix = "clawbench-import-*"
	// Multipart field names, as the panel writes them.
	liveNoteImportFieldFile    = "file"
	liveNoteImportFieldTitle   = "title"
	liveNoteImportFieldStarted = "startedAt"
)

// ErrLiveNoteImportUnavailable is returned when the server cannot decode media.
var ErrLiveNoteImportUnavailable = errors.New("ffmpeg is not installed")

// errImportNoFile means the multipart body carried no file part at all.
var errImportNoFile = errors.New("import: no file part")

// errImportTooLarge means the uploaded file passed the size cap.
var errImportTooLarge = errors.New("import: file too large")

// liveNoteImport is what the handler received, before any conversion.
type liveNoteImport struct {
	filePath  string
	fileName  string
	title     string
	startedAt int64
}

// transcodeToWebM and lookPath are variables so tests can exercise the pipeline
// without ffmpeg: the conversion is the one step whose correctness depends on an
// external binary, everything around it is ours.
var (
	transcodeToWebM = ffmpegTranscode
	lookPath        = exec.LookPath
)

// liveNoteImportAvailability reports whether this server can import media, and
// a stable reason code when it cannot.
func liveNoteImportAvailability() (bool, string) {
	if _, err := lookPath("ffmpeg"); err != nil {
		return false, "ffmpeg_missing"
	}
	return true, ""
}

// ServeLiveNotesImport handles POST /api/live-share/notes/import
// (authenticated, project-scoped, multipart).
//
// Fields: `file` (required), `title` (optional, defaults to the file name) and
// `startedAt` (optional unix ms, the client's file timestamp). The response is
// the new record, so the list can show it immediately; the conversion and the
// transcription chain run detached and are reported through the same artifact
// states a recorded meeting uses.
func ServeLiveNotesImport(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	if available, reason := liveNoteImportAvailability(); !available {
		slog.Warn("live notes: import refused", "reason", reason)
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "MeetingImportUnavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, liveNoteImportMaxBytes)
	upload, err := receiveLiveNoteImport(r)
	if err != nil {
		switch {
		case errors.Is(err, errImportTooLarge):
			writeLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "MeetingImportTooLarge")
		case errors.Is(err, errImportNoFile):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		default:
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "MeetingImportTooLarge")
				return
			}
			slog.Warn("live notes: import read failed", "err", err)
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		}
		return
	}

	token, err := liveshare.NewToken()
	if err != nil {
		_ = os.Remove(upload.filePath)
		slog.Error("live notes: import token failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	title := liveNoteImportTitle(upload.title, upload.fileName)
	startedAt, endedAt := liveNoteImportWindow(r.Context(), upload)

	// EnsureMeetingDir both creates the folder and writes the row, so a failure
	// here leaves nothing behind to clean up. The title is set afterwards
	// because the folder name is derived from it at creation time.
	paths, err := service.EnsureMeetingDir(token, projectID, projectPath, title, startedAt)
	if err != nil {
		_ = os.Remove(upload.filePath)
		slog.Error("live notes: import folder failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if err := service.SetMeetingTitle(projectID, token, title); err != nil {
		slog.Warn("live notes: import title failed", "err", err)
	}
	// An import is never a live meeting: it is closed the moment it exists, so
	// the list can never offer to "join" it while it transcodes.
	_ = service.EndLiveNote(token, endedAt)

	// The recording itself stays with the meeting: the user dragged a file in,
	// and the converted audio is a derivative. Only the scratch copy is deleted
	// when the move fails (a cross-device temp dir) — never the resource.
	source, cleanupPath := upload.filePath, upload.filePath
	if stored, storeErr := storeImportSource(paths.Dir, upload.filePath, upload.fileName); storeErr != nil {
		slog.Warn("live notes: keep imported recording failed", "err", storeErr)
	} else {
		source, cleanupPath = stored, ""
	}

	plan := liveNoteImportPlan()
	claimed, claimErr := service.ClaimArtifactJob(token, artifactPlanKinds(plan), 0)
	if claimErr != nil {
		cleanupImportFile(cleanupPath)
		slog.Error("live notes: import claim failed", "err", claimErr)
		model.WriteError(w, model.Internal(claimErr))
		return
	}
	if claimed {
		// The job outlives the request on purpose: the response is what puts the
		// record on screen, and conversion plus transcription take minutes.
		go runLiveNoteImport(token, projectID, projectPath, title, startedAt, paths, source, cleanupPath, plan) //nolint:gosec // detached by design
	} else {
		cleanupImportFile(cleanupPath)
	}

	record, _, recErr := service.GetMeetingRecord(projectPath, projectID, token)
	if recErr != nil {
		slog.Warn("live notes: import record read failed", "err", recErr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, shareSessionSegment: record})
}

// receiveLiveNoteImport streams the multipart body to a scratch file.
//
// Streamed rather than parsed into memory on purpose: the upload can be
// gigabytes, and `r.ParseMultipartForm` would either buffer it or spill it to a
// temp file anyway, with no size control of its own.
func receiveLiveNoteImport(r *http.Request) (liveNoteImport, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return liveNoteImport{}, err
	}
	var out liveNoteImport
	var fileErr error
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			cleanupImportFile(out.filePath)
			return liveNoteImport{}, nextErr
		}
		switch part.FormName() {
		case liveNoteImportFieldTitle:
			out.title = readImportField(part)
		case liveNoteImportFieldStarted:
			out.startedAt, _ = strconv.ParseInt(readImportField(part), 10, 64)
		case liveNoteImportFieldFile:
			if out.filePath != "" {
				_ = part.Close()
				continue
			}
			out.fileName = part.FileName()
			out.filePath, fileErr = writeImportFile(part, out.fileName)
			if fileErr != nil {
				_ = part.Close()
				cleanupImportFile(out.filePath)
				return liveNoteImport{}, fileErr
			}
		}
		_ = part.Close()
	}
	if out.filePath == "" {
		return liveNoteImport{}, errImportNoFile
	}
	return out, nil
}

// readImportField reads one small plain field.
func readImportField(part io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(part, liveNoteImportFieldMax))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// writeImportFile copies one part to a scratch file and reports its path.
func writeImportFile(part io.Reader, fileName string) (string, error) {
	suffix := strings.ToLower(filepath.Ext(fileName))
	if len(suffix) > 8 {
		suffix = ""
	}
	file, err := os.CreateTemp("", liveNoteImportTempPrefix+suffix)
	if err != nil {
		return "", fmt.Errorf("import: create temp file: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(part, liveNoteImportMaxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("import: write temp file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("import: close temp file: %w", closeErr)
	}
	if written == 0 {
		_ = os.Remove(file.Name())
		return "", errImportNoFile
	}
	if written > liveNoteImportMaxBytes {
		_ = os.Remove(file.Name())
		return "", errImportTooLarge
	}
	return file.Name(), nil
}

func cleanupImportFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// liveNoteImportTitle prefers what the user typed, then the file name.
//
// The file name is a far better default than the date placeholder a recorded
// meeting gets: a dropped file is already named by whoever recorded it, and the
// summary model only renames placeholder titles.
func liveNoteImportTitle(given, fileName string) string {
	if title := strings.TrimSpace(given); title != "" {
		return truncateRunes(title, liveNoteTitleMaxRunes)
	}
	if strings.TrimSpace(fileName) == "" {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	if base = strings.TrimSpace(base); base == "" {
		base = truncateRunes(fileName, liveNoteTitleMaxRunes)
	}
	return truncateRunes(base, liveNoteTitleMaxRunes)
}

// liveNoteImportStart trusts the client's file timestamp but never a future
// one: a bad clock would put the meeting at the top of the list forever.
func liveNoteImportStart(ms int64) int64 {
	now := time.Now().UnixMilli()
	if ms <= 0 || ms > now {
		return now
	}
	return ms
}

// liveNoteImportWindow turns the file's own timestamp into the meeting's span.
//
// A recorder writes as it records, so the file's timestamp is when the meeting
// FINISHED, not when it began. Probing the duration up front (a header read,
// not a decode) lets the record carry the real start, which is what the list
// sorts by and what every transcript offset is measured from. Without ffprobe
// the meeting collapses to an instant — wrong by one duration, but never in
// the future.
func liveNoteImportWindow(parent context.Context, upload liveNoteImport) (startedAt, endedAt int64) {
	endedAt = liveNoteImportStart(upload.startedAt)
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return liveNoteImportSpan(endedAt, probeDurationMS(ctx, upload.filePath))
}

// liveNoteImportSpan is the pure half: given the file's timestamp and the
// recording's length, where does the meeting sit on the clock?
func liveNoteImportSpan(endedAt, durationMS int64) (int64, int64) {
	if durationMS <= 0 || endedAt <= durationMS {
		return endedAt, endedAt
	}
	return endedAt - durationMS, endedAt
}

// liveNoteImportPlan is the initial job for an imported file: there are no
// segments yet, so the transcript step has to run recognition (the recorded
// path renders segments the client already uploaded).
func liveNoteImportPlan() []service.RegenStep {
	transcript := service.RegenStep{
		Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget,
	}
	minutes := service.RegenStep{
		Kind: service.ArtifactMinutes, Mode: service.RegenModeModel, Reason: service.RegenReasonTarget,
	}
	if !model.ConfigInstance.Meeting.Proofread {
		return []service.RegenStep{transcript, minutes}
	}
	return []service.RegenStep{transcript, {
		Kind: service.ArtifactRevised, Mode: service.RegenModeModel, Reason: service.RegenReasonDownstream,
	}, minutes}
}

// runLiveNoteImport is the detached job: convert, then run the normal chain.
//
// `source` is the recording inside the meeting folder; `cleanupPath` is a
// scratch copy to delete afterwards, empty when the upload was moved into place.
func runLiveNoteImport(
	token string,
	projectID int64,
	projectPath, title string,
	startedAt int64,
	paths service.LiveNotesPaths,
	source, cleanupPath string,
	plan []service.RegenStep,
) {
	defer cleanupImportFile(cleanupPath)

	ctx, cancel := context.WithTimeout(context.Background(), liveNoteImportTimeout)
	defer cancel()

	dst := filepath.Join(paths.Audio, service.LiveNoteMeetingAudioFile)
	stopBeat := heartbeatImport(token)
	durationMS, err := transcodeToWebM(ctx, source, dst)
	stopBeat()
	if err != nil {
		failLiveNoteImport(token, err)
		return
	}
	_ = service.RecordLiveNoteAudioChannels(token, []service.LiveNoteAudioChannel{{
		Channel:    service.LiveNoteMixChannel,
		File:       service.LiveNoteMeetingAudioFile,
		BaseAt:     startedAt,
		DurationMS: durationMS,
	}})

	// The frames are independent of the transcript and much cheaper for a short
	// clip, so they run alongside: whichever finishes first is on screen first.
	var frames sync.WaitGroup
	frames.Add(1)
	go func() {
		defer frames.Done()
		extractAndSaveFrames(ctx, token, projectID, paths, startedAt, source)
	}()

	runArtifactChain(token, projectID, projectPath, title, plan, false)
	frames.Wait()
}

// heartbeatImport keeps the transcript artifact fresh while ffmpeg works.
//
// A long conversion can outlive the 10-minute interruption sweep, and the record
// would then flip to "failed" while the job is still running. The chain's own
// steps heartbeat themselves; this covers the window before it starts.
func heartbeatImport(token string) func() {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := service.HeartbeatArtifact(token, service.ArtifactTranscript, "", 0); err != nil {
					slog.Warn("live notes: import heartbeat failed", "err", err)
				}
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// failLiveNoteImport records a conversion failure on the transcript artifact,
// which is the step the import owns until the chain takes over.
func failLiveNoteImport(token string, cause error) {
	slog.Warn("live notes: import failed", "token", token, "err", cause)
	if err := service.SetArtifactFailed(token, service.ArtifactTranscript,
		service.ArtifactErrImport, cause.Error(), 0); err != nil {
		slog.Error("live notes: record import failure failed", "err", err)
	}
	if err := service.ResetQueuedArtifacts(token, 0); err != nil {
		slog.Warn("live notes: reset import queue failed", "err", err)
	}
}

// runMediaCommand is the process boundary for ffmpeg/ffprobe. A variable for
// the same reason as lookPath above: CI runners do not ship ffmpeg, so without
// a seam every test past this line would skip — and the parsing, pairing and
// selection around it is the part that can actually be wrong.
var runMediaCommand = func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return out.Bytes(), errBuf.Bytes(), err
}

// ffmpegTranscode turns any container into the mono 16 kHz Opus track the rest
// of the pipeline expects, and reports the source duration in milliseconds.
func ffmpegTranscode(ctx context.Context, src, dst string) (int64, error) {
	bin, err := lookPath("ffmpeg")
	if err != nil {
		// Wrapped in the pipeline's own sentinel: the artifact row classifies by
		// error identity, and "ffmpeg vanished between the preflight and the job"
		// must still read as an import failure, not as an I/O error.
		return 0, fmt.Errorf("%w: %w", service.ErrLiveNoteImport, ErrLiveNoteImportUnavailable)
	}
	// -vn drops a video track; a file with no audio stream fails here, which is
	// the honest answer ("there is nothing to transcribe").
	_, stderr, runErr := runMediaCommand(ctx, bin,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", src,
		"-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "libopus", "-b:a", "32k",
		"-f", "webm", dst,
	)
	if runErr != nil {
		return 0, fmt.Errorf("%w: %s", service.ErrLiveNoteImport, lastLogLine(string(stderr), runErr))
	}
	if duration := probeDurationMS(ctx, dst); duration > 0 {
		return duration, nil
	}
	return probeDurationMS(ctx, src), nil
}

// probeDurationMS reads a media file's duration with ffprobe. It returns 0 when
// ffprobe is missing or the file has no duration: the meeting still works, it
// just has no length to report.
func probeDurationMS(ctx context.Context, path string) int64 {
	bin, err := lookPath("ffprobe")
	if err != nil {
		return 0
	}
	out, _, err := runMediaCommand(ctx, bin,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path,
	)
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) {
		return 0
	}
	return int64(math.Round(seconds * 1000))
}

// lastLogLine keeps the useful part of ffmpeg's stderr: it prints one diagnostic
// line per problem, and the last one names what actually went wrong.
func lastLogLine(stderr string, cause error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return cause.Error()
}
