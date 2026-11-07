package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"

	"github.com/coder/websocket"
)

// voiceTurnControl is a JSON control frame from the device.
type voiceTurnControl struct {
	Type      string `json:"type"` // listen | end | replay
	SessionID string `json:"sessionId"`
	MessageID int64  `json:"messageId"`
	Language  string `json:"language"`
}

// VoiceTurnWS runs one complete voice turn on a single connection:
//
//	client → {"type":"listen","sessionId":"..."}   (optional)
//	client → <binary PCM 16k mono frames>
//	client → {"type":"end"}
//	server → {"type":"final","text":"<what the user said>"}
//	server → {"type":"phase","phase":"thinking"}
//	server → {"type":"summary","text":"...","messageId":123}
//	server → {"type":"audio","sampleRate":24000,"channels":1,"bits":16}
//	server → <binary PCM frames>
//	server → {"type":"done"}
//
// Replay of an existing message uses the same connection:
//
//	client → {"type":"replay","messageId":123}
//	server → summary / audio / done
//
// One connection means one TLS handshake and one TLS context per turn. The
// previous design opened separate sockets for recognition and playback, and the
// embedded client could not always allocate a second context fast enough right
// after the first one closed.
func VoiceTurnWS(w http.ResponseWriter, r *http.Request) {
	projectPath := middleware.GetProjectFromCookie(r)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: wsOriginPatterns(r)})
	if err != nil {
		slog.Error("voice turn ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	kind, text, sessionID, lang, messageID := voiceTurnRead(ctx, conn)
	if kind == "" {
		return
	}
	if kind == "replay" {
		voiceTurnSpeak(ctx, conn, projectPath, messageID, lang)
		return
	}
	if text == "" {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "no speech"})
		return
	}

	if err := writeSpeechFrame(ctx, conn, map[string]any{"type": "final", "text": text}); err != nil {
		return
	}
	slog.Info("voice turn ws: transcribed", slog.String("text", text), slog.Int("runes", len([]rune(text))))

	// ---- Run the turn ----
	if err := writeSpeechFrame(ctx, conn, map[string]any{"type": "phase", "phase": "thinking"}); err != nil {
		return
	}
	info := service.GetSessionFullInfo(sessionID)
	if info == nil || info.ProjectPath != projectPath {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "session not found"})
		return
	}
	res := service.RunTurn(service.TurnSpec{
		Ctx:             ctx,
		Mode:            service.ModeInteractive,
		ProjectPath:     projectPath,
		BackendName:     info.Backend,
		SessionID:       sessionID,
		AgentID:         info.AgentID,
		ChatReq: service.BuildChatRequest(text, sessionID, projectPath, info.Backend,
			info.AgentID, "", "", "", "", "", false),
		DrainOnFinalize: true,
	})
	if res.Err != "" || res.MsgID == 0 {
		slog.Warn("voice turn ws: turn failed", slog.String("error", res.Err))
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "turn failed"})
		return
	}

	// ---- Summarize + stream audio ----
	voiceTurnSpeak(ctx, conn, projectPath, res.MsgID, lang)
}

// voiceTurnRead consumes the control frames and audio of one utterance.
// It returns kind="say" with the transcript, or kind="replay" with the message
// id in text, or kind="" when the client left.
func voiceTurnRead(ctx context.Context, conn *websocket.Conn) (kind, text, sessionID, lang string, messageID int64) {
	var buf bytes.Buffer
	lang = "zh"
	for {
		mt, msg, err := conn.Read(ctx)
		if err != nil {
			return "", "", "", "", 0
		}
		if mt == websocket.MessageBinary {
			buf.Write(msg)
			continue
		}
		var ctrl voiceTurnControl
		if json.Unmarshal(msg, &ctrl) != nil {
			continue
		}
		switch ctrl.Type {
		case "listen":
			sessionID = ctrl.SessionID
			if ctrl.Language != "" {
				lang = ctrl.Language
			}
		case "replay":
			return "replay", "", ctrl.SessionID, lang, ctrl.MessageID
		case "end":
			return "say", voiceTurnTranscribe(ctx, buf.Bytes()), sessionID, lang, 0
		}
	}
}

func voiceTurnTranscribe(ctx context.Context, audio []byte) string {
	if len(audio) == 0 {
		return ""
	}
	provider := GetSTTProvider()
	if provider == nil {
		return ""
	}
	text, err := provider.Transcribe(ctx, bytes.NewReader(audio), model.ConfigInstance.STT.Language)
	if err != nil {
		slog.Error("voice turn ws: transcribe failed", slog.String("error", err.Error()))
		return ""
	}
	return text
}

// voiceTurnSpeak summarizes the assistant message and streams its audio on the
// same connection. messageID is passed as a string so replay and turn share it.
func voiceTurnSpeak(ctx context.Context, conn *websocket.Conn, projectPath string, messageID int64, lang string) {
	text := ttsExtractConclusion(messageID)
	if text == "" {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "empty reply"})
		return
	}

	provider := GetSpeechProvider()
	cacheKey, speakerPrefix := ttsCacheKeyAndPrefix(text, "", lang, ttsProviderCacheIdentity(provider))
	mp3Path := filepath.Join(projectPath, ".clawbench", "generated", "tts", cacheKey+".mp3")

	summary, ok := ttsSummarize(ctx, GetSummarizer(), cacheKey, text, lang, messageID,
		"SummarizeFailed", true)
	if !ok {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "summarize failed"})
		return
	}
	if err := writeSpeechFrame(ctx, conn, map[string]any{
		"type": "summary", "text": summary, "messageId": messageID,
	}); err != nil {
		return
	}

	spoken := ttsSynthText(speakerPrefix, summary)
	if info, statErr := os.Stat(mp3Path); statErr == nil && info.Size() > 0 {
		streamPCMFile(ctx, conn, mp3Path)
		return
	}
	if streaming, isStreaming := provider.(speech.StreamingSpeechProvider); isStreaming {
		streamPCMSynth(ctx, conn, streaming, spoken, mp3Path, lang)
		return
	}
	sctx, scancel := context.WithTimeout(ctx, ttsSynthesizeTimeout)
	err := provider.Synthesize(sctx, spoken, mp3Path, lang)
	scancel()
	if err != nil {
		writeSpeechFrame(ctx, conn, map[string]any{"type": "error", "message": "synthesize failed"})
		return
	}
	streamPCMFile(ctx, conn, mp3Path)
}
