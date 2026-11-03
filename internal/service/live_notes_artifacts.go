package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawbench/internal/store"
)

// ArtifactKind identifies one regenerable meeting artifact.
//
// The order of ArtifactKinds is the dependency order: transcript feeds the
// proofread transcript, which feeds the minutes. Every plan and every status
// list uses this order so two call sites cannot disagree about "which step
// comes first".
type ArtifactKind string

const (
	ArtifactTranscript ArtifactKind = "transcript"
	ArtifactRevised    ArtifactKind = "revised"
	ArtifactMinutes    ArtifactKind = "minutes"
)

// ArtifactKinds is the canonical dependency order.
var ArtifactKinds = []ArtifactKind{ArtifactTranscript, ArtifactRevised, ArtifactMinutes}

// Valid reports whether k is one of the three artifacts this package knows.
func (k ArtifactKind) Valid() bool {
	switch k {
	case ArtifactTranscript, ArtifactRevised, ArtifactMinutes:
		return true
	}
	return false
}

// Artifact job statuses.
//
// idle means "no attempt is running"; it says nothing about whether content
// exists (that is content_at / the folder probe). queued is a step that belongs
// to an accepted plan but has not started yet.
const (
	ArtifactJobIdle    = "idle"
	ArtifactJobQueued  = "queued"
	ArtifactJobRunning = "running"
	ArtifactJobFailed  = "failed"
)

// Artifact error codes. The SPA localizes these; `error` is the raw detail.
const (
	ArtifactErrInterrupted = "interrupted"
	ArtifactErrNoSource    = "no_source"
	ArtifactErrModel       = "model"
	ArtifactErrSTT         = "stt"
	ArtifactErrAudio       = "audio"
	ArtifactErrIO          = "io"
	ArtifactErrTimeout     = "timeout"
	ArtifactErrUnknown     = "unknown"
)

// ArtifactState is one artifact's two axes.
//
// ContentAt only advances when the content actually CHANGED. That is what
// keeps a byte-identical rebuild from marking every downstream artifact stale
// and burning two model passes for nothing.
type ArtifactState struct {
	Token        string
	Kind         ArtifactKind
	JobStatus    string
	ErrorCode    string
	Error        string
	Detail       string
	ContentAt    int64
	JobAt        int64
	StartedAt    int64
	PendingASRAt int64
}

// Active reports whether this artifact belongs to a job that has not settled.
func (s ArtifactState) Active() bool {
	return s.JobStatus == ArtifactJobQueued || s.JobStatus == ArtifactJobRunning
}

// NormalizeArtifactState turns an abandoned heartbeat into a visible failure.
//
// A generating row is written by a detached goroutine. If the process dies
// inside the job's budget nothing ever writes the terminal status again, so the
// row stays running forever. There is no in-memory registry to consult after a
// restart; age is the only signal. Pure on purpose: the record builder and the
// hourly sweep must apply exactly the same rule.
func NormalizeArtifactState(s ArtifactState, nowMS int64) ArtifactState {
	if !s.Active() {
		return s
	}
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	if s.JobAt > 0 && nowMS-s.JobAt > liveNotesGeneratingStaleMS {
		s.JobStatus = ArtifactJobFailed
		s.ErrorCode = ArtifactErrInterrupted
		s.Error = "interrupted: no progress for 10 minutes"
		s.Detail = ""
		s.StartedAt = 0
	}
	return s
}

// ArtifactStatesForToken returns all three states, filling idle defaults for
// artifacts that have never been generated.
func ArtifactStatesForToken(token string) (map[ArtifactKind]ArtifactState, error) {
	byToken, err := ArtifactStatesForTokens([]string{token})
	if err != nil {
		return nil, err
	}
	return byToken[token], nil
}

