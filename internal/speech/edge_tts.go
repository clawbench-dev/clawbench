package speech

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	// Build SSML
	rate := p.Rate
	if rate == "" {
		rate = "+0%"
	}
	ssml := buildSSML(p.Voice, rate, cleaned)

	// Connect and synthesize
	if err := p.synthesizeViaWebSocket(ctx, ssml, f); err != nil {
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

// synthesizeViaWebSocket connects to the Edge TTS WebSocket, sends the SSML,
// and writes the received audio data to w.
//
//nolint:gocognit,gocyclo // WebSocket protocol requires sequential message handling with multiple error paths
func (p *EdgeTTSProvider) synthesizeViaWebSocket(ctx context.Context, ssml string, w *os.File) error {
	// Generate DRM token and connection ID
	secMsGec := generateSecMsGec()
	secMsGecVersion := fmt.Sprintf("1-%s", edgeChromiumVersion)
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
			return fmt.Errorf("websocket handshake failed (status %s): %w", resp.Status, err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Send speech generation config
	timestamp := currentTimeInMST()
	configMsg := fmt.Sprintf(
		"X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n"+
			`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"true","wordBoundaryEnabled":"false"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}`,
		timestamp,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(configMsg)); err != nil {
		return fmt.Errorf("failed to send config: %w", err)
	}

	// Send SSML
	requestID := generateConnectID()
	ssmlMsg := fmt.Sprintf(
		"X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%sZ\r\nPath:ssml\r\n\r\n%s",
		requestID, timestamp, ssml,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlMsg)); err != nil {
		return fmt.Errorf("failed to send ssml: %w", err)
	}

	// Read audio data from WebSocket
	audioReceived := false
	for {
		select {
		case <-ctx.Done():
			if !audioReceived {
				return fmt.Errorf("context cancelled before audio was received")
			}
			return nil
		default:
		}

		msgType, message, err := conn.ReadMessage()
		if err != nil {
			if audioReceived {
				// Connection closed after we got audio — that's fine
				return nil
			}
			return fmt.Errorf("websocket read error: %w", err)
		}

		if msgType == websocket.TextMessage {
			// Text messages contain metadata — check for turn.end
			msgStr := string(message)
			if strings.Contains(msgStr, "Path:turn.end") {
				if !audioReceived {
					return fmt.Errorf("no audio received from service")
				}
				return nil
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
				return fmt.Errorf("failed to write audio data: %w", err)
			}
			audioReceived = true
		}
	}
}

// --- DRM Token Generation ---

// generateSecMsGec generates the Sec-MS-GEC token required by Microsoft Edge TTS.
// Ported from Python edge-tts v7.x DRM.generate_sec_ms_gec().
func generateSecMsGec() string {
	// Get current Unix timestamp
	ticks := float64(time.Now().Unix())

	// Switch to Windows file time epoch (1601-01-01 00:00:00 UTC)
	ticks += float64(winEpochOffset)

	// Round down to the nearest 5 minutes (300 seconds)
	ticks -= math.Mod(ticks, 300)

	// Convert the ticks to 100-nanosecond intervals (Windows file time format)
	ticks *= 1e7 // 1e9 / 100

	// Create the string to hash
	strToHash := fmt.Sprintf("%.0f%s", ticks, edgeTrustedClientToken)

	// Compute the SHA256 hash and return uppercased hex digest
	hash := sha256.Sum256([]byte(strToHash))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

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
