package stt

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Tencent's realtime handshake is defined in terms of HMAC-SHA1
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Tencent Cloud realtime speech recognition (实时语音识别, WebSocket v2).
//
// The handshake is signed in the query string (HMAC-SHA1 over
// host + path + sorted query, base64, URL-encoded), unlike the file API's
// TC3 header signature. Audio is raw 16 kHz (or 8 kHz) mono 16-bit PCM sent as
// binary frames; the stream ends with a text frame {"type":"end"} after which
// the server answers final=1 and closes.
const (
	// tencentHandshakeTimeout bounds the signed handshake. A bad signature is
	// reported by the server and then the socket is closed, so without this a
	// silent server would hold the caller until the request context expires.
	tencentHandshakeTimeout = 15 * time.Second

	// tencentPaceAllowance is how far ahead of realtime the session may run
	// before it sleeps. The server drops a stream that sends more than three
	// seconds of audio inside one second (code 4000), so a burst after a
	// network hiccup has to be spread out.
	tencentPaceAllowance = 2 * time.Second

	// tencentWriteTimeout bounds a single frame write.
	tencentWriteTimeout = 15 * time.Second
)

// OpenStream starts a realtime recognition session.
func (p *TencentProvider) OpenStream(ctx context.Context, opts StreamOptions) (StreamSession, error) {
	if err := p.requireCredentials(); err != nil {
		return nil, err
	}

	model := p.model()
	rate, err := tencentStreamRate(model, opts)
	if err != nil {
		return nil, err
	}

	// The AppID is read from the key pair, not from the settings — see
	// EffectiveAppID for why. Resolved after the cheap local checks so a bad
	// request never costs a round trip.
	appID, err := p.EffectiveAppID(ctx)
	if err != nil {
		return nil, err
	}

	wsURL, err := p.streamURL(model, appID)
	if err != nil {
		return nil, err
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, tencentHandshakeTimeout)
	defer cancelDial()
	conn, resp, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("stt: tencent stream dial: %w", err)
	}
	conn.SetReadLimit(1 << 20)

	sessionCtx, cancel := context.WithCancel(ctx)
	hsCtx, cancelHandshake := context.WithTimeout(sessionCtx, tencentHandshakeTimeout)
	err = tencentWaitHandshake(hsCtx, conn)
	cancelHandshake()
	if err != nil {
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}

	s := &tencentStreamSession{
		conn:       conn,
		results:    make(chan StreamResult, 8),
		done:       make(chan struct{}),
		ctx:        sessionCtx,
		cancel:     cancel,
		bytesPerMS: rate * 16 / 8 / 1000,
	}
	go s.readLoop(sessionCtx)
	return s, nil
}

// tencentStreamRate validates the PCM format against the engine's rate and
// returns the rate to account the byte budget with.
func tencentStreamRate(model string, opts StreamOptions) (int, error) {
	rate := opts.SampleRate
	if rate <= 0 {
		rate = 16000
	}
	channels := opts.Channels
	if channels <= 0 {
		channels = 1
	}
	if channels != 1 {
		return 0, fmt.Errorf("stt: tencent stream accepts mono audio only (got %d channels)", channels)
	}
	bits := opts.Bits
	if bits <= 0 {
		bits = 16
	}
	if bits != 16 {
		return 0, fmt.Errorf("stt: tencent stream accepts 16-bit PCM only (got %d-bit)", bits)
	}
	if strings.HasPrefix(model, "8k") && rate != 8000 {
		return 0, fmt.Errorf("stt: tencent model %q is an 8 kHz engine; the recorder sends %d Hz, pick a 16k model", model, rate)
	}
	if !strings.HasPrefix(model, "8k") && rate != 16000 {
		return 0, fmt.Errorf("stt: tencent model %q expects 16 kHz audio (got %d Hz)", model, rate)
	}
	return rate, nil
}

// streamURL builds and signs the realtime handshake URL.
func (p *TencentProvider) streamURL(model, appID string) (string, error) {
	base := p.WSBaseURL
	if base == "" {
		base = TencentASRWSBaseURL
	}
	base = strings.TrimRight(base, "/")
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	if !strings.HasPrefix(base, "ws://") && !strings.HasPrefix(base, "wss://") {
		return "", fmt.Errorf("stt: tencent stream base %q must be a ws(s) URL", base)
	}

	now := time.Now().Unix()
	nonce, err := tencentNonce()
	if err != nil {
		return "", err
	}
	params := url.Values{
		"secretid":          {p.SecretID},
		"timestamp":         {strconv.FormatInt(now, 10)},
		"expired":           {strconv.FormatInt(now+86400, 10)},
		"nonce":             {strconv.FormatInt(nonce, 10)},
		"engine_model_type": {model},
		"voice_id":          {uuid.NewString()},
		"voice_format":      {"1"}, // 1 = raw PCM
		"needvad":           {"1"},
		"convert_num_mode":  {strconv.Itoa(boolToInt(p.EnableITN))},
		"filter_modal":      {strconv.Itoa(boolToInt(p.EnableDDC))},
	}
	if p.EndWindowSize > 0 {
		params.Set("vad_silence_time", strconv.Itoa(clampInt(p.EndWindowSize, 500, 2000)))
	}

	path := tencentStreamAudioPath + url.PathEscape(strings.TrimSpace(appID))
	// The signature covers exactly the query that goes on the wire, host
	// included, so it is computed from the same encoded string.
	query := params.Encode()
	signStr := strings.TrimPrefix(strings.TrimPrefix(base, "wss://"), "ws://") + path + "?" + query

	mac := hmac.New(sha1.New, []byte(p.SecretKey))
	_, _ = mac.Write([]byte(signStr))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return base + path + "?" + query + "&signature=" + url.QueryEscape(signature), nil
}

