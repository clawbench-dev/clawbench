package speech

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// edgeDefaultVoice is the default Chinese voice for edge-tts.
	edgeDefaultVoice = "zh-CN-XiaoxiaoNeural"

	// Microsoft Edge TTS service constants.
	edgeBaseURL            = "speech.platform.bing.com/consumer/speech/synthesize/readaloud"
	edgeTrustedClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4" //nolint:gosec // Edge TTS public trusted client token
	edgeChromiumVersion    = "143.0.3650.75"

	// Windows file time epoch offset (seconds from 1601-01-01 to 1970-01-01).
	winEpochOffset = 11644473600
)

// EdgeTTSProvider implements SpeechProvider using Microsoft Edge TTS.
// Uses native Go WebSocket implementation with DRM token generation —
// no external CLI or Python dependency required.
type EdgeTTSProvider struct {
	// Voice is the edge-tts voice ID (default: "zh-CN-XiaoxiaoNeural").
	Voice string
	// Rate is the speech speed adjustment (e.g. "+0%", "+20%", "-10%").
	Rate string

	// synthHook replaces the WebSocket request in synthesizeText, so the
	// segment-level integrity check can be tested without an Edge endpoint.
	synthHook func(ctx context.Context, ssml string, w io.Writer) (int, error)
}

// NewEdgeTTSProvider creates an EdgeTTSProvider with sensible defaults.
func NewEdgeTTSProvider() *EdgeTTSProvider {
	return &EdgeTTSProvider{
		Voice: edgeDefaultVoice,
		Rate:  "+0%",
	}
}

// Synthesize generates audio from text using Microsoft Edge TTS and writes to outputPath.
func (p *EdgeTTSProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("edge-tts: failed to create output directory: %w", err)
	}

	slog.Info(
		"edge-tts synthesize",
		slog.String("output", outputPath),
		slog.Int("text_len", len([]rune(text))),
	)

	// Clean text — remove control characters that the service doesn't support.
	cleaned := removeIncompatibleChars(text)

	// Open output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("edge-tts: failed to create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Connect and synthesize (one request per segment, one continuous file)
	if err := p.synthesizeText(ctx, cleaned, f); err != nil {
		// Remove the empty/broken output file on error
		_ = f.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("edge-tts: %w", err)
	}

	slog.Info(
		"edge-tts synthesize completed",
		slog.String("output", outputPath),
		slog.Int("text_len", len([]rune(text))),
	)
	return nil
}

const (
	// edgeSegmentRunes bounds ONE request to the Edge service. A reply read in
	// full can be several thousand runes (minutes of audio); sending that as a
	// single request makes the whole read hang on one connection and, when it
	// fails, loses everything after the failure point. One request per segment
	// keeps each request short and lets the audio of segment N play while
	// segment N+1 is being synthesized.
	edgeSegmentRunes = 500

	// edgeSegmentTimeout bounds one segment request. It is per segment, not per
	// text: a long read legitimately takes minutes in total, but a single
	// request that hangs must fail fast instead of stalling the whole job.
	edgeSegmentTimeout = 90 * time.Second

	// edgeMinBytesPerRune is the floor for how many MP3 bytes one spoken rune is
	// worth. Observed speech runs well above this (24 kHz / 48 kbps mono ≈ 12
	// bytes per character), so a segment below it was cut off: the service
	// closed the turn after a fragment of the sentence, which is exactly the
	// half-sentence audio that must never be cached and replayed as a success.
	// Deliberately a lower bound, so a fast or unusually short reading still
	// passes; only a fragment is caught.
	edgeMinBytesPerRune = 6
)