// ArtifactStatesForTokens returns every artifact state for the given meetings
// in one query. The meeting list uses this instead of one query per record.
func ArtifactStatesForTokens(tokens []string) (map[string]map[ArtifactKind]ArtifactState, error) {
	out := make(map[string]map[ArtifactKind]ArtifactState, len(tokens))
	for _, token := range tokens {
		out[token] = defaultArtifactStates(token)
	}
	if len(tokens) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(tokens))
	for _, token := range tokens {
		args = append(args, token)
	}
	query := `SELECT token, kind, job_status, error_code, error, detail,
	                 content_at, job_at, started_at, pending_asr_at
	            FROM live_notes_artifacts
	           WHERE token IN (` + placeholders(len(tokens)) + `)`
	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("live notes: list artifact states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s ArtifactState
		if scanErr := rows.Scan(&s.Token, &s.Kind, &s.JobStatus, &s.ErrorCode, &s.Error,
			&s.Detail, &s.ContentAt, &s.JobAt, &s.StartedAt, &s.PendingASRAt); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan artifact state: %w", scanErr)
		}
		if _, ok := out[s.Token]; !ok {
			out[s.Token] = defaultArtifactStates(s.Token)
		}
		out[s.Token][s.Kind] = s
	}
	return out, rows.Err()
}

func defaultArtifactStates(token string) map[ArtifactKind]ArtifactState {
	out := make(map[ArtifactKind]ArtifactState, len(ArtifactKinds))
	for _, kind := range ArtifactKinds {
		out[kind] = ArtifactState{Token: token, Kind: kind, JobStatus: ArtifactJobIdle}
	}
	return out
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ClaimArtifactJob reserves the meeting for one plan.
//
// The write transaction is the mutex: two devices (or two tabs) that click at
// the same time serialize here, and the second one sees the first one's queued
// rows. A stale heartbeat does not block a new attempt — the process that owned
// it is gone.
func ClaimArtifactJob(token string, plan []ArtifactKind, nowMS int64) (bool, error) {
	if token == "" {
		return false, errors.New("live notes: empty token")
	}
	if len(plan) == 0 {
		return false, errors.New("live notes: empty artifact plan")
	}
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return false, fmt.Errorf("live notes: claim begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	cutoff := nowMS - liveNotesGeneratingStaleMS
	var running int
	if scanErr := tx.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM live_notes_artifacts
		  WHERE token = ? AND job_status IN (?, ?) AND job_at >= ?`,
		token, ArtifactJobQueued, ArtifactJobRunning, cutoff,
	).Scan(&running); scanErr != nil {
		return false, fmt.Errorf("live notes: claim probe: %w", scanErr)
	}
	if running > 0 {
		return false, nil
	}

	seen := make(map[ArtifactKind]bool, len(plan))
	for i, kind := range plan {
		if !kind.Valid() {
			return false, fmt.Errorf("live notes: unknown artifact kind %q", kind)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		status := ArtifactJobQueued
		if i == 0 {
			status = ArtifactJobRunning
		}
		if _, execErr := tx.ExecContext(context.Background(),
			`INSERT INTO live_notes_artifacts
			   (token, kind, job_status, error_code, error, detail, job_at, started_at)
			 VALUES (?, ?, ?, '', '', '', ?, ?)
			 ON CONFLICT(token, kind) DO UPDATE SET
			   job_status = excluded.job_status,
			   error_code = '', error = '', detail = '',
			   job_at = excluded.job_at, started_at = excluded.started_at`,
			token, string(kind), status, nowMS, nowMS,
		); execErr != nil {
			return false, fmt.Errorf("live notes: claim %s: %w", kind, execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return false, fmt.Errorf("live notes: claim commit: %w", commitErr)
	}
	return true, nil
}

// SetArtifactRunning marks one step as the current one.
func SetArtifactRunning(token string, kind ArtifactKind, detail string, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`INSERT INTO live_notes_artifacts
		   (token, kind, job_status, error_code, error, detail, job_at, started_at)
		 VALUES (?, ?, ?, '', '', ?, ?, ?)
		 ON CONFLICT(token, kind) DO UPDATE SET
		   job_status = excluded.job_status,
		   error_code = '', error = '', detail = excluded.detail,
		   job_at = excluded.job_at, started_at = excluded.started_at`,
		token, string(kind), ArtifactJobRunning, detail, nowMS, nowMS,
	)
	if err != nil {
		return fmt.Errorf("live notes: mark %s running: %w", kind, err)
	}
	return nil
}

// HeartbeatArtifact records progress for the interruption watchdog. It never
// resurrects a settled row: a late heartbeat from a cancelled job must not put
// the artifact back into "running".
func HeartbeatArtifact(token string, kind ArtifactKind, detail string, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes_artifacts SET detail = ?, job_at = ?
		  WHERE token = ? AND kind = ? AND job_status IN (?, ?)`,
		detail, nowMS, token, string(kind), ArtifactJobQueued, ArtifactJobRunning,
	)
	if err != nil {
		return fmt.Errorf("live notes: heartbeat %s: %w", kind, err)
	}
	return nil
}

// SetArtifactSucceeded settles one step. contentAt is 0 when the produced
// content was identical to what was already there, which keeps downstream
// artifacts from becoming stale for no reason.
func SetArtifactSucceeded(token string, kind ArtifactKind, contentAt, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`INSERT INTO live_notes_artifacts
		   (token, kind, job_status, error_code, error, detail, content_at, job_at, started_at, pending_asr_at)
		 VALUES (?, ?, ?, '', '', '', ?, ?, 0, 0)
		 ON CONFLICT(token, kind) DO UPDATE SET
		   job_status = excluded.job_status,
		   error_code = '', error = '', detail = '', started_at = 0,
		   content_at = CASE WHEN excluded.content_at > 0
		                     THEN excluded.content_at ELSE live_notes_artifacts.content_at END,
		   job_at = excluded.job_at,
		   pending_asr_at = 0`,
		token, string(kind), ArtifactJobIdle, contentAt, nowMS,
	)
	if err != nil {
		return fmt.Errorf("live notes: mark %s succeeded: %w", kind, err)
	}
	return nil
}

