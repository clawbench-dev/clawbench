// Package service — AI 听记 (live meeting notes) persistence and summarisation.
//
// A live share is a transient, in-memory room (internal/liveshare): the server
// never sees the media. Transcription therefore happens on the publisher, which
// uploads one short audio segment per channel (system audio / its own
// microphone / each viewer's microphone). Each upload is transcribed by the
// configured STT provider and stored here.
//
// Keeping the channels separate is the whole point: the publisher physically
// holds one track per participant, so the speaker label comes for free — no
// diarisation model is involved. Reading merges the channels back into one
// chronological timeline (see BuildLiveNotesTranscript).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/summarize"
)

// LiveNotesDDL creates the two tables behind AI 听记.
//
// Exported so handler tests can create the schema in their own databases.
//
// live_notes_segments holds one row per transcribed audio segment. token is the
// share token (the room's only identifier) and project_id scopes the publisher
// endpoints; there is deliberately no foreign key — the room itself lives in
// memory and disappears the moment the publisher stops.
//
// live_notes holds one row per share: the generated minutes plus their status.
// Minutes are produced asynchronously (a long meeting takes tens of seconds to
// summarize), so status is polled rather than returned inline.
const LiveNotesDDL = `
CREATE TABLE IF NOT EXISTS live_notes_segments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	channel TEXT NOT NULL,
	speaker TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_live_note_segments_token ON live_notes_segments(token, started_at);

CREATE TABLE IF NOT EXISTS live_notes (
	token TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'idle',
	minutes TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_live_notes_updated ON live_notes(updated_at);
`

// Live note minutes statuses. Kept as constants so the handler, the SPA and the
// tests cannot drift on spelling.
const (
	LiveNotesStatusIdle       = "idle"
	LiveNotesStatusGenerating = "generating"
	LiveNotesStatusReady      = "ready"
	LiveNotesStatusFailed     = "failed"
)

// ErrLiveNotesEmpty is returned when minutes are requested for a share that has
// no transcribed text.
var ErrLiveNotesEmpty = errors.New("live notes: transcript is empty")

// LiveNoteSegment is one transcribed audio segment from one channel.
type LiveNoteSegment struct {
	ID         int64  `json:"id"`
	Channel    string `json:"channel"` // "system" | "mic" | "viewer:<id>"
	Speaker    string `json:"speaker"` // publisher-localized display label
	StartedAt  int64  `json:"startedAt"`
	DurationMS int64  `json:"durationMs"`
	Text       string `json:"text"`
}

// LiveNote is the per-share minutes row.
type LiveNote struct {
	Token   string `json:"token"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Minutes string `json:"minutes"`
	Error   string `json:"error"`
}

// AppendLiveNoteSegment stores one transcribed segment. It also makes sure the
// live_notes row exists so that a late upload (arriving after the room was
// torn down) still has an owner to check the project against.
func AppendLiveNoteSegment(ctx context.Context, token string, projectID int64, seg LiveNoteSegment) (int64, error) {
	if token == "" {
		return 0, errors.New("live notes: empty token")
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return 0, fmt.Errorf("live notes: begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, execErr := tx.ExecContext(ctx,
		`INSERT INTO live_notes (token, project_id, status) VALUES (?, ?, ?)
		 ON CONFLICT(token) DO NOTHING`,
		token, projectID, LiveNotesStatusIdle,
	); execErr != nil {
		return 0, fmt.Errorf("live notes: ensure row: %w", execErr)
	}
	res, execErr := tx.ExecContext(ctx,
		`INSERT INTO live_notes_segments (token, project_id, channel, speaker, started_at, duration_ms, text)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, projectID, seg.Channel, seg.Speaker, seg.StartedAt, seg.DurationMS, seg.Text,
	)
	if execErr != nil {
		return 0, fmt.Errorf("live notes: insert segment: %w", execErr)
	}
	id, idErr := res.LastInsertId()
	if idErr != nil {
		return 0, fmt.Errorf("live notes: segment id: %w", idErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("live notes: commit: %w", commitErr)
	}
	return id, nil
}

