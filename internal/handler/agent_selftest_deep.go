package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

// Deep-mode constants. The prompt is deliberately trivial: the point is to
// exercise the transport, not to evaluate the model, so a one-word question
// keeps latency and token cost at the floor while still proving the whole path
// (spawn → handshake → prompt → streamed reply → finalize) works.
const (
	selfTestDeepPrompt       = "Reply with exactly: OK"
	selfTestDeepTimeout      = 90 * time.Second
	selfTestDeepSessionTitle = "Agent self-test"
)

// selfTestDeepResult is the outcome of one real round trip.
type selfTestDeepResult struct {
	// Reply is the assistant text the model produced (empty on failure).
	Reply string `json:"reply,omitempty"`
	// Transport is the transport the turn actually used ("acp-stdio" / "cli"),
	// read back from the persisted metadata — the ground truth, not the config.
	Transport string `json:"transport,omitempty"`
	// StopReason is the ACP/CLI stop reason (e.g. "end_turn").
	StopReason string `json:"stopReason,omitempty"`
	// DurationMs is the wall time of the whole turn.
	DurationMs int64 `json:"durationMs"`
	// SessionID is the throwaway session the turn ran in (deleted afterwards).
	SessionID string `json:"-"`
}

// runDeepSelfTest performs one REAL turn and reports what came back.
//
// Why this exists in addition to the static checks: those prove the
// configuration is self-consistent, but they cannot prove the agent actually
// starts, completes a handshake, accepts a prompt and streams a reply. Only a
// turn does. It is opt-in (`deep: true`) because it costs tokens and time.
//
// Everything is torn down afterwards — session, chat rows, RAG chunks and the
// ACP connection — so a self-test never leaves a conversation behind in the
// user's session list. Cleanup is best-effort but every step is attempted.
func runDeepSelfTest(ctx context.Context, agent *model.Agent, projectPath string) (selfTestDeepResult, error) {
	var out selfTestDeepResult
	start := time.Now()

	// A dedicated throwaway session keeps the test out of the user's real
	// conversations, and gives the agent a session-scoped connection that can be
	// closed without disturbing any live one.
	sessionID, err := service.CreateSession(
		projectPath, agent.Backend, selfTestDeepSessionTitle, agent.ID, "", "default", store.SessionTypeChat,
	)
	if err != nil {
		return out, fmt.Errorf("create self-test session: %w", err)
	}
	out.SessionID = sessionID
	defer func() {
		// Close the agent-side process first: an open connection would keep the
		// session alive in the pool even after the rows are gone.
		ai.GetACPConnManager().CloseConn(sessionID)
		if _, perr := service.PurgeRAGChunksBySessionIDs([]string{sessionID}); perr != nil {
			// Best-effort: the rows are about to be deleted anyway.
			_ = perr
		}
		_ = service.HardDeleteSession(sessionID)
	}()

	// No summarize/recommend: this session is deleted immediately, so any extra
	// LLM call would be pure waste.
	req := service.BuildChatRequest(
		selfTestDeepPrompt, sessionID, projectPath, agent.Backend, agent.ID,
		"", "", "", "", service.ResolveFileDir(projectPath), false,
	)

	turnCtx, cancel := context.WithTimeout(ctx, selfTestDeepTimeout)
	defer cancel()

	res := service.RunTurn(service.TurnSpec{
		Ctx:                   turnCtx,
		Mode:                  service.ModeInteractive,
		ProjectPath:           projectPath,
		BackendName:           agent.Backend,
		SessionID:             sessionID,
		AgentID:               agent.ID,
		ChatReq:               req,
		FileDir:               service.ResolveFileDir(projectPath),
		DrainOnFinalize:       true,
		SuppressSummarization: true,
	})
	out.DurationMs = time.Since(start).Milliseconds()

	// The turn is the ground truth for everything below, so report what it
	// actually did rather than what the config said it would do.
	if res.Err != "" {
		return out, fmt.Errorf("%s", res.Err)
	}
	if res.CancelReason != "" {
		return out, fmt.Errorf("the turn was cancelled (%s)", res.CancelReason)
	}
	if !res.ReceivedTerminal {
		return out, fmt.Errorf("the agent closed the stream without a terminal event (it may have crashed)")
	}
	if res.Empty {
		return out, fmt.Errorf("the agent returned no content")
	}

	// Read the finalized assistant row: it carries both the text and the turn
	// metadata, and unlike the stream it survives any event the WS layer dropped.
	msgs, herr := service.GetChatHistory(projectPath, agent.Backend, sessionID)
	if herr != nil {
		return out, fmt.Errorf("read self-test history: %w", herr)
	}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		reply, transport, stop := parseSelfTestReply(m.Content)
		if reply == "" {
			continue
		}
		out.Reply = reply
		out.Transport = transport
		out.StopReason = stop
	}

	if out.Reply == "" {
		return out, fmt.Errorf("the agent produced no assistant text")
	}
	return out, nil
}

// parseSelfTestReply extracts the text, transport and stop reason from a
// persisted assistant content document.
//
// Content is `{"blocks":[...],"metadata":{...}}`. The summary pass can replace
// blocks with [] once a reading summary exists, but this session is never
// summarized (SuppressSummarization), so blocks are always present here — the
// metadata is still read from the same place to keep one parser.
func parseSelfTestReply(content string) (reply, transport, stopReason string) {
	var doc struct {
		Blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"blocks"`
		Metadata struct {
			Transport  string `json:"transport"`
			StopReason string `json:"stopReason"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", "", ""
	}
	var b strings.Builder
	for _, block := range doc.Blocks {
		// Only real text counts: a warning/error block means the turn failed,
		// and treating its text as a "reply" would make a broken agent pass.
		if block.Type == sttWSText {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String()), doc.Metadata.Transport, doc.Metadata.StopReason
}

// ServeAgentSelfTestDeep handles POST /api/agents/{id}/self-test-deep.
//
// Separate route (not a flag on the static endpoint) so a client can never
// accidentally spend money by setting a field, and so the two have distinct
// timeouts and rate expectations.
func ServeAgentSelfTestDeep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/self-test-deep")
	if !isValidAgentID(agentID) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.RLock()
	agent := model.GetAgent(agentID)
	configMutex.RUnlock()
	if agent == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoProjectSelected")
		return
	}

	// Static checks first: running a real turn on an agent whose command does not
	// exist wastes the full timeout to learn what one LookPath already knows.
	static := runAgentSelfTest(r.Context(), agent, projectPath)
	if static.Failed > 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			strReqError: "The configuration checks failed; fix them before running a real turn.",
			"static":    static,
		})
		return
	}

	deep, err := runDeepSelfTest(r.Context(), agent, projectPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			strReqError: err.Error(),
			"deep":      deep,
			"static":    static,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"deep":   deep,
		"static": static,
	})
}