// SetArtifactFailed settles one step as failed, keeping whatever content the
// artifact already had.
func SetArtifactFailed(token string, kind ArtifactKind, code, msg string, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`INSERT INTO live_notes_artifacts
		   (token, kind, job_status, error_code, error, detail, job_at, started_at)
		 VALUES (?, ?, ?, ?, ?, '', ?, 0)
		 ON CONFLICT(token, kind) DO UPDATE SET
		   job_status = excluded.job_status,
		   error_code = excluded.error_code, error = excluded.error,
		   detail = '', started_at = 0, job_at = excluded.job_at`,
		token, string(kind), ArtifactJobFailed, code, msg, nowMS,
	)
	if err != nil {
		return fmt.Errorf("live notes: mark %s failed: %w", kind, err)
	}
	return nil
}

// ResetQueuedArtifacts returns the not-yet-started steps to idle after an
// upstream failure or a cancellation. Content is untouched.
func ResetQueuedArtifacts(token string, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes_artifacts
		    SET job_status = ?, detail = '', error_code = '', error = '',
		        job_at = ?, started_at = 0
		  WHERE token = ? AND job_status = ?`,
		ArtifactJobIdle, nowMS, token, ArtifactJobQueued,
	)
	if err != nil {
		return fmt.Errorf("live notes: reset queued artifacts: %w", err)
	}
	return nil
}

// ResetActiveArtifacts settles every unfinished step as idle. Used by
// cancellation, where the attempt is abandoned rather than failed.
func ResetActiveArtifacts(token string, nowMS int64) error {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes_artifacts
		    SET job_status = ?, detail = '', error_code = '', error = '',
		        job_at = ?, started_at = 0
		  WHERE token = ? AND job_status IN (?, ?)`,
		ArtifactJobIdle, nowMS, token, ArtifactJobQueued, ArtifactJobRunning,
	)
	if err != nil {
		return fmt.Errorf("live notes: reset active artifacts: %w", err)
	}
	return nil
}

