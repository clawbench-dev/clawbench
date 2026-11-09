package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/ws"

	"github.com/coder/websocket"
)

// Voice turn protocol string constants (goconst). The frame vocabulary is
// shared with the speech stream, so the type/phase/text keys reuse the
// constants declared in speech_stream.go.
const (
	voiceTurnKindListen = "listen"
	voiceTurnKindReplay = "replay"
	voiceTurnKindSay    = "say"
	voiceTurnFinalType  = "final"
)

// voiceTurnControl is a JSON control frame from the device.
type voiceTurnControl struct {
	Type      string `json:"type"` // listen | end | replay | say
	SessionID string `json:"sessionId"`
	MessageID int64  `json:"messageId"`
	Language  string `json:"language"`
	Text      string `json:"text"` // type=say:跳过识别,直接用这段文字跑一轮
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

	// 一条连接跑多轮:嵌入式端内存紧张,反复建立加密连接会因碎片失败,
	// 所以连接一直保持,每轮只交换控制帧和音频。
	for {
		kind, text, sessionID, lang, messageID := voiceTurnRead(ctx, conn)
		if kind == "" {
			return
		}
		if kind == voiceTurnKindReplay {
			projectPath = voiceTurnReplay(ctx, conn, projectPath, sessionID, messageID, lang)
			continue
		}
		if text == "" {
			_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "no speech"})
			continue
		}

		if err := writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: voiceTurnFinalType, sttWSText: text}); err != nil {
			return
		}
		slog.Info("voice turn ws: transcribed", slog.String("text", text), slog.Int("runes", len([]rune(text))))

		if !voiceTurnHandleSay(ctx, conn, sessionID, text, lang, projectPath) {
			return // the connection can no longer be written to
		}
	}
}

// voiceTurnReplay speaks an already-persisted message and returns the project
// the connection is bound to from now on.
//
// The ownership check mirrors the say path: the message id comes from the
// device, and without it a logged-in device could replay any project's message.
func voiceTurnReplay(ctx context.Context, conn *websocket.Conn,
	cookieProject, sessionID string, messageID int64, lang string,
) string {
	projectPath := cookieProject
	if info := service.GetSessionFullInfo(sessionID); info != nil && info.ProjectPath != "" {
		if cookieProject != "" && info.ProjectPath != cookieProject {
			slog.Warn("voice turn ws: replay of another project's session",
				slog.String("session", sessionID),
				slog.String("project", cookieProject))
			_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "session not found"})
			return projectPath
		}
		projectPath = info.ProjectPath
	}
	voiceTurnSpeak(ctx, conn, projectPath, messageID, lang)
	return projectPath
}

// voiceTurnHandleSay runs one spoken (or typed) turn: resolve the session, take
// the run slot, persist and announce the question, run the turn, and close it.
//
// Returns false when the connection is gone, so the caller stops the loop.
func voiceTurnHandleSay(ctx context.Context, conn *websocket.Conn,
	sessionID, text, lang, cookieProject string,
) bool {
	// 项目以会话所属为准:cookie 里的项目在连接建立时可能还没设置。
	info := service.GetSessionFullInfo(sessionID)
	if info == nil || info.ProjectPath == "" {
		slog.Warn("voice turn ws: session lookup failed", slog.String("session", sessionID))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "session not found"})
		return true
	}
	// 归属校验:只认会话自己所属的项目。帧里的 sessionId 由设备提供,不核对
	// 的话,任一登录设备都能拿别的项目的会话跑回合并取走摘要。
	if cookieProject != "" && info.ProjectPath != cookieProject {
		slog.Warn("voice turn ws: session belongs to another project",
			slog.String("session", sessionID),
			slog.String("project", cookieProject))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "session not found"})
		return true
	}
	// 群聊由编排器驱动(主持人/自由模式),一条语音回合只对应一个发言人;
	// 在这里直接跑单聊会把群行当普通会话,成员不会发言、也没有路由卡片。
	if info.SessionType == "group" {
		slog.Warn("voice turn ws: group session is not supported", slog.String("session", sessionID))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "group session not supported"})
		return true
	}
	projectPath := info.ProjectPath

	if err := writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: speechTypePhase, speechTypePhase: speechPhaseThink}); err != nil {
		return false
	}

	// 与常规发消息一样先占用会话:它让会话显示为运行中、让这一轮可被停止,
	// 并挡住第二个并发回合。忙时等一会儿——设备看不到队列,也无法自己重试,
	// 「排队等会儿再答」对它没有意义,要么等到空位当场作答,要么明确回忙。
	runCtx, claimed := claimVoiceTurn(ctx, sessionID)
	if !claimed {
		slog.Warn("voice turn ws: session busy", slog.String("session", sessionID))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "session busy"})
		return true
	}

	// 设备只连这条语音 WS、不订阅聊天流,所以这句提问必须在这里自己落库并
	// 广播:RunTurn 只写助手占位行,不碰用户消息。少了这一步,网页端看不到
	// 用户气泡,刷新后这句提问也根本不存在。
	userMsgID, saveErr := service.AddChatMessage(projectPath, info.Backend, sessionID, "user", text, nil, false, "")
	if saveErr != nil {
		service.FinishSessionRun(sessionID)
		slog.Error("voice turn ws: save user message failed", slog.String("error", saveErr.Error()))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "save failed"})
		return true
	}
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID: userMsgID,
			Content:   text,
		},
	})

	res := service.RunTurn(service.TurnSpec{
		Ctx:         runCtx,
		Mode:        service.ModeInteractive,
		ProjectPath: projectPath,
		BackendName: info.Backend,
		SessionID:   sessionID,
		AgentID:     info.AgentID,
		ChatReq: service.BuildChatRequest(text, sessionID, projectPath, info.Backend,
			info.AgentID, "", "", "", "", "", false),
		// 不传工作目录时后端会继承服务器进程的 CWD,AI 读写的是服务器目录;
		// CLI 后端还会因为 cwd 不对找不到原会话。
		FileDir:         service.ResolveFileDir(projectPath),
		DrainOnFinalize: true,
	})
	service.FinishSessionRun(sessionID)
	emitVoiceTurnTerminal(sessionID, res)
	if res.Err != "" || res.MsgID == 0 {
		slog.Warn("voice turn ws: turn failed", slog.String("error", res.Err))
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "turn failed"})
		return true
	}
	voiceTurnSpeak(ctx, conn, projectPath, res.MsgID, lang)
	return true
}