// synthesizeText splits text into speech-sized segments and sends one Edge TTS
// request per segment, writing all of them into the same writer.
//
// The output stays ONE continuous stream/file: the caller (and the listener)
// sees no segment boundaries. What changes is the request granularity — and with
// it the failure granularity, which is why a segment that fails aborts the job
// instead of silently skipping its content.
func (p *EdgeTTSProvider) synthesizeText(ctx context.Context, text string, w io.Writer) error {
	synth := p.synthesizeViaWebSocket
	if p.synthHook != nil {
		synth = p.synthHook
	}
	return p.synthesizeSegments(ctx, text, func(sctx context.Context, ssml string) error {
		// One segment's audio is written to the caller's writer only AFTER the
		// service has answered for the whole segment. Streaming each frame as it
		// arrives is what let a half-spoken segment reach the listener and the
		// cache file before anyone knew it was half.
		var buf bytes.Buffer
		spoken, err := synth(sctx, ssml, &buf)
		if err != nil {
			return err
		}
		if err := checkSegmentAudio(edgeSSMLText(ssml), buf.Len(), spoken); err != nil {
			return err
		}
		_, writeErr := w.Write(buf.Bytes())
		return writeErr
	})
}

// edgeSSMLTextPattern pulls the text back out of the SSML a segment was sent
// with, so a finished segment can be checked against what it was asked to
// say. buildSSML only escapes &, < and >, and the whole payload is generated
// by this package, so this cannot match anything but the segment's own text.
var edgeSSMLTextPattern = regexp.MustCompile(`(?s)<prosody[^>]*>(.*)</prosody>`)

// edgeSSMLText returns the number of characters of text carried by ssml.
func edgeSSMLText(ssml string) int {
	m := edgeSSMLTextPattern.FindStringSubmatch(ssml)
	if m == nil {
		return 0
	}
	return len([]rune(m[1]))
}

// checkSegmentAudio rejects a segment whose audio is only a fraction of what its
// text should take: the service answered, closed the turn, and left the sentence
// half spoken. Such a fragment is not a short answer — the text it was asked to
// say is right there — so it must fail the job rather than be played back.
//
// runes is what the segment asked the service to say, bytes what came back, and
// heard how much of it the service reported as spoken. A service that reports
// word boundaries is the authority: a segment it admitted to covering only part
// of is rejected at any size. When it reports nothing, only the byte floor
// applies.
func checkSegmentAudio(runes, bytes, heard int) error {
	if runes <= 0 || bytes <= 0 {
		return fmt.Errorf("no audio received from service")
	}
	if heard > 0 && heard < runes {
		return fmt.Errorf("truncated: service spoke %d of %d runes", heard, runes)
	}
	if minBytes := runes * edgeMinBytesPerRune; bytes < minBytes {
		return fmt.Errorf("truncated: %d bytes of audio for %d runes (below %d)", bytes, runes, minBytes)
	}
	return nil
}

// synthesizeSegments is synthesizeText with the per-segment request lifted out,
// so the loop (ordering, per-segment budget, abort-on-failure) is testable
// without an Edge endpoint.
func (p *EdgeTTSProvider) synthesizeSegments(
	ctx context.Context, text string, synth func(context.Context, string) error,
) error {
	segments := splitSpeechSegments(text, edgeSegmentRunes)
	if len(segments) == 0 {
		return fmt.Errorf("no speakable text")
	}
	rate := p.Rate
	if rate == "" {
		rate = "+0%"
	}
	for i, seg := range segments {
		sctx, cancel := context.WithTimeout(ctx, edgeSegmentTimeout)
		err := synth(sctx, buildSSML(p.Voice, rate, seg))
		// Read the segment's own state BEFORE cancel(), which would set it.
		segmentErr := sctx.Err()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // caller cancelled (user stopped, client gone)
			}
			return fmt.Errorf("segment %d/%d: %w", i+1, len(segments), err)
		}
		if segmentErr != nil {
			// Returned without an error but its budget expired: the segment is
			// incomplete, and the rest must not pretend it was spoken in full.
			return fmt.Errorf("segment %d/%d: %w", i+1, len(segments), segmentErr)
		}
	}
	return nil
}

