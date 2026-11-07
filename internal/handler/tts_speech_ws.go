package handler

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/service"
	"clawbench/internal/speech"

	"github.com/coder/websocket"
	"github.com/hajimehoshi/go-mp3"
)

// ttsSpeechStart is the client's opening message on the speech WebSocket.
//
// The client may send either a messageId (the server extracts the answer and
// summarizes it — the same pipeline the web UI uses) or plain text.
type ttsSpeechStart struct {
	Type      string `json:"type"` // "start" (朗读已有消息) 或 "say" (发消息并朗读)
	MessageID int64  `json:"messageId"`
	Text      string `json:"text"`
	Language  string `json:"language"`
	SessionID string `json:"sessionId"` // type=say 时必填
}

// TTSSpeechWS is a single-call speech endpoint for embedded clients:
// summarize with the server's own summarizer, then stream the audio while it
// is synthesized, in headerless 16-bit mono PCM the device can play directly.
//
// Protocol (text frames are JSON, audio frames are binary):
//
//	client → {"type":"start","messageId":123}          // or {"text":"..."}
//	server → {"type":"summary","text":"..."}           // 摘要先给,设备立刻显示
//	server → {"type":"audio","sampleRate":24000,"channels":1,"bits":16}
//	server → <binary PCM frames>
//	server → {"type":"done"}
//
// Splitting the summary out as its own frame is deliberate: the client can
// render the text before the first audio byte arrives, instead of waiting for
// playback to finish.
func TTSSpeechWS(w http.ResponseWriter, r *http.Request) {
	projectPath := middleware.GetProjectFromCookie(r)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: wsOriginPatterns(r)})
	if err != nil {
		slog.Error("tts speech ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return // client left before sending start
	}
	var start ttsSpeechStart
	if err := json.Unmarshal(raw, &start); err != nil ||
		(start.Type != "start" && start.Type != "say") {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "invalid start message"})
		return
	}

	// type=say:发消息、跑完整轮、拿到回复,再继续摘要和朗读。设备因此只需要
	// 一条连接,不用自己发消息、轮询回复、再单独取摘要。
	if start.Type == "say" {
		if start.SessionID == "" || start.Text == "" {
			writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "sessionId and text required"})
			return
		}
		writeSpeechFrame(ctx, conn, map[string]any{"type": "phase", "phase": "thinking"})
		info := service.GetSessionFullInfo(start.SessionID)
		if info == nil || info.ProjectPath != projectPath {
			writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "session not found"})
			return
		}
		res := service.RunTurn(service.TurnSpec{
			Ctx:             ctx,
			Mode:            service.ModeInteractive,
			ProjectPath:     projectPath,
			BackendName:     info.Backend,
			SessionID:       start.SessionID,
			AgentID:         info.AgentID,
			ChatReq:         service.BuildChatRequest(start.Text, start.SessionID, projectPath, info.Backend, info.AgentID, "", "", "", "", "", false),
			DrainOnFinalize: true,
		})
		if res.Err != "" || res.MsgID == 0 {
			slog.Warn("tts speech ws: turn failed", slog.String("error", res.Err))
			writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "turn failed"})
			return
		}
		start.MessageID = res.MsgID
	}

	// Prefer the message id: the server extracts the answer and summarizes it
	// exactly like the web UI does. Plain text is the fallback.
	text := start.Text
	if start.MessageID > 0 {
		if conclusion := ttsExtractConclusion(start.MessageID); conclusion != "" {
			text = conclusion
		}
	}
	if text == "" {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "empty text"})
		return
	}
	lang := start.Language
	if lang == "" {
		lang = "zh"
	}

	provider := GetSpeechProvider()
	cacheKey, speakerPrefix := ttsCacheKeyAndPrefix(text, "", lang, ttsProviderCacheIdentity(provider))
	mp3Path := filepath.Join(projectPath, ".clawbench", "generated", "tts", cacheKey+".mp3")

	summary, ok := ttsSummarize(ctx, GetSummarizer(), cacheKey, text, lang, start.MessageID,
		"SummarizeFailed", true)
	if !ok {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "summarize failed"})
		return
	}
	if err := writeSpeechFrame(ctx, conn, map[string]any{
		"type": "summary", "text": summary, "messageId": start.MessageID,
	}); err != nil {
		return
	}
	slog.Info("tts speech ws: summary sent",
		slog.String("cache_key", cacheKey),
		slog.Int("summary_len", len([]rune(summary))),
	)

	spoken := ttsSynthText(speakerPrefix, summary)

	// Cached audio: decode straight from disk, no synthesis at all.
	if info, statErr := os.Stat(mp3Path); statErr == nil && info.Size() > 0 {
		streamPCMFile(ctx, conn, mp3Path)
		return
	}

	if streaming, ok := provider.(speech.StreamingSpeechProvider); ok {
		streamPCMSynth(ctx, conn, streaming, spoken, mp3Path, lang)
		return
	}

	// Provider without streaming support: synthesize to the file, then decode.
	sctx, scancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
	err = provider.Synthesize(sctx, spoken, mp3Path, lang)
	scancel()
	if err != nil {
		slog.Error("tts speech ws: synthesize failed", slog.String("error", err.Error()))
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "synthesize failed"})
		return
	}
	streamPCMFile(ctx, conn, mp3Path)
}