// voiceTurnBusyWait bounds how long a voice turn waits for another turn on the
// same session to finish. A voice client cannot see a queue and cannot answer
// one later, so "queue it and reply when it is this message's turn" is not an
// option: either the session frees up and we speak the answer now, or the device
// is told the session is busy. A var so tests can shorten the wait.
var voiceTurnBusyWait = 30 * time.Second

const voiceTurnBusyPoll = 200 * time.Millisecond

// claimVoiceTurn takes the session's run slot, waiting for a concurrent turn to
// finish. TryClaimSessionRun is the same primitive the chat handler uses: it
// marks the session running, makes the turn cancellable, and refuses a second
// concurrent turn on the same session.
func claimVoiceTurn(ctx context.Context, sessionID string) (context.Context, bool) {
	deadline := time.Now().Add(voiceTurnBusyWait)
	for {
		if runCtx, ok := service.TryClaimSessionRun(sessionID); ok {
			return runCtx, true
		}
		if !time.Now().Before(deadline) {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(voiceTurnBusyPoll):
		}
	}
}

// emitVoiceTurnTerminal closes the turn on the chat timeline. runTurn never
// sends a terminal event (each entry point owns its own), so without this the
// web UI's bubble streams forever and no completion push goes out.
func emitVoiceTurnTerminal(sessionID string, res service.TurnResult) {
	switch {
	case res.CancelReason != "":
		// 用户从界面停掉了这一轮:CancelSession 已经广播过 cancelled,这里只
		// 补一条让尚未订阅的客户端也收到终态。
		ws.EmitToSession(sessionID, ai.StreamEvent{Type: "cancelled"})
	case res.Err == "":
		service.EmitTurnTerminal(sessionID)
	}
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
		case voiceTurnKindListen:
			sessionID = ctrl.SessionID
			if ctrl.Language != "" {
				lang = ctrl.Language
			}
		case voiceTurnKindReplay:
			return voiceTurnKindReplay, "", ctrl.SessionID, lang, ctrl.MessageID
		case voiceTurnKindSay:
			if ctrl.SessionID != "" {
				sessionID = ctrl.SessionID
			}
			if ctrl.Language != "" {
				lang = ctrl.Language
			}
			return "say", ctrl.Text, sessionID, lang, 0
		case sttWSEndCtl:
			return voiceTurnKindSay, voiceTurnTranscribe(ctx, buf.Bytes()), sessionID, lang, 0
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
	text, err := provider.Transcribe(ctx, bytes.NewReader(audio), model.ConfigInstance.STT.ActiveLanguage())
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
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "empty reply"})
		return
	}

	provider := GetSpeechProvider()
	cacheKey, speakerPrefix := ttsCacheKeyAndPrefix(text, "", lang, ttsProviderCacheIdentity(provider))
	mp3Path := filepath.Join(projectPath, ".clawbench", "generated", "tts", cacheKey+".mp3")

	summary, ok := ttsSummarize(ctx, GetSummarizer(), cacheKey, text, lang, messageID,
		"SummarizeFailed", true)
	if !ok {
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "summarize failed"})
		return
	}
	if err := writeSpeechFrame(ctx, conn, map[string]any{
		ttsWSType: ttsWSSummary, sttWSText: summary, "messageId": messageID,
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
		_ = writeSpeechFrame(ctx, conn, map[string]any{ttsWSType: strError, messageKey: "synthesize failed"})
		return
	}
	streamPCMFile(ctx, conn, mp3Path)
}