// splitSpeechSegments cuts text into segments of at most maxRunes runes, ending
// each one at a sentence boundary when there is one (so a segment is a whole
// sentence, not half of one). A run with no punctuation at all is cut at
// maxRunes, preferring a comma or space near the cut.
func splitSpeechSegments(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = edgeSegmentRunes
	}
	runes := []rune(text)
	var out []string
	for start := 0; start < len(runes); {
		if len(runes)-start <= maxRunes {
			if seg := strings.TrimSpace(string(runes[start:])); seg != "" {
				out = append(out, seg)
			}
			break
		}
		cut := start + maxRunes
		for i := start + maxRunes - 1; i > start; i-- {
			if isSpeechBoundary(runes[i]) {
				cut = i + 1
				break
			}
		}
		if cut == start+maxRunes {
			// No sentence boundary in the window: fall back to a comma/space in
			// the second half, so a long unpunctuated run still breaks somewhere
			// a listener would pause.
			for i := cut - 1; i > start+maxRunes/2; i-- {
				if runes[i] == '，' || runes[i] == ',' || runes[i] == ' ' {
					cut = i + 1
					break
				}
			}
		}
		if seg := strings.TrimSpace(string(runes[start:cut])); seg != "" {
			out = append(out, seg)
		}
		start = cut
	}
	return out
}

// isSpeechBoundary reports whether r ends a sentence (and so is a good place to
// split a synthesis request).
func isSpeechBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '…', '\n', '.', '!', '?', ';':
		return true
	}
	return false
}

