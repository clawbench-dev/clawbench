package handler

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/stt"

	"github.com/coder/websocket"
)

// Live-note streaming transcription.
//
// The publisher sends raw PCM for one channel over a WebSocket and receives
// transcript lines as they are recognized, instead of cutting the audio into
// files and posting each one. Two things fall out of that:
//
//   - Text appears while the person is still talking (the segment path could
//     only speak after a pause plus an upload plus a transcription round trip).
//   - The client stops caring which engine is configured. Doubao streams, and
//     Whisper cannot — so the SERVER picks: a streaming engine gets the frames
//     as they arrive, a buffered one gets them accumulated into a WAV every few
//     seconds. One client path, one endpoint, no capability flag to keep in
//     sync.
//
// The endpoint never decides what a "line" is by itself: both paths run the
// same commit rule below, so a meeting recorded on either engine produces the
// same shape of transcript.

const (
	// notesStreamCommitAfterMS ends a line once the engine has stopped revising
	// it for this long.
	//
	// A streaming engine has no "this utterance is finished" signal — it just
	// keeps returning the whole text of what it has heard so far. Stability is
	// the available substitute: when no new revision arrives for a moment, the
	// speaker has stopped, and what we hold is the line.
	notesStreamCommitAfterMS = 1200
	// notesStreamMaxLineMS force-commits a line that never goes quiet (someone
	// reading aloud without pausing), so the transcript does not become one
	// enormous row.
	notesStreamMaxLineMS = 15_000
	// notesStreamSilenceMS is how much quiet ends a buffered engine's chunk.
	notesStreamSilenceMS = 800
	// notesStreamBufferedMaxMS caps one buffered chunk regardless of silence.
	notesStreamBufferedMaxMS = 10_000
	// notesStreamTickMS is how often the commit rule is evaluated.
	notesStreamTickMS = 200
	// notesStreamMaxAudioBytes bounds one connection, matching the segment cap's
	// intent: a runaway client must not fill memory.
	notesStreamMaxAudioBytes = 64 << 20
)

// notesStreamSampleRate is the PCM rate the client is told to send and the
// rate the buffered path labels its WAV with.
const notesStreamSampleRate = 16000

// notesStreamRelay is the per-connection state.
type notesStreamRelay struct {
	conn      *websocket.Conn
	token     string
	projectID int64
	channel   string
	speaker   string

	streaming stt.StreamingSTTProvider
	session   stt.StreamSession

	mu sync.Mutex
	// current is the engine's latest full text for the line being spoken.
	current string
	// committed is how much of `current` already went to the database. A
	// streaming engine returns the whole utterance each time, so a line is
	// `current` minus this prefix.
	committed string
	// lineStart is when the current line's first audio arrived.
	lineStart time.Time
	// lastResultAt is when the engine last revised the text.
	lastResultAt time.Time
	// lastVoiceAt is when the channel last carried audio above the floor.
	lastVoiceAt time.Time

	// Buffered path: PCM waiting to be transcribed, and how long it spans.
	pending     []byte
	pendingFrom time.Time

	closed bool
}

// LiveNotesStreamWS handles WS /api/live-share/notes/stream.
//
// Query: token, channel, speaker.
// Client → server: binary PCM (16 kHz mono s16le), and {"type":"end"} to finish.
// Server → client: {"type":"partial","text":...} while a line is being revised,
//
//	{"type":"line","text":...,"startedAt":...,"durationMs":...} once it is stored,
//	{"type":"error","message":...} on failure.
func LiveNotesStreamWS(w http.ResponseWriter, r *http.Request) {
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	channel := r.URL.Query().Get("channel")
	if !validLiveNoteChannel(channel) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	provider := GetSTTProvider()
	if provider == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "BadRequest")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: wsOriginPatterns(r)})
	if err != nil {
		slog.Error("live notes stream: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	relay := &notesStreamRelay{
		conn:      conn,
		token:     token,
		projectID: projectID,
		channel:   channel,
		speaker:   truncateRunes(strings.TrimSpace(r.URL.Query().Get("speaker")), liveNoteSpeakerMaxRunes),
	}
	if s, isStreaming := provider.(stt.StreamingSTTProvider); isStreaming {
		relay.streaming = s
	}
	if err := relay.run(r.Context()); err != nil && !errors.Is(err, context.Canceled) {
		_ = sendNotesStreamMsg(conn, map[string]any{sttWSType: strReqError, messageKey: err.Error()})
	}
}

