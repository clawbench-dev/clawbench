package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// ACPHTTPTransport implements ACP communication over HTTP+SSE for daemon-mode agents.
// It bypasses the acp-go-sdk (which only supports stdio) and implements
// JSON-RPC 2.0 directly over HTTP.
type ACPHTTPTransport struct {
	baseURL    string
	headers    map[string]string
	httpClient *http.Client
	connID     string // from /connect response
	nextID     int
}

// acpConnectResponse is the response from the /connect endpoint.
type acpConnectResponse struct {
	ConnectionID string `json:"connectionId"`
	SessionToken string `json:"sessionToken"`
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCNotification is a JSON-RPC 2.0 notification (no ID).
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewACPHTTPTransport creates a new HTTP transport for the given daemon URL.
func NewACPHTTPTransport(baseURL string, headers map[string]string) *ACPHTTPTransport {
	return &ACPHTTPTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // long timeout for prompt requests
		},
	}
}

// Connect establishes a connection to the HTTP daemon (daemon-specific step).
// Not all ACP agents require this; CodeBuddy does.
func (t *ACPHTTPTransport) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/api/v1/acp/connect", nil)
	if err != nil {
		return fmt.Errorf("acp http connect: %w", err)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("acp http connect: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("acp http connect: status %d: %s", resp.StatusCode, string(body))
	}

	var connResp acpConnectResponse
	if err := json.NewDecoder(resp.Body).Decode(&connResp); err != nil {
		return fmt.Errorf("acp http connect: decode: %w", err)
	}

	t.connID = connResp.ConnectionID
	slog.Info("acp http: connected to daemon", "conn_id", t.connID)
	return nil
}

// Initialize performs the ACP initialize handshake.
func (t *ACPHTTPTransport) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": acp.ProtocolVersionNumber,
		"clientInfo": map[string]string{
			"name":    "clawbench",
			"version": "1.0.0",
		},
		"clientCapabilities": map[string]any{
			"fs": map[string]bool{
				"readTextFile":  true,
				"writeTextFile": true,
			},
			"terminal": true,
		},
	}
	_, err := t.sendRequest(ctx, "initialize", params)
	return err
}

// NewSession creates a new ACP session via HTTP.
// Returns the ACP session ID.
func (t *ACPHTTPTransport) NewSession(ctx context.Context, cwd string) (string, *ModeState, *ConfigOptionState, error) {
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}

	result, err := t.sendRequestReadResult(ctx, "session/new", params)
	if err != nil {
		return "", nil, nil, err
	}

	// Parse session ID and optional mode/config state from result
	var sessResp struct {
		SessionId string `json:"sessionId"`
		Modes     *struct {
			CurrentModeId  string `json:"currentModeId"`
			AvailableModes []struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			} `json:"availableModes"`
		} `json:"modes"`
		ConfigOptions []struct {
			Id       string `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category"`
			Values   []struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			} `json:"values"`
		} `json:"configOptions"`
	}
	if err := json.Unmarshal(result, &sessResp); err != nil {
		// Some agents return sessionId at the top level of result
		var raw map[string]json.RawMessage
		if json.Unmarshal(result, &raw) == nil {
			if sidBytes, ok := raw["sessionId"]; ok {
				_ = json.Unmarshal(sidBytes, &sessResp.SessionId)
			}
		}
	}

	// Extract mode state
	var modeState *ModeState
	if sessResp.Modes != nil && (sessResp.Modes.CurrentModeId != "" || len(sessResp.Modes.AvailableModes) > 0) {
		modeState = &ModeState{
			CurrentModeID: sessResp.Modes.CurrentModeId,
		}
		for _, m := range sessResp.Modes.AvailableModes {
			modeState.AvailableModes = append(modeState.AvailableModes, ModeDef{ID: m.Id, Name: m.Name})
		}
	}

	// Extract config option state (mode category only)
	var configState *ConfigOptionState
	for _, opt := range sessResp.ConfigOptions {
		if opt.Category != "mode" && opt.Id != "mode" {
			continue
		}
		configState = &ConfigOptionState{
			ConfigID: opt.Id,
		}
		optDef := ConfigOptionDef{
			ID:       opt.Id,
			Name:     opt.Name,
			Category: opt.Category,
		}
		for _, v := range opt.Values {
			optDef.Values = append(optDef.Values, ConfigOptionValue{ID: v.Id, Name: v.Name})
		}
		configState.Options = append(configState.Options, optDef)
		break // only first mode-relevant option
	}

	return sessResp.SessionId, modeState, configState, nil
}