// SetArtifactPendingASR records that recognition finished but the VTT write did
// not. A retry must render instead of paying for the whole recognition again.
func SetArtifactPendingASR(token string, atMS int64) error {
	if atMS <= 0 {
		atMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`INSERT INTO live_notes_artifacts (token, kind, job_status, pending_asr_at, job_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(token, kind) DO UPDATE SET
		   pending_asr_at = excluded.pending_asr_at, job_at = excluded.job_at`,
		token, string(ArtifactTranscript), ArtifactJobRunning, atMS, atMS,
	)
	if err != nil {
		return fmt.Errorf("live notes: mark pending asr: %w", err)
	}
	return nil
}

// ArtifactJobActive reports whether a fresh job is still plausibly running.
// Same age threshold as NormalizeArtifactState, applied as a read so the delete
// guard does not depend on the hourly sweep having run since a crash.
func ArtifactJobActive(token string, nowMS int64) (bool, error) {
	if token == "" {
		return false, nil
	}
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	cutoff := nowMS - liveNotesGeneratingStaleMS
	var n int
	err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM live_notes_artifacts
		  WHERE token = ? AND job_status IN (?, ?) AND job_at >= ?`,
		token, ArtifactJobQueued, ArtifactJobRunning, cutoff,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("live notes: artifact job lookup: %w", err)
	}
	return n > 0, nil
}

// ResetStaleArtifactJobs flips abandoned attempts to failed and reports how many
// it touched. Called at startup and by the hourly sweep.
func ResetStaleArtifactJobs(nowMS int64) (int, error) {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	cutoff := nowMS - liveNotesGeneratingStaleMS
	res, err := store.WriteExec(
		`UPDATE live_notes_artifacts
		    SET job_status = ?, error_code = ?, error = ?,
		        detail = '', job_at = ?, started_at = 0
		  WHERE job_status IN (?, ?) AND job_at < ?`,
		ArtifactJobFailed, ArtifactErrInterrupted, "interrupted: no progress for 10 minutes",
		nowMS, ArtifactJobQueued, ArtifactJobRunning, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: reset stale artifacts: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ── Record payload ─────────────────────────────────────────────────────────

// MeetingArtifactView is one artifact as the SPA sees it.
type MeetingArtifactView struct {
	HasContent bool   `json:"hasContent"`
	JobStatus  string `json:"jobStatus"`
	ErrorCode  string `json:"errorCode,omitempty"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
	ContentAt  int64  `json:"contentAt"`
	Stale      bool   `json:"stale"`
}

// MeetingGeneration is the derived "what is running now" summary.
type MeetingGeneration struct {
	Step      ArtifactKind   `json:"step"`
	Remaining []ArtifactKind `json:"remaining"`
	Detail    string         `json:"detail,omitempty"`
	StartedAt int64          `json:"startedAt"`
}

// BuildMeetingArtifactViews derives the SPA payload from the stored states.
//
// `flags` is the folder probe: it answers "is there a file the user can open",
// which is deliberately not the same question as content_at (a legacy meeting
// can have a file and no recorded content time).
func BuildMeetingArtifactViews(
	flags liveNoteArtifactState,
	states map[ArtifactKind]ArtifactState,
	nowMS int64,
) (map[ArtifactKind]MeetingArtifactView, *MeetingGeneration) {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	return buildArtifactViews(flags, states, nowMS), buildMeetingGeneration(states, nowMS)
}

func buildArtifactViews(
	flags liveNoteArtifactState,
	states map[ArtifactKind]ArtifactState,
	nowMS int64,
) map[ArtifactKind]MeetingArtifactView {
	views := make(map[ArtifactKind]MeetingArtifactView, len(ArtifactKinds))
	for _, kind := range ArtifactKinds {
		s := NormalizeArtifactState(states[kind], nowMS)
		views[kind] = MeetingArtifactView{
			HasContent: artifactHasContent(flags, kind),
			JobStatus:  s.JobStatus,
			ErrorCode:  s.ErrorCode,
			Error:      s.Error,
			Detail:     s.Detail,
			ContentAt:  s.ContentAt,
		}
	}
	markStale(views)
	return views
}