// run reads audio until the client stops, then flushes whatever is pending.
func (n *notesStreamRelay) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if n.streaming != nil {
		session, err := n.streaming.OpenStream(ctx, stt.StreamOptions{
			SampleRate: notesStreamSampleRate,
			Channels:   1,
			Bits:       16,
		})
		if err != nil {
			return err
		}
		n.session = session
		defer session.Close()
		go n.forwardResults()
	}

	go n.tick(ctx)

	var total int
	for {
		mt, msg, err := n.conn.Read(ctx)
		if err != nil {
			break
		}
		switch mt {
		case websocket.MessageBinary:
			total += len(msg)
			if total > notesStreamMaxAudioBytes {
				return errors.New("audio exceeds limit")
			}
			if err := n.writeAudio(msg); err != nil {
				return err
			}
		case websocket.MessageText:
			var ctl struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &ctl) == nil && ctl.Type == "end" {
				n.finish(ctx)
				return nil
			}
		}
	}
	// Socket closed without an explicit end (tab closed, network dropped):
	// keep what was already recognized rather than discarding the line.
	n.finish(context.WithoutCancel(ctx))
	return nil
}

// writeAudio feeds one PCM frame to whichever engine path is in use.
func (n *notesStreamRelay) writeAudio(pcm []byte) error {
	if len(pcm) < 2 {
		return nil
	}
	now := time.Now()
	loud := pcmPeakRMS(pcm) >= notesStreamSilenceFloor

	n.mu.Lock()
	if n.lineStart.IsZero() && loud {
		n.lineStart = now
	}
	if loud {
		n.lastVoiceAt = now
	}
	if n.streaming == nil {
		n.pending = append(n.pending, pcm...)
		if n.pendingFrom.IsZero() {
			n.pendingFrom = now
		}
	}
	n.mu.Unlock()

	if n.streaming == nil {
		return nil
	}
	return n.session.Write(pcm)
}

// forwardResults mirrors the engine's revisions to the client.
func (n *notesStreamRelay) forwardResults() {
	for res := range n.session.Results() {
		if res.Err != nil {
			_ = sendNotesStreamMsg(n.conn, map[string]any{sttWSType: strReqError, messageKey: res.Err.Error()})
			continue
		}
		if res.Text == "" {
			continue
		}
		n.mu.Lock()
		n.current = res.Text
		n.lastResultAt = time.Now()
		// The engine returns the WHOLE session's text every time, so what is new
		// is the part after what has already been stored.
		partial := strings.TrimPrefix(res.Text, n.committed)
		n.mu.Unlock()
		if partial != "" {
			_ = sendNotesStreamMsg(n.conn, map[string]any{sttWSType: "partial", liveNoteTextKey: partial})
		}
	}
}

// tick applies the commit rule on a fixed cadence.
func (n *notesStreamRelay) tick(ctx context.Context) {
	t := time.NewTicker(notesStreamTickMS * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.commitIfDue(ctx)
		}
	}
}

// commitIfDue stores the current line when the engine has settled, or when the
// line has run long enough that waiting for silence would never end it.
func (n *notesStreamRelay) commitIfDue(ctx context.Context) {
	if n.streaming == nil {
		n.commitBufferedIfDue(ctx)
		return
	}
	now := time.Now()
	n.mu.Lock()
	full := n.current
	text := strings.TrimSpace(strings.TrimPrefix(full, n.committed))
	settled := !n.lastResultAt.IsZero() && now.Sub(n.lastResultAt) >= notesStreamCommitAfterMS*time.Millisecond
	tooLong := !n.lineStart.IsZero() && now.Sub(n.lineStart) >= notesStreamMaxLineMS*time.Millisecond
	var startedAt time.Time
	if settled || tooLong {
		startedAt = n.lineStart
	}
	n.mu.Unlock()

	if text == "" || (!settled && !tooLong) {
		return
	}
	n.commit(ctx, full, startedAt)
}