// synthesizeViaWebSocket connects to the Edge TTS WebSocket, sends the SSML,
// and writes the received audio data to w.
//
// The return value is how many runes of text the service reported actually
// speaking (from its sentence/word boundary metadata), or 0 when it reported
// nothing — 0 means "unknown", never "spoke nothing".
//
//nolint:gocognit,gocyclo // WebSocket protocol requires sequential message handling with multiple error paths
func (p *EdgeTTSProvider) synthesizeViaWebSocket(ctx context.Context, ssml string, w io.Writer) (int, error) {
	// Generate DRM token and connection ID.
	// Sec-MS-GEC-Version "2-<ver>" pairs with the v2 token; a v1 token with a
	// "1-" version is rejected by the service with HTTP 403.
	secMsGec := generateSecMsGecV2(time.Now().Unix())
	secMsGecVersion := fmt.Sprintf("2-%s", edgeChromiumVersion)
	connID := generateConnectID()
	muid := generateMUID()

	// Build WebSocket URL with DRM parameters
	wsURL := fmt.Sprintf("wss://%s/edge/v1?TrustedClientToken=%s&ConnectionId=%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=%s",
		edgeBaseURL, edgeTrustedClientToken, connID, secMsGec, secMsGecVersion)

	// Build headers matching current Python edge-tts (v7.x)
	chromiumMajor := strings.Split(edgeChromiumVersion, ".")[0]
	header := make(http.Header)
	header.Set("Pragma", "no-cache")
	header.Set("Cache-Control", "no-cache")
	header.Set("Origin", "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold")
	header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	header.Set("Accept-Language", "en-US,en;q=0.9")
	header.Set("User-Agent", fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36 Edg/%s.0.0.0", chromiumMajor, chromiumMajor))
	header.Set("Cookie", fmt.Sprintf("muid=%s;", muid))

	// Dial with context
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if resp != nil {
			return 0, fmt.Errorf("websocket handshake failed (status %s): %w", resp.Status, err)
		}
		return 0, fmt.Errorf("websocket dial failed: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Word boundaries are enabled on purpose: the service then reports how much
	// of the text it actually spoke, which is what tells a complete reading from
	// a fragment (audio arriving is not the same as the sentence being finished).
	// A service that ignores the option simply reports nothing — callers treat
	// that as "unknown" and fall back to the end-only signal.
	timestamp := currentTimeInMST()
	configMsg := fmt.Sprintf(
		"X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n"+
			`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"true","wordBoundaryEnabled":"true"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}`,
		timestamp,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(configMsg)); err != nil {
		return 0, fmt.Errorf("failed to send config: %w", err)
	}

	// Send SSML
	requestID := generateConnectID()
	ssmlMsg := fmt.Sprintf(
		"X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%sZ\r\nPath:ssml\r\n\r\n%s",
		requestID, timestamp, ssml,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlMsg)); err != nil {
		return 0, fmt.Errorf("failed to send ssml: %w", err)
	}

	// Read audio data from WebSocket
	audioReceived := false
	spoken := 0
	for {
		select {
		case <-ctx.Done():
			if !audioReceived {
				return spoken, fmt.Errorf("context cancelled before audio was received")
			}
			// Report it rather than returning nil: the segment is incomplete, and
			// the caller must not carry on as if it had been spoken in full.
			return spoken, ctx.Err()
		default:
		}

		msgType, message, err := conn.ReadMessage()
		if err != nil {
			if audioReceived {
				// Connection closed after we got audio. It ended mid-turn, so the
				// speech may be cut off; the caller judges it by what was spoken.
				return spoken, nil
			}
			return spoken, fmt.Errorf("websocket read error: %w", err)
		}

		if msgType == websocket.TextMessage {
			msgStr := string(message)
			isTurnEnd := strings.Contains(msgStr, "Path:turn.end")
			if strings.Contains(msgStr, edgeWordBoundaryPath) {
				spoken += edgeBoundaryRunes(msgStr)
			}
			if isTurnEnd {
				if !audioReceived {
					return spoken, fmt.Errorf("no audio received from service")
				}
				return spoken, nil
			}
			// Other text messages (response, turn.start, audio.metadata) — ignore
			continue
		}

		if msgType == websocket.BinaryMessage {
			// Binary messages contain audio data.
			// Format: 2-byte header length (big-endian) + header text + \r\n + audio data
			// The headerLen value points to the \r\n separator after the header text.
			// Audio data starts at headerLen + 2 (after the \r\n).
			if len(message) < 2 {
				continue
			}
			headerLen := int(message[0])<<8 | int(message[1])
			audioStart := headerLen + 2 // skip the \r\n separator
			if audioStart > len(message) {
				continue
			}
			audioData := message[audioStart:]
			if len(audioData) == 0 {
				continue
			}
			if _, err := w.Write(audioData); err != nil {
				return spoken, fmt.Errorf("failed to write audio data: %w", err)
			}
			audioReceived = true
		}
	}
}

// edgeWordBoundaryPath marks a word-boundary metadata frame: the service telling
// us which characters of the request it has just spoken.
const edgeWordBoundaryPath = "Path:audio.metadata"

// edgeBoundaryRunes extracts the character span of one boundary frame.
//
// The frame is a bare JSON object after the "Path:audio.metadata" header line:
// {"Metadata":[{"Type":"WordBoundary","Data":{"text":{"Text":"你好","Length":2,"BoundaryType":"WordBoundary"},...}}]}
// Length is the number of characters spoken; it is what makes the covered span
// independent of punctuation or of how the service tokenizes.
func edgeBoundaryRunes(frame string) int {
	idx := strings.Index(frame, "{")
	if idx < 0 {
		return 0
	}
	var payload struct {
		Metadata []struct {
			Type string `json:"Type"`
			Data struct {
				Text struct {
					Length int `json:"Length"`
				} `json:"text"`
			} `json:"Data"`
		} `json:"Metadata"`
	}
	if err := json.Unmarshal([]byte(frame[idx:]), &payload); err != nil {
		return 0
	}
	n := 0
	for _, m := range payload.Metadata {
		if m.Type == "WordBoundary" && m.Data.Text.Length > 0 {
			n += m.Data.Text.Length
		}
	}
	return n
}

// --- DRM Token Generation ---

// The Sec-MS-GEC token generator (v2) lives in edge_tts_drm.go.

// generateMUID generates a random MUID (32 hex characters, uppercase).
func generateMUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// generateConnectID generates a UUID without dashes.
func generateConnectID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x%04x%04x%04x%012x",
		binaryUint32(b[0:4]), binaryUint16(b[4:6]),
		binaryUint16(b[6:8]), binaryUint16(b[8:10]),
		binaryUint48(b[10:16]))
}

func binaryUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func binaryUint16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func binaryUint48(b []byte) uint64 {
	return uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 | uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5])
}

// --- SSML Building ---

// buildSSML creates the SSML payload for Edge TTS.
func buildSSML(voice, rate, text string) string {
	// Escape XML special characters in text
	escaped := strings.ReplaceAll(text, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")

	return fmt.Sprintf(
		"<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'>"+
			"<voice name='%s'><prosody rate='%s'>%s</prosody></voice></speak>",
		voice, rate, escaped,
	)
}

// --- Utility Functions ---

// currentTimeInMST returns a JavaScript-style date string in UTC.
func currentTimeInMST() string {
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)")
}

// removeIncompatibleChars removes control characters that the Edge TTS service doesn't support.
func removeIncompatibleChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0 && r <= 8 || r >= 11 && r <= 12 || r >= 14 && r <= 31 {
			return ' '
		}
		return r
	}, s)
}

// --- Streaming Support ---

// Compile-time assertion that EdgeTTSProvider implements StreamingSpeechProvider.
var _ StreamingSpeechProvider = (*EdgeTTSProvider)(nil)

// Compile-time assertion that EdgeTTSProvider reports its cache identity.
var _ CacheIdentityProvider = (*EdgeTTSProvider)(nil)

// CacheIdentity reports the voice and rate, which both change the audio.
func (p *EdgeTTSProvider) CacheIdentity() string {
	return "edge|" + p.Voice + "|" + p.Rate
}

// SynthesizeStream generates audio from text using Microsoft Edge TTS, streaming
// audio chunks to chunkCh while simultaneously writing the complete file to outputPath
// for caching. The caller is responsible for closing chunkCh after this method returns.
func (p *EdgeTTSProvider) SynthesizeStream(ctx context.Context, text string, outputPath string, language string, chunkCh chan<- []byte) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("edge-tts: failed to create output directory: %w", err)
	}

	slog.Info(
		"edge-tts stream synthesize",
		slog.String("output", outputPath),
		slog.Int("text_len", len([]rune(text))),
	)

	// Clean text — remove control characters that the service doesn't support.
	cleaned := removeIncompatibleChars(text)

	// Open output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("edge-tts: failed to create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Create multi-writer: write to file AND send chunks to channel
	var writer io.Writer = f
	if chunkCh != nil {
		writer = io.MultiWriter(f, &chunkWriter{ch: chunkCh})
	}

	// Connect and synthesize (one request per segment, streamed as it arrives)
	if err := p.synthesizeText(ctx, cleaned, writer); err != nil {
		// Remove the empty/broken output file on error
		_ = f.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("edge-tts: %w", err)
	}

	slog.Info(
		"edge-tts stream synthesize completed",
		slog.String("output", outputPath),
		slog.Int("text_len", len([]rune(text))),
	)
	return nil
}

// chunkWriter sends each Write as a copied []byte on the channel.
// Uses a short timeout to handle transient client backpressure while
// preventing indefinite blocking of the Edge TTS WebSocket read loop.
type chunkWriter struct {
	ch      chan<- []byte
	dropped atomic.Int64
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case w.ch <- cp:
		// Chunk sent successfully
	default:
		// Channel full — try with 1s timeout to handle transient backpressure
		timer := time.NewTimer(1 * time.Second)
		defer timer.Stop()
		select {
		case w.ch <- cp:
			// Sent after brief wait
		case <-timer.C:
			// Client too slow — drop chunk. File still gets complete data.
			dropped := w.dropped.Add(1)
			if dropped <= 3 || dropped%10 == 0 {
				slog.Warn("tts streaming: audio chunk dropped, client too slow",
					slog.Int64("dropped_total", dropped),
					slog.Int("chunk_size", len(p)))
			}
		}
	}
	return len(p), nil
}