// markStale flags a level whose upstream has newer content.
//
// Staleness only compares adjacent levels, and only when both sides have
// actually recorded a content time. A legacy row with content_at = 0 must not
// make its downstream look stale.
func markStale(views map[ArtifactKind]MeetingArtifactView) {
	transcriptAt := views[ArtifactTranscript].ContentAt
	revisedAt := views[ArtifactRevised].ContentAt
	minutesAt := views[ArtifactMinutes].ContentAt
	revised := views[ArtifactRevised]
	revised.Stale = transcriptAt > 0 && revisedAt > 0 && transcriptAt > revisedAt
	views[ArtifactRevised] = revised
	minutes := views[ArtifactMinutes]
	minutes.Stale = revisedAt > 0 && minutesAt > 0 && revisedAt > minutesAt
	views[ArtifactMinutes] = minutes
}

// buildMeetingGeneration answers "what is running now": the first active step is
// the current one, every other queued step is what comes after it.
func buildMeetingGeneration(states map[ArtifactKind]ArtifactState, nowMS int64) *MeetingGeneration {
	var gen *MeetingGeneration
	for _, kind := range ArtifactKinds {
		s := NormalizeArtifactState(states[kind], nowMS)
		if !s.Active() {
			continue
		}
		if gen == nil {
			gen = &MeetingGeneration{Step: kind, StartedAt: s.StartedAt}
			if s.JobStatus == ArtifactJobRunning {
				gen.Detail = s.Detail
			}
		}
		if s.JobStatus == ArtifactJobQueued {
			gen.Remaining = append(gen.Remaining, kind)
		}
	}
	if gen == nil {
		return nil
	}
	// The running step is not "remaining"; if the first active row was queued
	// (a tiny window before the goroutine starts), the step is that queued kind
	// and it must not also appear in Remaining.
	filtered := gen.Remaining[:0]
	for _, kind := range gen.Remaining {
		if kind != gen.Step {
			filtered = append(filtered, kind)
		}
	}
	gen.Remaining = filtered
	if gen.StartedAt <= 0 {
		gen.StartedAt = firstActiveStart(states, nowMS)
	}
	return gen
}

// firstActiveStart is the fallback start time when the first active row has none
// (queued by a process that has not marked it running yet).
func firstActiveStart(states map[ArtifactKind]ArtifactState, nowMS int64) int64 {
	for _, kind := range ArtifactKinds {
		s := NormalizeArtifactState(states[kind], nowMS)
		if s.Active() && s.StartedAt > 0 {
			return s.StartedAt
		}
	}
	return 0
}

func artifactHasContent(flags liveNoteArtifactState, kind ArtifactKind) bool {
	switch kind {
	case ArtifactTranscript:
		return flags.hasTranscript
	case ArtifactRevised:
		return flags.hasRevised
	case ArtifactMinutes:
		return flags.hasMinutes
	default:
		return false
	}
}

// ── Audio channel metadata ─────────────────────────────────────────────────

// LiveNoteAudioChannel is one merged channel file's absolute origin.
//
// The merged file starts at 0ms, but only the merge knows which absolute time
// that is. Leading silent chunks never produce live_notes_segments rows, so
// MIN(started_at) cannot reconstruct it.
type LiveNoteAudioChannel struct {
	Channel    string `json:"channel"`
	File       string `json:"file"`
	BaseAt     int64  `json:"baseAt"`
	DurationMS int64  `json:"durationMs"`
}

