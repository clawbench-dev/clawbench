package stt

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Doubao streaming (bidirectional WebSocket) protocol constants.
//
// Every frame is a 4-byte header followed by an optional 4-byte sequence
// number, a 4-byte payload length and the payload itself:
//
//	byte 0: (protocol version << 4) | header size in 4-byte units
//	byte 1: (message type << 4)    | message-type-specific flags
//	byte 2: (serialization << 4)   | compression
//	byte 3: reserved
//
// The sequence number is only present when the flags carry 0x1/0x2; assuming it
// is always there (or never there) silently shifts every later field.
const (
	doubaoStreamPath = "/api/v3/sauc/bigmodel_async"

	volcHeaderBytes = 4

	volcMsgFullClientRequest   = 0x1
	volcMsgAudioOnlyRequest    = 0x2
	volcMsgFullServerResponse  = 0x9
	volcMsgAudioServerResponse = 0xB
	volcMsgError               = 0xF

	volcFlagNoSequence  = 0x0
	volcFlagPosSequence = 0x1
	volcFlagNegSequence = 0x2

	volcSerializationJSON = 0x1
	volcSerializationNone = 0x0

	volcCompressionGzip = 0x1
	volcCompressionNone = 0x0

	// doubaoFirstAudioSequence is where audio packets start counting. The
	// server assigns sequence 1 to the initial (full client request) frame and
	// rejects a stream whose first audio packet claims 1.
	doubaoFirstAudioSequence = 2

	// doubaoContextRuneLimit keeps the injected context inside the API's
	// 100-token budget for context + hotwords; longer input is truncated by the
	// server anyway, and cutting it ourselves keeps the request small.
	doubaoContextRuneLimit = 200
)

// doubaoStreamRequest is the initial (full client request) payload.
type doubaoStreamRequest struct {
	User    doubaoUser          `json:"user"`
	Audio   doubaoStreamAudio   `json:"audio"`
	Request doubaoStreamOptions `json:"request"`
}