// ResumeSession resumes an existing ACP session.
func (t *ACPHTTPTransport) ResumeSession(ctx context.Context, sessionID string) error {
	params := map[string]any{
		"sessionId": sessionID,
	}
	_, err := t.sendRequest(ctx, "session/resume", params)
	return err
}

// SetSessionConfigOption sets a config option (e.g., model, mode).
func (t *ACPHTTPTransport) SetSessionConfigOption(ctx context.Context, sessionID string, option acp.SetSessionConfigOptionRequest) error {
	_, err := t.sendRequest(ctx, "session/set_config_option", option)
	return err
}

// Prompt sends a prompt request and streams the response via SSE.
// It blocks until the prompt turn completes. StreamEvents are sent to streamCh.
// Returns the stop reason from the prompt response.
func (t *ACPHTTPTransport) Prompt(ctx context.Context, sessionID string, prompt []acp.ContentBlock, streamCh chan<- StreamEvent) (string, error) {
	params := map[string]any{
		"sessionId": sessionID,
		"prompt":    prompt,
	}

	reqBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      t.nextID,
		Method:  "session/prompt",
		Params:  params,
	})
	t.nextID++
	if err != nil {
		return "", fmt.Errorf("acp http prompt: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/api/v1/acp", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("acp http prompt: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("acp http prompt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("acp http prompt: status %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	stopReason := ""
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and SSE comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Only process "data:" lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var rpcResp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
			slog.Debug("acp http: failed to parse SSE data", "data", data)
			continue
		}

		// Check for JSON-RPC error
		if rpcResp.Error != nil {
			forwardACPEvent(streamCh, mapACPError(rpcResp.Error.Code, rpcResp.Error.Message))
			return rpcResp.Error.Message, nil
		}

		// Process result — could be a response or a notification
		if rpcResp.Result != nil {
			// Parse as session/update notification embedded in result
			var update map[string]json.RawMessage
			if err := json.Unmarshal(rpcResp.Result, &update); err == nil {
				if typeBytes, ok := update["type"]; ok {
					var updateType string
					_ = json.Unmarshal(typeBytes, &updateType)

					switch updateType {
					case "agent_message_chunk":
						var chunk struct {
							Content string `json:"content"`
						}
						if json.Unmarshal(rpcResp.Result, &chunk) == nil && chunk.Content != "" {
							forwardACPEvent(streamCh, StreamEvent{Type: "content", Content: chunk.Content})
						}
					case "agent_thought_chunk":
						var chunk struct {
							Content string `json:"content"`
						}
						if json.Unmarshal(rpcResp.Result, &chunk) == nil && chunk.Content != "" {
							forwardACPEvent(streamCh, StreamEvent{Type: "thinking", Content: chunk.Content})
						}
					case "tool_call":
						var tc struct {
							ToolCall struct {
								ID    string         `json:"id"`
								Title string         `json:"title"`
								Input map[string]any `json:"input"`
							} `json:"toolCall"`
						}
						if json.Unmarshal(rpcResp.Result, &tc) == nil {
							inputJSON, _ := json.Marshal(tc.ToolCall.Input)
							forwardACPEvent(streamCh, StreamEvent{
								Type: "tool_use",
								Tool: &ToolCall{
									Name:  tc.ToolCall.Title,
									ID:    tc.ToolCall.ID,
									Input: string(inputJSON),
									Done:  false,
								},
							})
						}
					case "tool_call_update":
						var tcu struct {
							ToolCallID string `json:"toolCallId"`
							Status     string `json:"status"`
							Output     string `json:"output"`
						}
						if json.Unmarshal(rpcResp.Result, &tcu) == nil {
							done := tcu.Status == "completed" || tcu.Status == "failed"
							status := ""
							if tcu.Status == "completed" {
								status = "success"
							} else if tcu.Status == "failed" {
								status = "error"
							}
							// Match stdio path: tool_use for in-progress, tool_result for completed
							eventType := "tool_use"
							if done {
								eventType = "tool_result"
							}
							forwardACPEvent(streamCh, StreamEvent{
								Type: eventType,
								Tool: &ToolCall{
									ID:     tcu.ToolCallID,
									Output: truncateToolOutput(tcu.Output),
									Status: status,
									Done:   done,
								},
							})
						}
					case "current_mode_update":
						var modeUpd struct {
							CurrentModeId  string `json:"currentModeId"`
							AvailableModes []struct {
								Id   string `json:"id"`
								Name string `json:"name"`
							} `json:"availableModes"`
						}
						if json.Unmarshal(rpcResp.Result, &modeUpd) == nil {
							modeState := &ModeState{
								CurrentModeID: modeUpd.CurrentModeId,
							}
							for _, m := range modeUpd.AvailableModes {
								modeState.AvailableModes = append(modeState.AvailableModes, ModeDef{ID: m.Id, Name: m.Name})
							}
							forwardACPEvent(streamCh, StreamEvent{Type: "mode_update", Mode: modeState})
						}
					case "config_option_update":
						var configUpd struct {
							ConfigId        string `json:"configId"`
							CurrentValueId  string `json:"currentValueId"`
							Options         []struct {
								Id       string `json:"id"`
								Name     string `json:"name"`
								Category string `json:"category"`
								Values   []struct {
									Id   string `json:"id"`
									Name string `json:"name"`
								} `json:"values"`
							} `json:"options"`
						}
						if json.Unmarshal(rpcResp.Result, &configUpd) == nil {
							configState := &ConfigOptionState{
								ConfigID:  configUpd.ConfigId,
								CurrentID: configUpd.CurrentValueId,
							}
							for _, opt := range configUpd.Options {
								optDef := ConfigOptionDef{
									ID:       opt.Id,
									Name:     opt.Name,
									Category: opt.Category,
								}
								for _, v := range opt.Values {
									optDef.Values = append(optDef.Values, ConfigOptionValue{ID: v.Id, Name: v.Name})
								}
								configState.Options = append(configState.Options, optDef)
							}
							forwardACPEvent(streamCh, StreamEvent{Type: "config_update", Config: configState})
						}
					case "session_end":
						var end struct {
							Reason string `json:"reason"`
						}
						if json.Unmarshal(rpcResp.Result, &end) == nil {
							stopReason = end.Reason
						}
					}
				} else {
					// Result might be the final PromptResponse
					var promptResp struct {
						StopReason string `json:"stopReason"`
					}
					if json.Unmarshal(rpcResp.Result, &promptResp) == nil && promptResp.StopReason != "" {
						stopReason = promptResp.StopReason
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("acp http: SSE stream error", "error", err)
	}

	return stopReason, nil
}

// Cancel sends a session/cancel notification.
func (t *ACPHTTPTransport) Cancel(ctx context.Context, sessionID string) error {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  "session/cancel",
		Params: map[string]string{"sessionId": sessionID},
	}
	return t.sendNotification(ctx, notif)
}

// Close sends a session/close request and cleans up.
func (t *ACPHTTPTransport) Close(ctx context.Context) error {
	if t.connID == "" {
		return nil
	}
	// Best-effort close
	_, _ = t.sendRequest(ctx, "session/close", map[string]string{"connectionId": t.connID})
	return nil
}

// sendRequest sends a JSON-RPC request and reads the full response.
func (t *ACPHTTPTransport) sendRequest(ctx context.Context, method string, params any) (*jsonRPCResponse, error) {
	result, err := t.sendRequestRaw(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// sendRequestReadResult sends a JSON-RPC request and returns the raw result bytes.
func (t *ACPHTTPTransport) sendRequestReadResult(ctx context.Context, method string, params any) (json.RawMessage, error) {
	resp, err := t.sendRequestRaw(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("acp http %s: error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// sendRequestRaw sends a JSON-RPC request and parses the response.
func (t *ACPHTTPTransport) sendRequestRaw(ctx context.Context, method string, params any) (*jsonRPCResponse, error) {
	id := t.nextID
	t.nextID++

	reqBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("acp http %s: marshal: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/api/v1/acp", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("acp http %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	// Use shorter timeout for non-prompt requests
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acp http %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("acp http %s: status %d: %s", method, resp.StatusCode, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("acp http %s: decode: %w", method, err)
	}

	return &rpcResp, nil
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (t *ACPHTTPTransport) sendNotification(ctx context.Context, notif jsonRPCNotification) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("acp http notify %s: marshal: %w", notif.Method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/api/v1/acp", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("acp http notify %s: %w", notif.Method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("acp http notify %s: %w", notif.Method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("acp http: notification failed", "method", notif.Method, "status", resp.StatusCode, "body", string(respBody))
	}
	return nil
}

// HealthCheck performs a GET request to the health endpoint.
func (t *ACPHTTPTransport) HealthCheck(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// String returns a debug description.
func (t *ACPHTTPTransport) String() string {
	return "acp-http:" + t.baseURL
}