// commitBufferedIfDue transcribes the accumulated chunk once it ends.
func (n *notesStreamRelay) commitBufferedIfDue(ctx context.Context) {
	now := time.Now()
	n.mu.Lock()
	if len(n.pending) < notesStreamSampleRate/2 { // under ~250 ms of audio
		n.mu.Unlock()
		return
	}
	quiet := now.Sub(n.lastVoiceAt) >= notesStreamSilenceMS*time.Millisecond
	tooLong := now.Sub(n.pendingFrom) >= notesStreamBufferedMaxMS*time.Millisecond
	if !quiet && !tooLong {
		n.mu.Unlock()
		return
	}
	pcm := n.pending
	from := n.pendingFrom
	n.pending = nil
	n.pendingFrom = time.Time{}
	n.mu.Unlock()

	text, err := transcribeLiveNoteSegment(ctx, wavFromPCM(pcm, notesStreamSampleRate))
	if err != nil {
		slog.Warn("live notes stream: transcribe failed", "channel", n.channel, "err", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	_ = sendNotesStreamMsg(n.conn, map[string]any{sttWSType: "partial", liveNoteTextKey: text})
	n.commit(ctx, text, from)
}

// commit stores one line and tells the client it is final.
//
// `full` is the engine's whole-session text; the line is the part that has not
// been stored yet. Remembering `full` is what keeps the second line from
// repeating the first.
func (n *notesStreamRelay) commit(ctx context.Context, full string, startedAt time.Time) {
	text := strings.TrimSpace(strings.TrimPrefix(full, n.committed))
	if text == "" {
		return
	}
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	if startedAt.IsZero() {
		startedAt = n.lineStart
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	duration := time.Since(startedAt).Milliseconds()
	// Reset before the write: a slow database must not make the next line
	// inherit this one's text.
	n.committed = full
	n.current = ""
	n.lastResultAt = time.Time{}
	n.lineStart = time.Time{}
	n.mu.Unlock()

	seg := service.LiveNoteSegment{
		Channel:    n.channel,
		Speaker:    n.speaker,
		StartedAt:  startedAt.UnixMilli(),
		DurationMS: capInt64(duration, liveNoteSegmentMaxDurationMS),
		Text:       truncateRunes(text, liveNoteTextMaxRunes),
	}
	id, err := service.AppendLiveNoteSegment(ctx, n.token, n.projectID, seg)
	if err != nil {
		slog.Error("live notes stream: append failed", "err", err)
		return
	}
	// The row id is not decoration: the client merges transcript rows by id and
	// drops anything without one, so omitting it left the panel empty for the
	// whole meeting while the database was filling up correctly.
	_ = sendNotesStreamMsg(n.conn, map[string]any{
		sttWSType:       "line",
		"id":            id,
		liveNoteTextKey: seg.Text,
		"channel":       seg.Channel,
		"speaker":       seg.Speaker,
		"startedAt":     seg.StartedAt,
		"durationMs":    seg.DurationMS,
	})
}

// finish flushes the last line and closes the engine session.
func (n *notesStreamRelay) finish(ctx context.Context) {
	if n.streaming != nil && n.session != nil {
		_ = n.session.Finish()
		// The engine answers the finish with its final text; wait briefly so the
		// last sentence is not dropped.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			n.mu.Lock()
			done := n.current != "" && n.lastResultAt.After(time.Now().Add(-500*time.Millisecond))
			n.mu.Unlock()
			if done {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Flush whatever is pending regardless of the quiet/age gate: the meeting
	// is over, so there will never be more audio to satisfy it.
	n.flushBuffered(ctx)
	n.mu.Lock()
	full := n.current
	startedAt := n.lineStart
	n.mu.Unlock()
	// Commit BEFORE marking closed: commit() refuses to run on a closed relay,
	// so the other order silently dropped the last sentence of every meeting.
	n.commit(ctx, full, startedAt)
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
}

// flushBuffered transcribes everything still buffered, ignoring the gate.
func (n *notesStreamRelay) flushBuffered(ctx context.Context) {
	n.mu.Lock()
	if len(n.pending) < notesStreamSampleRate/2 {
		n.mu.Unlock()
		return
	}
	pcm := n.pending
	from := n.pendingFrom
	n.pending = nil
	n.pendingFrom = time.Time{}
	n.mu.Unlock()

	text, err := transcribeLiveNoteSegment(ctx, wavFromPCM(pcm, notesStreamSampleRate))
	if err != nil {
		slog.Warn("live notes stream: final transcribe failed", "channel", n.channel, "err", err)
		return
	}
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	_ = sendNotesStreamMsg(n.conn, map[string]any{sttWSType: "partial", liveNoteTextKey: text})
	n.commit(ctx, text, from)
}

// notesStreamSilenceFloor is the peak level below which a frame carries no
// speech. Same scale as the client's gate (0..1), set low on purpose: this only
// decides when a buffered chunk ends, and ending early loses words.
const notesStreamSilenceFloor = 0.004

// wavFromPCM wraps raw 16-bit mono PCM in a WAV container.
//
// The buffered engines (Whisper and friends) take a FILE, not a byte stream:
// handing them headerless samples produces either a decode error or silence.
func wavFromPCM(pcm []byte, rate int) []byte {
	const headerBytes = 44
	// The caller only ever passes 16 kHz, but the conversion below must not be a
	// silent truncation if that ever changes.
	if rate <= 0 || rate > 192_000 {
		rate = notesStreamSampleRate
	}
	out := make([]byte, headerBytes, headerBytes+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm))) //nolint:gosec // bounded by the frame cap
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(rate*2)) // byte rate
	binary.LittleEndian.PutUint16(out[32:34], 2)              // block align
	binary.LittleEndian.PutUint16(out[34:36], 16)             // bits
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm))) //nolint:gosec // bounded by the frame cap
	return append(out, pcm...)
}

// sendNotesStreamMsg writes one JSON message.
func sendNotesStreamMsg(conn *websocket.Conn, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}
