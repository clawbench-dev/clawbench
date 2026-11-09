package handler

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/stt"

	"github.com/coder/websocket"
)

//go:embed assets/audio_stt_probe.mp3
var sttProbeFiles embed.FS

// sttProbeAudio is the embedded "你好" voice clip used as a real-audio probe
// for STT connectivity tests (replaces the old silence WAV, which could not
// verify actual speech recognition).
var sttProbeAudio = mustLoadSTTProbe()

// mustLoadSTTProbe reads the embedded mp3 probe, panicking if it is missing so
// misconfiguration is caught at startup.
func mustLoadSTTProbe() []byte {
	data, err := sttProbeFiles.ReadFile("assets/audio_stt_probe.mp3")
	if err != nil {
		panic("stt: embedded probe audio missing: " + err.Error())
	}
	return data
}

// sttProvider is the global STT provider instance.
// Access is protected by sttProviderMu for hot-reload safety.
var (
	sttProvider   stt.STTProvider = stt.NewVLLMProvider("http://localhost:8000/v1", "openai/whisper-large-v3", "", "zh")
	sttProviderMu sync.RWMutex
)

// SetSTTProvider replaces the global STT provider.
// Goroutine-safe: concurrent Transcribe calls are protected by RWMutex.
func SetSTTProvider(p stt.STTProvider) {
	sttProviderMu.Lock()
	sttProvider = p
	sttProviderMu.Unlock()
}

// GetSTTProvider returns the current STT provider.
// Goroutine-safe for concurrent reads.
func GetSTTProvider() stt.STTProvider {
	sttProviderMu.RLock()
	p := sttProvider
	sttProviderMu.RUnlock()
	return p
}

// sttMaxBodyBytes limits the STT request body size (10MB).
const sttMaxBodyBytes = 10 << 20

// STTTranscribe handles POST /api/stt/transcribe (non-streaming).
// Multipart fields: file (audio), language (optional).
func STTTranscribe(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sttMaxBodyBytes)

	language := r.FormValue("language")
	file, _, err := r.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{strReqError: "audio file too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "missing audio file"})
		return
	}
	defer func() { _ = file.Close() }()

	provider := GetSTTProvider()
	text, err := provider.Transcribe(r.Context(), file, language)
	if err != nil {
		slog.Error("stt: transcribe failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: "transcription failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{sttWSText: text})
}

// sttWSWriteTimeout is the timeout for individual WebSocket writes.
const sttWSWriteTimeout = 5 * time.Second

// sttWSMaxAudioBytes caps total streamed audio per WS connection (20MB).
const sttWSMaxAudioBytes = 20 << 20

// sttWS type constants (satisfy goconst, aligned with existing WS constants).
const (
	sttWSType    = "type"
	sttWSText    = "text"
	sttWSPartial = "partial"
	sttWSDone    = "done"
	sttWSError   = "error"
	sttWSEndCtl  = "end"
)

// errSTTWSEnd is the sentinel returned by handleSTTWSMessage when the
// client sends the "end" control frame, signaling a clean stream completion.
var errSTTWSEnd = errors.New("stt ws: end")

// sttWSControl is a client text control message over the streaming WS.
type sttWSControl struct {
	Type       string `json:"type"`                 // "start" or "end"
	SampleRate int    `json:"sampleRate,omitempty"` // start: PCM sample rate
	Channels   int    `json:"channels,omitempty"`   // start: PCM channels
	Bits       int    `json:"bits,omitempty"`       // start: PCM bits per sample
	Context    string `json:"context,omitempty"`    // start: text to recognize against
}