// ListLiveNoteSegments returns a share's segments with id > sinceID, ordered by
// id. Ordering by id (not time) is what makes `since` a correct incremental
// cursor: a segment transcribed late can carry an earlier started_at, so the
// client re-sorts by time after appending.
func ListLiveNoteSegments(token string, sinceID int64) ([]LiveNoteSegment, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, channel, speaker, started_at, duration_ms, text
		   FROM live_notes_segments
		  WHERE token = ? AND id > ?
		  ORDER BY id`,
		token, sinceID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list segments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]LiveNoteSegment, 0, 32)
	for rows.Next() {
		var s LiveNoteSegment
		if err := rows.Scan(&s.ID, &s.Channel, &s.Speaker, &s.StartedAt, &s.DurationMS, &s.Text); err != nil {
			return nil, fmt.Errorf("live notes: scan segment: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live notes: iterate segments: %w", err)
	}
	return out, nil
}

// GetLiveNote returns the minutes row for a share.
func GetLiveNote(token string) (LiveNote, bool, error) {
	var n LiveNote
	err := store.ReadDB().QueryRow(
		`SELECT token, title, status, minutes, error FROM live_notes WHERE token = ?`, token,
	).Scan(&n.Token, &n.Title, &n.Status, &n.Minutes, &n.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveNote{}, false, nil
	}
	if err != nil {
		return LiveNote{}, false, fmt.Errorf("live notes: get: %w", err)
	}
	return n, true, nil
}

// LiveNoteProjectID reports which project owns a share's notes. The publisher
// endpoints use it to authorize requests for a room that has already ended.
func LiveNoteProjectID(token string) (int64, bool, error) {
	var id int64
	err := store.ReadDB().QueryRow(`SELECT project_id FROM live_notes WHERE token = ?`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("live notes: project lookup: %w", err)
	}
	return id, true, nil
}

// SetLiveNoteMinutes records the minutes status/output for a share.
func SetLiveNoteMinutes(token string, projectID int64, title, status, minutes, errMsg string) error {
	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, title, status, minutes, error, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   status = excluded.status,
		   minutes = excluded.minutes,
		   error = excluded.error,
		   title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE live_notes.title END,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, title, status, minutes, errMsg,
	)
	if err != nil {
		return fmt.Errorf("live notes: set minutes: %w", err)
	}
	return nil
}

// CleanupLiveNotes deletes notes (and their segments) last updated before the
// cutoff. Returns the number of shares removed.
func CleanupLiveNotes(ctx context.Context, before time.Time) (int, error) {
	var tokens []string
	rows, err := store.ReadDB().QueryContext(ctx,
		`SELECT token FROM live_notes WHERE updated_at < ?`, before.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: cleanup select: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t string
		if scanErr := rows.Scan(&t); scanErr != nil {
			return 0, fmt.Errorf("live notes: cleanup scan: %w", scanErr)
		}
		tokens = append(tokens, t)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return 0, fmt.Errorf("live notes: cleanup iterate: %w", rowsErr)
	}
	if len(tokens) == 0 {
		return 0, nil
	}

	tx, err := store.WriteBegin()
	if err != nil {
		return 0, fmt.Errorf("live notes: cleanup begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()
	for _, t := range tokens {
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM live_notes_segments WHERE token = ?`, t); execErr != nil {
			return 0, fmt.Errorf("live notes: cleanup segments: %w", execErr)
		}
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM live_notes WHERE token = ?`, t); execErr != nil {
			return 0, fmt.Errorf("live notes: cleanup notes: %w", execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("live notes: cleanup commit: %w", commitErr)
	}
	return len(tokens), nil
}

// ── Timeline assembly ───────────────────────────────────────────────────────

// liveNotesDedupeWindow is how far apart two identical lines may start and
// still be considered the same utterance. Speakers' voices reach the system
// loopback (and the publisher's microphone) with a delay, so a duplicate is
// never sample-aligned.
const liveNotesDedupeWindowMS = 5000

// SortLiveNoteSegments returns the segments in speaking order.
//
// started_at is stamped by a single clock (the publisher's), so comparing it
// across channels is meaningful. id breaks ties deterministically.
func SortLiveNoteSegments(in []LiveNoteSegment) []LiveNoteSegment {
	out := make([]LiveNoteSegment, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// normalizeNoteText folds a transcript line for duplicate detection: case,
// whitespace and punctuation are ignored so that the same sentence captured by
// two microphones compares equal.
func normalizeNoteText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DedupeLiveNoteSegments drops a segment when another channel captured the same
// words at nearly the same moment.
//
// This is the speaker's own voice leaking into a second channel: the audience
// playing through the publisher's speakers is re-captured by the system-audio
// loopback, and the publisher's microphone picks up whatever the speakers play.
// Without this, every remote participant appears twice.
//
// The earliest segment wins (it is the one closest to the source); the later
// copy is dropped. Genuine simultaneous speech by two people differs in words,
// so it is unaffected.
func DedupeLiveNoteSegments(sorted []LiveNoteSegment) []LiveNoteSegment {
	out := make([]LiveNoteSegment, 0, len(sorted))
	type kept struct {
		norm string
		at   int64
	}
	recent := make([]kept, 0, 8)
	for _, s := range sorted {
		norm := normalizeNoteText(s.Text)
		if norm == "" {
			continue
		}
		dup := false
		for _, k := range recent {
			if k.norm == norm && absInt64(k.at-s.StartedAt) <= liveNotesDedupeWindowMS {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		recent = append(recent, kept{norm: norm, at: s.StartedAt})
		if len(recent) > 8 {
			recent = recent[1:]
		}
		out = append(out, s)
	}
	return out
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// BuildLiveNotesTranscript renders the merged timeline as the text handed to
// the summariser: one line per utterance, prefixed with the offset from the
// first segment and the speaker label.
func BuildLiveNotesTranscript(segments []LiveNoteSegment) string {
	merged := DedupeLiveNoteSegments(SortLiveNoteSegments(segments))
	if len(merged) == 0 {
		return ""
	}
	base := merged[0].StartedAt
	var b strings.Builder
	for _, s := range merged {
		speaker := strings.TrimSpace(s.Speaker)
		if speaker == "" {
			speaker = s.Channel
		}
		text := strings.Join(strings.Fields(s.Text), " ")
		offset := (s.StartedAt - base) / 1000
		if offset < 0 {
			offset = 0
		}
		fmt.Fprintf(&b, "[%02d:%02d] %s：%s\n", offset/60, offset%60, speaker, text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ── Minutes generation ──────────────────────────────────────────────────────

const liveNotesMinutesPrompt = `你是一名专业的会议记录员。根据下面这场会议的实时转写（每行是「时间 说话人：内容」，不同说话人的语音来自不同信道，已按时间重排），生成结构化的会议纪要。