type doubaoStreamAudio struct {
	Format  string `json:"format"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type doubaoStreamOptions struct {
	ModelName       string        `json:"model_name"`
	EnableITN       bool          `json:"enable_itn"`
	EnablePunc      bool          `json:"enable_punc"`
	EnableDDC       bool          `json:"enable_ddc"`
	OutputZHVariant string        `json:"output_zh_variant,omitempty"`
	ResultType      string        `json:"result_type"`
	EndWindowSize   int           `json:"end_window_size,omitempty"`
	Corpus          *doubaoCorpus `json:"corpus,omitempty"`
}

// doubaoCorpus carries context. The API takes `context` as a JSON *string*
// (nested JSON), not as an object — sending an object is rejected.
type doubaoCorpus struct {
	Context string `json:"context,omitempty"`
}

// doubaoStreamResponse is the decoded server frame.
type doubaoStreamResponse struct {
	Result struct {
		Text string `json:"text"`
	} `json:"result"`
	Error string `json:"error"`
}

// OpenStream starts a live recognition session.
func (p *DoubaoProvider) OpenStream(ctx context.Context, opts StreamOptions) (StreamSession, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errors.New("stt: doubao API key is required")
	}

	rate := opts.SampleRate
	if rate <= 0 {
		rate = 16000
	}
	channels := opts.Channels
	if channels <= 0 {
		channels = 1
	}
	bits := opts.Bits
	if bits <= 0 {
		bits = 16
	}

	base := p.BaseURL
	if base == "" {
		base = DoubaoBaseURL
	}
	wsURL := strings.TrimRight(base, "/")
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += doubaoStreamPath

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"X-Api-Key":         {p.APIKey},
			"X-Api-Resource-Id": {p.resourceID()},
			"X-Api-Request-Id":  {uuid.NewString()},
		},
	})
	if err != nil {
		// A successful upgrade hands the connection to Conn and nils the body;
		// only a failed dial leaves a response body to drain.
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("stt: doubao stream dial: %w", err)
	}
	// Recognition results can be large; the default 32 KiB read limit truncates
	// them once the transcript grows past a few sentences.
	conn.SetReadLimit(1 << 20)

	req := doubaoStreamRequest{
		User:  doubaoUser{UID: "clawbench"},
		Audio: doubaoStreamAudio{Format: "pcm", Rate: rate, Bits: bits, Channel: channels},
		Request: doubaoStreamOptions{
			ModelName:       doubaoModelName,
			EnableITN:       p.EnableITN,
			EnablePunc:      p.EnablePunc,
			EnableDDC:       p.EnableDDC,
			OutputZHVariant: p.OutputZHVariant,
			ResultType:      "full",
			EndWindowSize:   p.EndWindowSize,
		},
	}
	if ctxText := doubaoContextPayload(opts.Context); ctxText != "" {
		req.Request.Corpus = &doubaoCorpus{Context: ctxText}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "encode request")
		return nil, fmt.Errorf("stt: encode stream request: %w", err)
	}
	if err := writeVolcFrame(ctx, conn, volcMsgFullClientRequest, volcFlagNoSequence,
		volcSerializationJSON, volcCompressionGzip, nil, payload); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "send request")
		return nil, fmt.Errorf("stt: send stream request: %w", err)
	}

	s := &doubaoStreamSession{
		conn:    conn,
		seq:     doubaoFirstAudioSequence,
		results: make(chan StreamResult, 8),
		done:    make(chan struct{}),
	}
	go s.readLoop(ctx)
	return s, nil
}

// doubaoContextPayload wraps free-form text into the dialog-context JSON string
// the API expects, truncating to the documented token budget.
func doubaoContextPayload(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > doubaoContextRuneLimit {
		runes = runes[len(runes)-doubaoContextRuneLimit:]
	}
	payload, err := json.Marshal(map[string]any{
		"context_type": "dialog_ctx",
		"context_data": []map[string]string{{"speaker": "user", "text": string(runes)}},
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

// writeVolcFrame encodes and sends one protocol frame.
func writeVolcFrame(ctx context.Context, conn *websocket.Conn, msgType, flags, serialization, compression byte, seq *int32, payload []byte) error {
	frame, err := volcEncodeFrame(msgType, flags, serialization, compression, seq, payload)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, frame)
}

// volcEncodeFrame builds one protocol frame.
func volcEncodeFrame(msgType, flags, serialization, compression byte, seq *int32, payload []byte) ([]byte, error) {
	if compression == volcCompressionGzip {
		compressed, err := gzipPayload(payload)
		if err != nil {
			return nil, err
		}
		payload = compressed
	}

	buf := make([]byte, 0, volcHeaderBytes+8+len(payload))
	buf = append(
		buf,
		(0x1<<4)|0x1, // protocol version 1, header size 1 (4 bytes)
		(msgType<<4)|flags,
		(serialization<<4)|compression,
		0x0,
	)
	if seq != nil {
		//nolint:gosec // the wire format is a 32-bit field; negative values are the protocol's end-of-stream marker
		buf = binary.BigEndian.AppendUint32(buf, uint32(*seq))
	}
	//nolint:gosec // frame payloads are far below 4 GiB (capped by the caller)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(payload)))
	return append(buf, payload...), nil
}

// volcDecodeFrame splits a server frame into its message type and payload.
func volcDecodeFrame(data []byte) (msgType byte, payload []byte, err error) {
	if len(data) < volcHeaderBytes {
		return 0, nil, fmt.Errorf("stt: short frame (%d bytes)", len(data))
	}
	headerSize := int(data[0]&0x0F) * 4
	if headerSize < volcHeaderBytes || len(data) < headerSize {
		return 0, nil, fmt.Errorf("stt: bad header size (%d)", headerSize)
	}
	msgType = data[1] >> 4
	flags := data[1] & 0x0F
	compression := data[2] & 0x0F

	rest := data[headerSize:]
	// A sequence number is present only for the flags that say so.
	if flags&(volcFlagPosSequence|volcFlagNegSequence) != 0 {
		if len(rest) < 4 {
			return 0, nil, errors.New("stt: truncated sequence number")
		}
		rest = rest[4:]
	}
	if len(rest) < 4 {
		return 0, nil, errors.New("stt: truncated payload length")
	}
	size := int(binary.BigEndian.Uint32(rest[:4]))
	rest = rest[4:]
	if size > len(rest) {
		return 0, nil, fmt.Errorf("stt: payload size %d exceeds frame (%d)", size, len(rest))
	}
	payload = rest[:size]
	if compression == volcCompressionGzip && len(payload) > 0 {
		payload, err = gzipReader(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("stt: gunzip payload: %w", err)
		}
	}
	return msgType, payload, nil
}

func gzipPayload(p []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(p); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipReader(p []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(zr)
}

// doubaoStreamSession is one live recognition session.
type doubaoStreamSession struct {
	conn    *websocket.Conn
	results chan StreamResult
	done    chan struct{}

	mu       sync.Mutex
	seq      int32
	finished bool
	closed   bool
}

// Write sends one chunk of PCM audio.
func (s *doubaoStreamSession) Write(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.finished {
		return errors.New("stt: stream already finished")
	}
	seq := s.seq
	s.seq++
	return writeVolcFrame(context.Background(), s.conn, volcMsgAudioOnlyRequest, volcFlagPosSequence,
		volcSerializationNone, volcCompressionGzip, &seq, pcm)
}

// Finish tells the engine that no more audio will arrive.
func (s *doubaoStreamSession) Finish() error {
	s.mu.Lock()
	if s.finished || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	seq := -s.seq
	s.mu.Unlock()
	return writeVolcFrame(context.Background(), s.conn, volcMsgAudioOnlyRequest, volcFlagNegSequence,
		volcSerializationNone, volcCompressionGzip, &seq, nil)
}

// Results streams recognition updates until the session ends.
func (s *doubaoStreamSession) Results() <-chan StreamResult { return s.results }

// Close releases the connection. Safe to call more than once.
func (s *doubaoStreamSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.done)
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

// readLoop decodes server frames and republishes cumulative transcripts.
func (s *doubaoStreamSession) readLoop(ctx context.Context) {
	defer close(s.results)

	var last string
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			break
		}
		msgType, payload, err := volcDecodeFrame(data)
		if err != nil {
			continue
		}
		if msgType == volcMsgError {
			break
		}
		if msgType != volcMsgFullServerResponse && msgType != volcMsgAudioServerResponse {
			continue
		}
		var resp doubaoStreamResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			continue
		}
		if resp.Error != "" {
			break
		}
		if resp.Result.Text != "" && resp.Result.Text != last {
			last = resp.Result.Text
			if !s.emit(StreamResult{Text: last}) {
				return
			}
		}
	}

	s.emit(StreamResult{Text: last, Final: true})
}

// emit delivers a result unless the session has been closed.
func (s *doubaoStreamSession) emit(r StreamResult) bool {
	select {
	case s.results <- r:
		return true
	case <-s.done:
		return false
	}
}