// sttWSServerMsg is a server message over the streaming WS.
type sttWSServerMsg struct {
	// "text" (append), "partial" (replace), "done" (final) or "error".
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`    // "text" delta, or full text for "partial"
	Final   string `json:"final,omitempty"`   // final full text for "done"
	Message string `json:"message,omitempty"` // failure detail for "error"
}

// Buffered-engine chunking.
//
// A buffered engine is handed a FILE, so it has no idea where a word or a
// sentence ends — wherever the chunk is cut, that is what it transcribes. A cut
// on a fixed clock therefore lands mid-syllable every so often, and the piece
// on either side of it is transcribed wrong; there is no later revision to fix
// it, because this path appends text as it arrives. So a chunk is closed on a
// pause, and only force-cut when the speaker has gone on long enough that
// waiting for a pause would starve the client.
const (
	// sttBufferedTickMs is how often the chunk boundary is evaluated.
	sttBufferedTickMs = 200
	// sttBufferedQuietMs is how much silence closes a chunk.
	sttBufferedQuietMs = 800
	// sttBufferedMinChunkMs keeps a chunk from being opened for a few stray
	// frames: the client's voice gate sends one 100 ms keepalive every couple of
	// seconds while nothing is being said.
	sttBufferedMinChunkMs = 250
	// sttBufferedMinForceMs is the shortest forced cut. Below this a clip carries
	// so little context that a buffered engine guesses, and this path appends
	// text, so nothing corrects it afterwards. It only ever applies to speech
	// that never pauses — a normal sentence is cut at the pause, which is
	// shorter.
	sttBufferedMinForceMs = 800
	// sttBufferedForceMarginMs keeps a forced cut ahead of the client's
	// pause-to-send deadline, so the text lands in time to restart that timer
	// instead of letting the client send without it.
	sttBufferedForceMarginMs = 500
	// sttBufferedMinVoiceMs is how much of a chunk must actually carry speech
	// before it is uploaded. A single loud frame is not an utterance: the client
	// forwards one 100 ms keepalive frame every couple of seconds regardless of
	// the gate, and a noisy room keeps that above the floor. One such frame in an
	// otherwise silent chunk is exactly the input a buffered engine answers with
	// invented text. Kept just over that 100 ms so a one-word reply still counts.
	sttBufferedMinVoiceMs = 150
	// sttBufferedSilenceFloor is the peak level below which a frame carries no
	// speech, on the same 0..1 scale as the client's gate. It must stay BELOW the
	// most conservative sensitivity the client offers (0.0015): frames that
	// passed the client's gate are speech by definition, and a stricter floor
	// here would silently throw a soft-spoken user's words away.
	sttBufferedSilenceFloor = 0.0012
	// sttBufferedFailStreakLimit is how many chunks in a row may fail to be
	// transcribed before the client is told. One failure is a hiccup; a streak
	// means the engine is unusable (bad key, quota, unreachable host) and the
	// mode would otherwise keep "listening" forever while producing nothing.
	sttBufferedFailStreakLimit = 3
)

// sttStreamState holds the mutable streaming state, protected by mu to avoid
// data races between the incremental goroutine and the read goroutine.
type sttStreamState struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	// offset is the start of the audio not yet transcribed. Chunks are taken
	// from buffer[offset:] so the whole recording stays available for the final
	// re-transcription.
	offset int
	done   bool
	// sampleRate comes from the client's start frame and labels the WAV
	// container handed to the engine (a wrong rate is not an error, it is
	// garbled audio).
	sampleRate int
	// lastVoiceAt is when the pending chunk last carried audio above the floor.
	lastVoiceAt time.Time
	// voiceBytes is how much audio in the pending chunk carried speech, so a
	// lone keepalive frame is not mistaken for an utterance. Bytes rather than a
	// frame count: how many frames the client packs a second into is its own
	// business.
	voiceBytes int
	// voiceSeen records the same for the whole recording, so a session that
	// only ever carried silence is not sent to the engine at all.
	voiceSeen bool
	// failStreak counts consecutive chunks the engine refused, so a real
	// outage is reported while a single hiccup is not.
	failStreak int
}

// rateLocked returns the sample rate the client declared. Callers hold mu.
func (s *sttStreamState) rateLocked() int {
	if s.sampleRate <= 0 {
		return sttDefaultSampleRate
	}
	return s.sampleRate
}

// STTTranscribeWS handles WS /api/stt/transcribe/ws (streaming).
//
// Client → server:
//   - binary frames: raw audio bytes (appended to running buffer)
//   - text frame: {"type":"start",...} declares the PCM format
//   - text frame: {"type":"end"} signals recording stopped → final full transcription
//
// Server → client:
//   - {"type":"text","text":"<incremental>"} — transcribed new segment
//   - {"type":"done","final":"<full text>"} — final result after "end"
//   - {"type":"error","message":"..."} — failure
//
// The server accumulates audio and, whenever a chunk has ended (a pause, or the
// configured interval), transcribes that chunk and appends the text. After
// "end" it re-transcribes the full buffer only when stt.vllm.final_recheck is on;
// otherwise "done" carries no final and the accumulated text stands.
func STTTranscribeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: wsOriginPatterns(r)})
	if err != nil {
		slog.Error("stt ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	provider := GetSTTProvider()
	if streaming, ok := provider.(stt.StreamingSTTProvider); ok {
		serveStreamingSTT(r.Context(), conn, streaming, model.ConfigInstance)
		return
	}
	serveBufferedSTT(r.Context(), conn, provider, model.ConfigInstance)
}

// serveBufferedSTT handles engines that can only transcribe a complete audio
// segment: audio is accumulated and every finished chunk is transcribed once.
// It stays as the fallback for providers without live-streaming support, which
// is every engine except the two streaming ones — including the default
// OpenAI-compatible engine, so this is the path continuous-conversation mode
// uses there.
func serveBufferedSTT(ctx context.Context, conn *websocket.Conn, provider stt.STTProvider, cfg model.Config) {
	chunkMs := cfg.STT.VLLM.ChunkMs
	if chunkMs <= 0 {
		chunkMs = 1000
	}
	forceMs := bufferedForceCutMs(chunkMs, cfg.STT.SilenceSendSeconds)

	// One cancellable scope for the whole exchange: the chunker aborts the
	// connection by cancelling it, and the final transcription runs on a context
	// detached from that cancellation (see below).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	state := &sttStreamState{}
	readErr := make(chan error, 1)
	tickDone := make(chan struct{})

	// Incremental goroutine: close the pending chunk when it has ended and
	// transcribe it. It runs for as long as the connection does — continuous
	// conversation keeps this socket open across long silences, so a tick that
	// finds nothing to do must not be mistaken for the end of the stream.
	go func() {
		defer close(tickDone)
		ticker := time.NewTicker(sttBufferedTickMs * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if transcribeBufferedChunkIfDue(ctx, state, provider, conn, cfg.STT.ActiveLanguage(), forceMs) {
				cancel()
				return
			}
		}
	}()

	// Read goroutine: collect audio frames and watch for "end".
	go func() {
		for {
			mt, msg, rerr := conn.Read(ctx)
			if rerr != nil {
				readErr <- rerr
				return
			}
			if rerr = handleSTTWSMessage(state, conn, mt, msg); rerr != nil {
				readErr <- rerr
				return
			}
		}
	}()

	readFailure := <-readErr
	// Only an explicit "end" means the recording is over and somebody is
	// waiting for the final text. Any other read error means the socket is gone
	// — continuous-conversation mode closes it between turns — so a full
	// re-transcription would be work nobody waits for, written to a connection
	// that cannot receive it.
	if !errors.Is(readFailure, errSTTWSEnd) {
		return
	}
	// Stop the chunker and let it finish before the final write, so a late chunk
	// cannot land after "done" and be appended to a transcript the client has
	// already replaced. The final transcription below therefore runs on a
	// context detached from this cancellation.
	cancel()
	<-tickDone

	// Final answer. Only a whole-buffer re-transcription can span the chunk
	// boundaries, so it is what makes the final text more coherent than the
	// chunks the client accumulated — but it is a second pass over the entire
	// clip, so it is opt-in (stt.vllm.final_recheck, default off). When off, "done"
	// carries no final and the client keeps what the chunks produced.
	finalText := ""
	if cfg.STT.VLLM.FinalRecheck {
		state.mu.Lock()
		full := append([]byte(nil), state.buffer.Bytes()...)
		rate := state.rateLocked()
		heard := state.voiceSeen
		state.mu.Unlock()

		if len(full) > 0 && heard {
			text, terr := provider.Transcribe(context.WithoutCancel(ctx), bytes.NewReader(wavFromPCM(full, rate)), cfg.STT.ActiveLanguage())
			if terr == nil {
				finalText = text
			}
		}
	}

	msg := sttWSServerMsg{Type: sttWSDone, Final: finalText}
	data, _ := json.Marshal(msg)
	_ = writeSTTWSText(conn, data)
}

// bufferedForceCutMs is how long a chunk may run while the speaker keeps
// talking before it is cut anyway.
//
// `chunk_ms` is what the operator asked for, but it is not the only constraint:
// the client measures its pause-to-send timer from the last transcript it
// received, so a cut landing after that interval lets the client send and close
// the socket with this sentence's tail still unread. The client's deadline
// therefore wins — unless the operator set it so short that the clip would be
// useless to the engine, in which case recognition simply cannot keep up and
// the floor is kept.
func bufferedForceCutMs(chunkMs, silenceSendSeconds int) int {
	ms := chunkMs
	if silenceSendSeconds > 0 {
		ms = min(ms, silenceSendSeconds*1000-sttBufferedForceMarginMs)
	}
	return max(ms, sttBufferedMinForceMs)
}

// transcribeBufferedChunkIfDue takes the pending chunk once it has ended and
// transcribes it, sending the text as an incremental delta.
//
// A chunk ends on a pause — that is where a word boundary actually is — or when
// it has run past forceMs. Returns abort=true when the connection can no longer
// be written to.
func transcribeBufferedChunkIfDue(ctx context.Context, state *sttStreamState, provider stt.STTProvider, conn *websocket.Conn, language string, forceMs int) bool {
	state.mu.Lock()
	if state.done {
		state.mu.Unlock()
		return false
	}
	rate := state.rateLocked()
	pending := state.buffer.Len() - state.offset
	if pending < sttBufferedMinChunkMs*rate/1000*2 {
		state.mu.Unlock()
		return false
	}
	quiet := time.Since(state.lastVoiceAt) >= sttBufferedQuietMs*time.Millisecond
	if !quiet && pending < forceMs*rate/1000*2 {
		state.mu.Unlock()
		return false
	}
	seg := append([]byte(nil), state.buffer.Bytes()[state.offset:]...)
	state.offset = state.buffer.Len()
	voiced := state.voiceBytes >= rate/1000*sttBufferedMinVoiceMs*2
	state.voiceBytes = 0
	state.mu.Unlock()

	if !voiced {
		// Too little of this chunk carried speech: uploading it would only buy
		// invented text.
		return false
	}

	// Buffered engines take a file, not a byte stream: headerless samples are
	// either rejected or decoded as silence. This is the whole reason the
	// OpenAI-compatible path could not transcribe continuous conversation.
	text, terr := provider.Transcribe(ctx, bytes.NewReader(wavFromPCM(seg, rate)), language)
	if terr != nil {
		// A single failure is not worth ending the conversation — a gateway
		// hiccup would otherwise close the mode mid-sentence. A streak is: the
		// mode would keep "listening" and produce nothing, which looks exactly
		// like a microphone that hears nothing, so say which one it is.
		state.mu.Lock()
		state.failStreak++
		streak := state.failStreak
		state.mu.Unlock()
		slog.Warn("stt ws: incremental transcribe failed", slog.Int("streak", streak), slog.String("error", terr.Error()))
		if streak < sttBufferedFailStreakLimit {
			return false
		}
		_ = sendSTTMsg(conn, sttWSServerMsg{Type: sttWSError, Message: terr.Error()})
		return true
	}
	state.mu.Lock()
	state.failStreak = 0
	state.mu.Unlock()
	if text == "" {
		return false
	}
	state.mu.Lock()
	done := state.done
	state.mu.Unlock()
	if done {
		return false
	}

	msg := sttWSServerMsg{Type: sttWSText, Text: text}
	data, _ := json.Marshal(msg)
	return writeSTTWSText(conn, data) != nil
}

// handleSTTWSMessage processes a single STT WebSocket frame. Binary frames
// append audio to the streaming buffer (bounded) and note whether the chunk
// carries speech; a text "start" frame declares the PCM format and an "end"
// frame marks the stream done. Returns a non-nil error to terminate the read
// loop.
func handleSTTWSMessage(state *sttStreamState, conn *websocket.Conn, mt websocket.MessageType, msg []byte) error {
	switch mt {
	case websocket.MessageBinary:
		peak := pcmPeakRMS(msg)
		state.mu.Lock()
		if state.buffer.Len()+len(msg) > sttWSMaxAudioBytes {
			state.mu.Unlock()
			errData, _ := json.Marshal(sttWSServerMsg{Type: sttWSError})
			_ = writeSTTWSText(conn, errData)
			return errors.New("stt ws: audio exceeds limit")
		}
		state.buffer.Write(msg)
		if peak >= sttBufferedSilenceFloor {
			state.lastVoiceAt = time.Now()
			state.voiceBytes += len(msg)
			state.voiceSeen = true
		}
		state.mu.Unlock()
	case websocket.MessageText:
		var ctl sttWSControl
		if json.Unmarshal(msg, &ctl) != nil {
			// A malformed control frame must not kill the audio path.
			return nil //nolint:nilerr // intentional: ignore unparseable control frames
		}
		switch ctl.Type {
		case sttWSStartCtl:
			state.mu.Lock()
			if ctl.SampleRate > 0 {
				state.sampleRate = ctl.SampleRate
			}
			state.mu.Unlock()
		case sttWSEndCtl:
			state.mu.Lock()
			state.done = true
			state.mu.Unlock()
			return errSTTWSEnd
		}
	}
	return nil
}

// writeSTTWSText sends a text message to the client with a write timeout.
func writeSTTWSText(conn *websocket.Conn, data []byte) error {
	writeCtx, cancel := context.WithTimeout(context.Background(), sttWSWriteTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

// sttWSStartCtl is the control frame that carries the PCM format and the
// optional recognition context before any audio arrives.
const sttWSStartCtl = "start"

// sttDefaultSampleRate is assumed when the client sends no start frame. The
// browser capture path always produces 16 kHz mono 16-bit PCM.
const sttDefaultSampleRate = 16000

// sttStreamFinalTimeout bounds how long we wait for the engine's final result
// after the client signals end-of-audio. Without it a hung backend keeps the
// WebSocket (and the recording UI) open forever.
const sttStreamFinalTimeout = 15 * time.Second

// sttStreamRelay owns one live-streaming exchange: the engine session, the
// audio accounting and the result forwarder.
type sttStreamRelay struct {
	// Diagnostics: what arrived, and how loud. See close().
	frames  int
	peakRMS float64

	conn     *websocket.Conn
	provider stt.StreamingSTTProvider
	opts     stt.StreamOptions
	session  stt.StreamSession
	results  sync.WaitGroup
	total    int
}

// serveStreamingSTT relays audio to a live streaming engine and forwards its
// cumulative transcripts.
//
// Client → server: optional {"type":"start",...}, then binary PCM frames, then
// {"type":"end"}. Server → client: {"type":"partial","text":<text so far>}
// while speaking, then {"type":"done","final":<final text>}.
func serveStreamingSTT(ctx context.Context, conn *websocket.Conn, provider stt.StreamingSTTProvider, _ model.Config) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	relay := &sttStreamRelay{
		conn:     conn,
		provider: provider,
		opts:     stt.StreamOptions{SampleRate: sttDefaultSampleRate, Channels: 1, Bits: 16},
	}
	defer relay.close()

	for {
		mt, msg, rerr := conn.Read(ctx)
		if rerr != nil {
			return
		}
		done, herr := relay.handle(ctx, mt, msg)
		if herr != nil {
			_ = sendSTTMsg(conn, sttWSServerMsg{Type: sttWSError, Message: herr.Error()})
			return
		}
		if done {
			return
		}
	}
}

// handle processes one client frame. done=true means the exchange is over.
func (r *sttStreamRelay) handle(ctx context.Context, mt websocket.MessageType, msg []byte) (done bool, err error) {
	switch mt {
	case websocket.MessageBinary:
		return false, r.writeAudio(ctx, msg)
	case websocket.MessageText:
		return r.handleControl(msg)
	default:
		return false, nil
	}
}

// writeAudio forwards one PCM frame, opening the session on the first frame so
// a preceding start control frame can still shape the options.
func (r *sttStreamRelay) writeAudio(ctx context.Context, pcm []byte) error {
	if r.total == 0 {
		// One line per recording, and the only server-side evidence that the
		// client is actually producing audio. Without it "the microphone hears
		// nothing" and "the engine returns nothing" look identical from here.
		slog.Info("stt ws: audio started", slog.Int("first_frame_bytes", len(pcm)))
	}
	r.total += len(pcm)
	r.frames++
	if peak := pcmPeakRMS(pcm); peak > r.peakRMS {
		r.peakRMS = peak
	}
	if r.total > sttWSMaxAudioBytes {
		return errors.New("audio exceeds limit")
	}
	if r.session == nil {
		if err := r.openSession(ctx); err != nil {
			return err
		}
	}
	return r.session.Write(pcm)
}

// handleControl applies a start frame or finishes the stream.
func (r *sttStreamRelay) handleControl(msg []byte) (bool, error) {
	var ctl sttWSControl
	if err := json.Unmarshal(msg, &ctl); err != nil {
		// A malformed control frame must not kill the audio path.
		return false, nil //nolint:nilerr // intentional: ignore unparseable control frames
	}
	switch ctl.Type {
	case sttWSStartCtl:
		r.opts = applySTTStreamOptions(r.opts, ctl)
		return false, nil
	case sttWSEndCtl:
		if r.session == nil {
			_ = sendSTTMsg(r.conn, sttWSServerMsg{Type: sttWSDone, Final: ""})
			return true, nil
		}
		_ = r.session.Finish()
		waitForSTTResults(&r.results)
		return true, nil
	default:
		return false, nil
	}
}

// openSession dials the engine and starts forwarding its results.
func (r *sttStreamRelay) openSession(ctx context.Context) error {
	session, err := r.provider.OpenStream(ctx, r.opts)
	if err != nil {
		// The engine failing to open is the difference between "the microphone
		// is silent" and "the service rejected us"; say which one happened.
		slog.Warn("stt ws: engine session failed", slog.String("error", err.Error()))
		return err
	}
	r.session = session
	r.results.Add(1)
	go r.forwardResults()
	return nil
}

// forwardResults republishes engine results as partial/done messages.
func (r *sttStreamRelay) forwardResults() {
	defer r.results.Done()
	for res := range r.session.Results() {
		// An engine failure (bad key, quota, rejected frame) is terminal and
		// must reach the client as an error, not as an empty "done".
		if res.Err != nil {
			_ = sendSTTMsg(r.conn, sttWSServerMsg{Type: sttWSError, Message: res.Err.Error()})
			return
		}
		msg := sttWSServerMsg{Type: sttWSPartial, Text: res.Text}
		if res.Final {
			msg = sttWSServerMsg{Type: sttWSDone, Final: res.Text}
		}
		if err := sendSTTMsg(r.conn, msg); err != nil {
			return
		}
	}
}

// close releases the engine session.
func (r *sttStreamRelay) close() {
	if r.session != nil {
		r.session.Close()
	}
	// What the microphone actually delivered, so a silent client is
	// distinguishable from an engine that understood nothing. peak_rms is on
	// the same 0..1 scale as the client's gate: speech is ~0.02-0.2, room tone
	// below ~0.002, a muted device ~0.
	slog.Info("stt ws: session ended",
		slog.Int("bytes", r.total),
		slog.Int("frames", r.frames),
		slog.Float64("peak_rms", math.Round(r.peakRMS*10000)/10000),
	)
}

// pcmPeakRMS is the loudest 16-bit sample magnitude in one frame, as a 0..1
// fraction of full scale. Peak rather than average: a frame of speech with
// pauses would otherwise average down to nothing.
func pcmPeakRMS(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var peak float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8) //nolint:gosec // G115: the two bytes ARE a signed 16-bit sample; the wrap is the conversion
		f := float64(v) / 32768
		if f < 0 {
			f = -f
		}
		if f > peak {
			peak = f
		}
	}
	return peak
}

// applySTTStreamOptions overlays the start control frame onto the defaults.
func applySTTStreamOptions(opts stt.StreamOptions, ctl sttWSControl) stt.StreamOptions {
	if ctl.SampleRate > 0 {
		opts.SampleRate = ctl.SampleRate
	}
	if ctl.Channels > 0 {
		opts.Channels = ctl.Channels
	}
	if ctl.Bits > 0 {
		opts.Bits = ctl.Bits
	}
	if ctl.Context != "" {
		opts.Context = ctl.Context
	}
	return opts
}

// waitForSTTResults waits for the result forwarder to finish, bounded by
// sttStreamFinalTimeout so a silent engine cannot hang the connection.
func waitForSTTResults(wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sttStreamFinalTimeout):
		slog.Warn("stt ws: timed out waiting for the final transcript")
	}
}

// sendSTTMsg marshals and writes one server message.
func sendSTTMsg(conn *websocket.Conn, msg sttWSServerMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return writeSTTWSText(conn, data)
}