func writeSpeechFrame(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, wcancel := context.WithTimeout(ctx, ttsWSWriteTimeout)
	defer wcancel()
	return conn.Write(wctx, websocket.MessageText, data)
}

// streamPCMFile decodes an already synthesized MP3 file into PCM frames.
func streamPCMFile(ctx context.Context, conn *websocket.Conn, path string) {
	f, err := os.Open(path)
	if err != nil {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "open audio failed"})
		return
	}
	defer func() { _ = f.Close() }()

	dec, err := mp3.NewDecoder(f)
	if err != nil {
		slog.Error("tts speech ws: decode failed", slog.String("error", err.Error()))
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "decode failed"})
		return
	}
	pumpPCM(ctx, conn, dec)
}

// streamPCMSynth synthesizes while decoding: the MP3 chunks go through a pipe
// into the decoder, so the first PCM frame leaves before synthesis finishes.
func streamPCMSynth(ctx context.Context, conn *websocket.Conn,
	provider speech.StreamingSpeechProvider, text, outPath, lang string) {
	pr, pw := io.Pipe()
	chunks := make(chan []byte, 16)
	synthDone := make(chan error, 1)

	go func() {
		defer close(chunks)
		synthDone <- provider.SynthesizeStream(ctx, text, outPath, lang, chunks)
	}()
	go func() {
		for b := range chunks {
			if _, werr := pw.Write(b); werr != nil {
				break // client gone or decoder stopped
			}
		}
		_ = pw.Close()
	}()

	// NewDecoder blocks until enough bytes for the MP3 header have arrived.
	dec, err := mp3.NewDecoder(pr)
	if err != nil {
		slog.Error("tts speech ws: stream decode failed", slog.String("error", err.Error()))
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "decode failed"})
		_ = pr.Close()
		return
	}
	pumpPCM(ctx, conn, dec)
	_ = pr.Close()
	<-synthDone
}

// pumpPCM converts go-mp3's 16-bit stereo output to mono and writes it as
// binary frames. The format frame goes first so the client can configure its
// output before the first sample arrives.
func pumpPCM(ctx context.Context, conn *websocket.Conn, dec *mp3.Decoder) {
	rate := dec.SampleRate()
	if rate <= 0 {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "invalid sample rate"})
		return
	}
	if err := writeSpeechFrame(ctx, conn, map[string]any{
		"type": "audio", "sampleRate": rate, "channels": 1, "bits": 16,
	}); err != nil {
		return
	}

	buf := make([]byte, 8192)   // 立体声 16bit
	mono := make([]byte, 4096)  // 一半大小
	sent := 0
	started := time.Now()
	firstFrame := true
	for {
		n, err := dec.Read(buf)
		if n > 0 {
			frames := n / 4 // 2 声道 × 2 字节
			for i := 0; i < frames; i++ {
				l := int16(binary.LittleEndian.Uint16(buf[i*4:]))
				r := int16(binary.LittleEndian.Uint16(buf[i*4+2:]))
				m := int16((int32(l) + int32(r)) / 2)
				binary.LittleEndian.PutUint16(mono[i*2:], uint16(m))
			}
			if firstFrame {
				firstFrame = false
				slog.Info("tts speech ws: first audio frame",
					slog.Duration("after", time.Since(started)))
			}
			wctx, wcancel := context.WithTimeout(ctx, ttsWSWriteTimeout)
			werr := conn.Write(wctx, websocket.MessageBinary, mono[:frames*2])
			wcancel()
			if werr != nil {
				slog.Info("tts speech ws: client gone", slog.Int("sent_bytes", sent))
				return
			}
			sent += frames * 2
			// 按实时速度发:设备只有几十 KB 播放缓冲,发太快会把它压爆,
			// 它停止读 -> 服务器写超时 -> 连接被重置 -> 结尾整段丢失。
			if audioSec := float64(sent) / float64(rate*2); audioSec > time.Since(started).Seconds() {
				time.Sleep(time.Duration((audioSec - time.Since(started).Seconds()) * float64(time.Second)))
			}
		}
		if err != nil {
			break
		}
	}
	slog.Info("tts speech ws: audio done",
		slog.Int("sent_bytes", sent),
		slog.Float64("seconds", float64(sent)/float64(rate*2)),
	)
	writeSpeechFrame(ctx, conn, map[string]any{"type": "done"})
	// 给 TCP 一点时间把最后一帧真正发出去,再关连接。
	time.Sleep(300 * time.Millisecond)
}