要求：
1. 输出 Markdown，使用以下章节（没有内容的章节可省略）：
   ## 概要
   ## 议题与讨论
   ## 结论与决定
   ## 待办事项
2. 待办事项用列表表示，能判断负责人时标注说话人，提到时间点时一并保留。
3. 只依据转写内容，不要编造。信息不完整或听不清的地方标注「（待确认）」。
4. 忽略寒暄、口头语和明显的转写错误；不要输出转写原文。
5. 使用与转写内容一致的语言输出（中英混合时以中文为主）。`

const liveNotesChunkPrompt = `你是一名会议记录员。下面是一场长会议转写的其中一段（每行是「时间 说话人：内容」）。请只提取这一段里的关键信息，用简短要点列出：
- 讨论的议题
- 达成的结论或决定
- 待办事项（尽量标注负责人）
不要展开叙述，不要输出转写原文，保留说话人姓名。使用与原文一致的语言。`

// liveNotesChunkRunes is the per-chunk input budget for a long meeting. Kept
// well below the model window so that prompt + output still fit.
const liveNotesChunkRunes = 6000

// GenerateLiveNotesMinutes turns a share's transcript into Markdown minutes.
//
// Long meetings are map-reduced: each chunk is condensed to bullet points, then
// the bullet points are merged into the final minutes. Sending the whole
// transcript in one request would either overflow the model window or be
// silently truncated by the provider.
func GenerateLiveNotesMinutes(ctx context.Context, title string, segments []LiveNoteSegment) (string, error) {
	transcript := BuildLiveNotesTranscript(segments)
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}

	chunks := splitRunes(transcript, liveNotesChunkRunes)
	contextBlock := transcript
	if len(chunks) > 1 {
		partials := make([]string, 0, len(chunks))
		for i, chunk := range chunks {
			out, err := summarize.AskAboutContext(
				ctx, summarizer, liveNotesChunkPrompt,
				fmt.Sprintf("第 %d/%d 段：\n%s", i+1, len(chunks), chunk),
				"请提取这一段的关键信息。", 1200,
			)
			if err != nil {
				return "", fmt.Errorf("live notes: chunk %d: %w", i+1, err)
			}
			partials = append(partials, out)
		}
		contextBlock = strings.Join(partials, "\n\n")
	}

	question := "请根据以上内容生成会议纪要。"
	if title != "" {
		question = "会议标题：" + title + "\n请根据以上内容生成会议纪要。"
	}
	minutes, err := summarize.AskAboutContext(ctx, summarizer, liveNotesMinutesPrompt, contextBlock, question, 3000)
	if err != nil {
		return "", fmt.Errorf("live notes: minutes: %w", err)
	}
	return strings.TrimSpace(minutes), nil
}

// splitRunes splits s into chunks of at most n runes, preferring a line break
// near the boundary so a single utterance is not cut in half.
func splitRunes(s string, n int) []string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return []string{s}
	}
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		if len(runes) <= n {
			out = append(out, string(runes))
			break
		}
		cut := n
		// Walk back to a newline within the last 15% of the window.
		for i := n; i > n-n/6 && i > 0; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	return out
}