// tencentWaitHandshake reads the handshake ack. A rejected signature comes back
// as a non-zero code followed by a close, so it must surface as an open error
// rather than an empty session.
func tencentWaitHandshake(ctx context.Context, conn volcConn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("stt: tencent stream handshake: %w", err)
		}
		var ack tencentStreamResponse
		if err := json.Unmarshal(data, &ack); err != nil {
			continue
		}
		if ack.Code != 0 {
			return fmt.Errorf("stt: tencent stream handshake failed: code %d: %s%s", ack.Code, ack.Message, tencentHandshakeHint(ack.Code))
		}
		return nil
	}
}

// tencentHandshakeHint explains the one auth failure users actually hit.
//
// The realtime API compares the AppID in the path with the account's APPID,
// while 一句话识别 never looks at it. The console shows the UIN right next to
// the APPID, so pasting the UIN passes every other check (including the
// settings test, before it probed the realtime handshake) and fails here with
// an error that only says "check the AppID".
func tencentHandshakeHint(code int) string {
	if code == 4002 {
		return " (the AppID is the account APPID — 10 digits starting with 125 — not the UIN)"
	}
	return ""
}

// tencentNonce returns a positive integer below 10 digits, as the handshake
// requires.
func tencentNonce() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		return 0, fmt.Errorf("stt: generate nonce: %w", err)
	}
	return n.Int64(), nil
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// tencentStreamResponse is one server push. The handshake ack has no result;
// recognition pushes carry one.
type tencentStreamResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	VoiceID string `json:"voice_id"`
	Final   int    `json:"final"`
	Result  *struct {
		SliceType    int    `json:"slice_type"`
		Index        int    `json:"index"`
		VoiceTextStr string `json:"voice_text_str"`
	} `json:"result"`
}

// tencentStreamSession is one live recognition session.
type tencentStreamSession struct {
	conn    volcConn
	results chan StreamResult
	done    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	writeMu sync.Mutex

	mu       sync.Mutex
	finished bool
	closed   bool
	// Pace state: the server enforces a 1:1 realtime rate.
	startedAt  time.Time
	sentBytes  int64
	bytesPerMS int
}

// Write sends one chunk of PCM audio, throttled to the realtime rate.
func (s *tencentStreamSession) Write(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	if s.closed || s.finished {
		s.mu.Unlock()
		return errors.New("stt: stream already finished")
	}
	if s.startedAt.IsZero() {
		s.startedAt = time.Now()
	}
	s.sentBytes += int64(len(pcm))
	ahead := s.realtimeAheadLocked()
	s.mu.Unlock()

	if ahead > tencentPaceAllowance {
		select {
		case <-time.After(ahead - tencentPaceAllowance):
		case <-s.ctx.Done():
			return errors.New("stt: stream closed")
		}
	}

	writeCtx, cancel := context.WithTimeout(s.ctx, tencentWriteTimeout)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageBinary, pcm)
}

// realtimeAheadLocked reports how much further ahead of realtime the stream is.
// Callers hold mu.
func (s *tencentStreamSession) realtimeAheadLocked() time.Duration {
	if s.bytesPerMS <= 0 || s.startedAt.IsZero() {
		return 0
	}
	audio := time.Duration(s.sentBytes/int64(s.bytesPerMS)) * time.Millisecond
	return audio - time.Since(s.startedAt)
}

// Finish tells the engine that no more audio will arrive.
func (s *tencentStreamSession) Finish() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.finished || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	s.mu.Unlock()

	writeCtx, cancel := context.WithTimeout(s.ctx, tencentWriteTimeout)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"end"}`))
}

// Results streams recognition updates until the session ends.
func (s *tencentStreamSession) Results() <-chan StreamResult { return s.results }

// Close releases the connection. Safe to call more than once.
//
// It deliberately does not take writeMu: cancelling the session context aborts
// a write stuck on an unresponsive upstream, and closing the socket unblocks it
// as well.
func (s *tencentStreamSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	close(s.done)
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

// readLoop accumulates per-sentence results into cumulative text.
//
// Tencent reports one sentence at a time: slice_type 0/1 carry the sentence
// being revised, 2 carries its final text. StreamResult.Text is the whole
// transcript, so finished sentences are kept as a prefix. A repeated index is
// ignored — the engine may resend a finalized sentence in the final frame.
func (s *tencentStreamSession) readLoop(ctx context.Context) {
	defer close(s.results)

	var committed, current, last string
	lastFinalIndex := -1
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			break
		}
		var resp tencentStreamResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp.Code != 0 {
			s.emit(StreamResult{Err: fmt.Errorf("stt: tencent stream error: code %d: %s", resp.Code, resp.Message)})
			return
		}
		if resp.Result != nil {
			r := resp.Result
			if r.SliceType == 2 {
				if r.Index != lastFinalIndex {
					committed += r.VoiceTextStr
					lastFinalIndex = r.Index
				}
				current = ""
			} else {
				current = r.VoiceTextStr
			}
			text := committed + current
			if text != last {
				last = text
				if !s.emit(StreamResult{Text: last}) {
					return
				}
			}
		}
		if resp.Final == 1 {
			break
		}
	}

	s.emit(StreamResult{Text: last, Final: true})
}

// emit delivers a result unless the session has been closed.
func (s *tencentStreamSession) emit(r StreamResult) bool {
	select {
	case s.results <- r:
		return true
	case <-s.done:
		return false
	}
}