// RecordLiveNoteAudioChannels persists the origins the merge just computed.
// Rows for channels the merge did not touch are left alone: a second pass sees
// the already-merged files as "done" and has no origin to report.
func RecordLiveNoteAudioChannels(token string, channels []LiveNoteAudioChannel) error {
	if token == "" || len(channels) == 0 {
		return nil
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("live notes: audio meta begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()
	for _, ch := range channels {
		if ch.Channel == "" || ch.File == "" || ch.BaseAt <= 0 {
			continue
		}
		if _, execErr := tx.ExecContext(context.Background(),
			`INSERT INTO live_notes_audio (token, channel, file, base_at, duration_ms)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(token, channel) DO UPDATE SET
			   file = excluded.file, base_at = excluded.base_at,
			   duration_ms = excluded.duration_ms`,
			token, ch.Channel, ch.File, ch.BaseAt, ch.DurationMS,
		); execErr != nil {
			return fmt.Errorf("live notes: record audio channel %s: %w", ch.Channel, execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("live notes: audio meta commit: %w", commitErr)
	}
	return nil
}

// ListLiveNoteAudioChannels returns the recorded channel origins, in a stable
// order (mix last, so the per-channel path is the default).
func ListLiveNoteAudioChannels(token string) ([]LiveNoteAudioChannel, error) {
	rows, err := store.ReadDB().Query(
		`SELECT channel, file, base_at, duration_ms FROM live_notes_audio
		  WHERE token = ?
		  ORDER BY CASE WHEN channel = ? THEN 1 ELSE 0 END, channel`,
		token, LiveNoteMixChannel,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list audio channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]LiveNoteAudioChannel, 0, 4)
	for rows.Next() {
		var ch LiveNoteAudioChannel
		if scanErr := rows.Scan(&ch.Channel, &ch.File, &ch.BaseAt, &ch.DurationMS); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan audio channel: %w", scanErr)
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// ErrLiveNoteAudioMissing means the recording a step needs is not on disk (or a
// channel with transcript text has no usable file). The SPA maps it to the
// `audio` error code.
var ErrLiveNoteAudioMissing = errors.New("live notes: recording is unavailable")

// ErrLiveNoteSTT wraps a speech-recognition failure so the handler can classify
// it separately from a model or file error.
var ErrLiveNoteSTT = errors.New("live notes: speech recognition failed")

// ReplaceLiveNoteSegments swaps a meeting's transcript rows for a new set and
// records that the VTT still has to be rendered, in ONE transaction.
//
// Recognition is expensive, so its output must not be lost if the render fails:
// pending_asr_at lets the next attempt render instead of re-running the whole
// recording. The old rows are only deleted once every new row is in hand.
func ReplaceLiveNoteSegments(token string, projectID int64, segments []LiveNoteSegment) error {
	if token == "" {
		return errors.New("live notes: empty token")
	}
	nowMS := time.Now().UnixMilli()
	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("live notes: replace segments begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(context.Background(), `DELETE FROM live_notes_segments WHERE token = ?`, token); err != nil {
		return fmt.Errorf("live notes: clear segments: %w", err)
	}
	for _, seg := range segments {
		if _, err := tx.ExecContext(context.Background(),
			`INSERT INTO live_notes_segments (token, project_id, channel, speaker, started_at, duration_ms, text)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			token, projectID, seg.Channel, seg.Speaker, seg.StartedAt, seg.DurationMS, seg.Text,
		); err != nil {
			return fmt.Errorf("live notes: insert recognized segment: %w", err)
		}
	}
	if _, err := tx.ExecContext(context.Background(),
		`INSERT INTO live_notes_artifacts (token, kind, job_status, pending_asr_at, job_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(token, kind) DO UPDATE SET
		   pending_asr_at = excluded.pending_asr_at, job_at = excluded.job_at`,
		token, string(ArtifactTranscript), ArtifactJobRunning, nowMS, nowMS,
	); err != nil {
		return fmt.Errorf("live notes: mark pending asr: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("live notes: replace segments commit: %w", err)
	}
	return nil
}

// LiveNoteSummarizing is the progress detail shown while the final summary pass
// runs. Localized from the server language because the job has no request.
func LiveNoteSummarizing() string {
	return liveNoteT("MeetingProgressFinal")
}

// LiveNoteUnknownSpeaker is the label used when a mix-only recording has no
// transcript history to copy a name from. Localized from the server language,
// like the other artifact text written without a request in scope.
func LiveNoteUnknownSpeaker() string {
	return liveNoteT("MeetingSpeakerUnknown")
}
