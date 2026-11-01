package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/summarize"
)

// providerResponse is the wire shape of a provider. It deliberately has no key
// field: every ordinary response (list, create, update) is secret-free by
// construction, so no path can leak a key by forgetting a flag.
const (
	jsonKeyModels = "models"
	// strRole is the chat-message role key both the OpenAI and the Anthropic
	// probe bodies carry.
	strRole = "role"
)

type providerResponse struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	BaseURL      string   `json:"baseUrl"`
	AuthMode     string   `json:"authMode"`
	PresetID     string   `json:"presetId,omitempty"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel,omitempty"`
	WireAPI      string   `json:"wireApi"`
	HasKey       bool     `json:"hasKey"`
	AgentCount   int      `json:"agentCount"`
	Agents       []string `json:"agents,omitempty"`
	CreatedAt    string   `json:"createdAt,omitempty"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
}

// providerRevealResponse is the one reply that carries the stored key, and only
// for GET /api/providers/{id}?reveal=1 — the explicit "show me what is stored"
// request behind the eye button in the edit dialog.
type providerRevealResponse struct {
	providerResponse
	APIKey string `json:"apiKey"`
}

func toProviderResponse(p *model.Provider, usage map[int64]int, agents []string) providerResponse {
	return providerResponse{
		ID:           p.ID,
		Name:         p.Name,
		Protocol:     p.Protocol,
		BaseURL:      p.BaseURL,
		AuthMode:     p.AuthMode,
		PresetID:     p.PresetID,
		Models:       append([]string{}, p.Models...),
		DefaultModel: p.DefaultModel,
		WireAPI:      p.WireAPI,
		HasKey:       p.APIKey != "",
		AgentCount:   usage[p.ID],
		Agents:       agents,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

// ServeProviders handles GET (list) and POST (create).
func ServeProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveProvidersList(w, r)
	case http.MethodPost:
		serveProviderCreate(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveProvidersList(w http.ResponseWriter, r *http.Request) {
	providers, err := service.ListProviders()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	usage, err := service.ProviderUsageCounts()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	agentNames, err := service.ProviderAgentNamesAll()
	if err != nil {
		// The names are decoration on the list card; a failure must not take
		// the whole provider list down with it.
		agentNames = nil
	}
	out := make([]providerResponse, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProviderResponse(p, usage, agentNames[p.ID]))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"providers": out})
}

type providerCreateRequest struct {
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	BaseURL      string   `json:"baseUrl"`
	APIKey       string   `json:"apiKey"`
	AuthMode     string   `json:"authMode"`
	PresetID     string   `json:"presetId"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
	WireAPI      string   `json:"wireApi"`
}

func serveProviderCreate(w http.ResponseWriter, r *http.Request) {
	var req providerCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := service.CreateProvider(model.Provider{
		Name:         req.Name,
		Protocol:     req.Protocol,
		BaseURL:      req.BaseURL,
		APIKey:       req.APIKey,
		AuthMode:     req.AuthMode,
		PresetID:     req.PresetID,
		Models:       req.Models,
		DefaultModel: req.DefaultModel,
		WireAPI:      req.WireAPI,
	})
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toProviderResponse(created, map[int64]int{}, nil))
}

// ServeProviderSubRoutes dispatches /api/providers/{id} and the fixed
// sub-resources (/presets, /test). http.ServeMux prefers the longer exact
// pattern, so the fixed routes never reach this handler.
func ServeProviderSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	switch path {
	case "presets":
		serveProviderPresets(w, r)
		return
	case "test":
		serveProviderTest(w, r)
		return
	}
	// /api/providers/{id}/models — the model catalog a provider exposes.
	if idPart, sub, found := strings.Cut(path, "/"); found {
		if sub != "models" {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
			return
		}
		id, err := strconv.ParseInt(idPart, 10, 64)
		if err != nil || id <= 0 {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
			return
		}
		serveProviderModels(w, r, id)
		return
	}
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := service.GetProvider(id)
		if err != nil {
			writeProviderError(w, r, err)
			return
		}
		usage, _ := service.ProviderUsageCounts()
		names, _ := service.ProviderAgentNames(id)
		resp := toProviderResponse(p, usage, names)
		w.Header().Set("Content-Type", "application/json")
		// `reveal=1` is the only path that carries the stored key: the request
		// is authenticated and explicit, and the value never appears in the
		// list or in a write echo.
		if r.URL.Query().Get("reveal") == "1" {
			//nolint:gosec // G117: returning the key is the whole point of reveal=1.
			_ = json.NewEncoder(w).Encode(providerRevealResponse{providerResponse: resp, APIKey: p.APIKey})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	case http.MethodPut, http.MethodPatch:
		serveProviderUpdate(w, r, id)
	case http.MethodDelete:
		serveProviderDelete(w, r, id)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

type providerUpdateRequest struct {
	Name         *string   `json:"name"`
	Protocol     *string   `json:"protocol"`
	BaseURL      *string   `json:"baseUrl"`
	APIKey       *string   `json:"apiKey"`
	AuthMode     *string   `json:"authMode"`
	PresetID     *string   `json:"presetId"`
	Models       *[]string `json:"models"`
	DefaultModel *string   `json:"defaultModel"`
	WireAPI      *string   `json:"wireApi"`
	ClearAPIKey  bool      `json:"clearApiKey"`
}

func serveProviderUpdate(w http.ResponseWriter, r *http.Request, id int64) {
	var req providerUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := service.UpdateProvider(id, service.ProviderPatch{
		Name:         req.Name,
		Protocol:     req.Protocol,
		BaseURL:      req.BaseURL,
		APIKey:       req.APIKey,
		AuthMode:     req.AuthMode,
		PresetID:     req.PresetID,
		Models:       req.Models,
		DefaultModel: req.DefaultModel,
		WireAPI:      req.WireAPI,
		ClearAPIKey:  req.ClearAPIKey,
	})
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	usage, _ := service.ProviderUsageCounts()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toProviderResponse(updated, usage, nil))
}

func serveProviderDelete(w http.ResponseWriter, r *http.Request, id int64) {
	if err := service.DeleteProvider(id); err != nil {
		writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProviderError maps the service sentinels to HTTP status + i18n keys.
func writeProviderError(w http.ResponseWriter, r *http.Request, err error) {
	var conflict *service.ProviderProtocolConflictError
	switch {
	case errors.Is(err, service.ErrProviderNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "ProviderNotFound")
	case errors.Is(err, service.ErrProviderNameTaken):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProviderNameTaken")
	case errors.Is(err, service.ErrProviderInUse):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProviderInUse")
	case errors.Is(err, service.ErrProviderUsedBySummary):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProviderUsedBySummary")
	case errors.Is(err, service.ErrProviderInvalid):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderInvalid", map[string]any{"Reason": err.Error()})
	case errors.As(err, &conflict):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProviderProtocolConflict",
			map[string]any{"Agents": strings.Join(conflict.AgentNames, ", ")})
	default:
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
	}
}

// ServeProviderPresets is registered separately so the settings page can offer
// a provider catalog without a round trip through /api/config.
func serveProviderPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"presets": model.ProviderPresets()})
}

type providerTestRequest struct {
	ID    int64  `json:"id"`
	Model string `json:"model"`
	// Unsaved values from the edit dialog. Empty means "use what is saved", so
	// an endpoint or key can be tested before it is written — the same rule
	// /api/config/test follows. Without them a user has to save a wrong key
	// before finding out it is wrong.
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	AuthMode string `json:"authMode"`
}

// serveProviderTest is a diagnostic, never a validation gate: the outcome is
// reported without changing any state.
//
//   - Without `model` it performs a best-effort GET {base}/models. A failure
//     here does not mean chat is broken — many gateways do not expose a
//     catalog.
//   - With `model` it sends one minimal chat request, which is the only thing
//     that answers "does this model actually work with this endpoint and key".
//     A gateway can list a model it cannot serve, and a model can be missing
//     from the list while working fine.
func serveProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	var req providerTestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := service.GetProvider(req.ID)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	if req.Protocol != "" && !model.ValidProtocol(strings.ToLower(strings.TrimSpace(req.Protocol))) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderInvalid",
			map[string]any{"Reason": "unknown protocol " + req.Protocol})
		return
	}
	if req.AuthMode != "" && !model.ValidAuthMode(strings.ToLower(strings.TrimSpace(req.AuthMode))) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderInvalid",
			map[string]any{"Reason": "unknown auth mode " + req.AuthMode})
		return
	}
	target := *p
	applyProviderTestOverrides(&target, req)
	if target.BaseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, watchMsgError: "provider has no base URL"})
		return
	}

	if req.Model != "" {
		reply, status, elapsed, probeErr := probeProviderModel(r.Context(), &target, req.Model)
		result := map[string]any{
			"ok":          probeErr == nil,
			jsonModel:     req.Model,
			jsonKeyStatus: status,
			"latencyMs":   elapsed.Milliseconds(),
		}
		if probeErr != nil {
			result[watchMsgError] = probeErr.Error()
		} else {
			result["reply"] = reply
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	models, status, probeErr := probeProviderModels(r.Context(), &target)
	result := map[string]any{"ok": probeErr == nil, jsonKeyStatus: status}
	if probeErr != nil {
		result[watchMsgError] = probeErr.Error()
	} else {
		result["models"] = models
	}
	writeJSON(w, http.StatusOK, result)
}

// applyProviderTestOverrides folds the dialog's unsaved fields into the probe
// target. Empty values keep the saved row: the form cannot distinguish "cleared"
// from "untouched", and testing a cleared key is not a case worth supporting.
func applyProviderTestOverrides(p *model.Provider, req providerTestRequest) {
	if v := strings.TrimSpace(req.Protocol); v != "" {
		p.Protocol = strings.ToLower(v)
	}
	if v := strings.TrimSpace(req.BaseURL); v != "" {
		p.BaseURL = v
	}
	if v := strings.TrimSpace(req.APIKey); v != "" {
		p.APIKey = v
	}
	if v := strings.TrimSpace(req.AuthMode); v != "" {
		p.AuthMode = strings.ToLower(v)
	}
}

// applyProviderAuth sets the credential header this provider's protocol and
// auth mode require. Shared by both probes so the header a test sends is
// exactly the one a real call sends — the two used to be written twice, and a
// gateway that only accepts one of the two headers turns that into a test that
// passes while every turn fails.
func applyProviderAuth(req *http.Request, p *model.Provider) {
	switch p.Protocol {
	case model.ProviderProtocolAnthropic:
		if p.AuthMode == model.ProviderAuthBearer {
			if p.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		} else if p.APIKey != "" {
			req.Header.Set("x-api-key", p.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case model.ProviderProtocolGemini:
		if p.AuthMode == model.ProviderAuthBearer {
			if p.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		} else if p.APIKey != "" {
			req.Header.Set("x-goog-api-key", p.APIKey)
		}
	default:
		if p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
	}
}

// providerProbePrompt is deliberately trivial: the probe measures whether the
// model answers at all, not how well it reasons, and a longer prompt would only
// add latency and cost to every click.
const providerProbePrompt = "Reply with the single word: ok"

// providerChatProbeTimeout bounds one model test. Generous compared with the
// catalog probe because this one waits for a generated token, and a reasoning
// model can take several seconds before its first output.
const providerChatProbeTimeout = 30 * time.Second

// probeProviderModel sends one minimal chat request and reports what came back.
// It speaks the provider's own wire protocol, so a model that only exists in
// the catalog (or a key that only works for /models) is caught here.
func probeProviderModel(ctx context.Context, p *model.Provider, modelID string) (string, int, time.Duration, error) {
	req, err := buildProviderProbeRequest(ctx, p, modelID)
	if err != nil {
		return "", 0, 0, err
	}
	client := &http.Client{Timeout: providerChatProbeTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return "", 0, elapsed, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, elapsed, providerProbeError(resp.StatusCode, body)
	}
	return clipProbeText(providerReplyText(body), 120), resp.StatusCode, elapsed, nil
}

func buildProviderProbeRequest(ctx context.Context, p *model.Provider, modelID string) (*http.Request, error) {
	var (
		endpoint string
		payload  map[string]any
	)
	switch p.Protocol {
	case model.ProviderProtocolAnthropic:
		endpoint = summarize.BuildEndpointURL(p.BaseURL, summarize.AnthropicMessagesPath)
		// max_tokens is mandatory for the Messages API.
		payload = map[string]any{
			jsonModel:    modelID,
			"max_tokens": 16,
			strMessages:  []map[string]string{{strRole: strUser, strContent: providerProbePrompt}},
		}
	case model.ProviderProtocolGemini:
		endpoint = geminiGenerateURL(p.BaseURL, modelID)
		payload = map[string]any{
			"contents":         []map[string]any{{"parts": []map[string]string{{liveNoteTextKey: providerProbePrompt}}}},
			"generationConfig": map[string]any{"maxOutputTokens": 16},
		}
	default:
		endpoint = summarize.BuildEndpointURL(p.BaseURL, summarize.OpenAIChatCompletionsPath)
		// No max_tokens: the o-series and gpt-5 models reject it outright
		// ("Unsupported parameter"), which would fail a perfectly working
		// model. The prompt is short enough to bound the answer.
		payload = map[string]any{
			jsonModel:   modelID,
			strMessages: []map[string]string{{strRole: strUser, strContent: providerProbePrompt}},
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyProviderAuth(req, p)
	return req, nil
}

// geminiGenerateURL resolves the generateContent endpoint for one model.
// Gemini's path carries the model, so summarize.BuildEndpointURL (which appends
// a fixed path) cannot be used here.
func geminiGenerateURL(baseURL, modelID string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return ""
	}
	id := url.PathEscape(strings.TrimPrefix(modelID, "models/"))
	// A URL that already names a model is rewritten rather than used verbatim:
	// the probe must exercise the model that was clicked, not the one baked
	// into the endpoint.
	if idx := strings.LastIndex(base, "/models/"); idx >= 0 {
		return base[:idx] + "/models/" + id + ":generateContent"
	}
	parsed, err := url.Parse(base)
	if err == nil {
		switch path := strings.TrimRight(parsed.Path, "/"); {
		case path == "" || path == "/":
			return base + "/v1beta/models/" + id + ":generateContent"
		case isGeminiVersionPath(path):
			return base + "/models/" + id + ":generateContent"
		}
	}
	// A gateway mounted under its own namespace still expects the versioned
	// path after it, exactly as BuildEndpointURL does for the other protocols.
	return base + "/v1beta/models/" + id + ":generateContent"
}

func isGeminiVersionPath(path string) bool {
	switch path[strings.LastIndex(path, "/")+1:] {
	case "v1", "v1beta", "v1alpha":
		return true
	}
	return false
}

// providerReplyText pulls the assistant text out of any of the three response
// shapes. An empty answer is not an error — a model capped at a few tokens can
// legitimately return nothing — so callers must not treat "" as a failure.
func providerReplyText(body []byte) string {
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	var out strings.Builder
	for _, c := range payload.Choices {
		out.WriteString(c.Message.Content)
	}
	for _, c := range payload.Content {
		out.WriteString(c.Text)
	}
	for _, c := range payload.Candidates {
		for _, part := range c.Content.Parts {
			out.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(out.String())
}

// providerProbeError turns a non-2xx body into a one-line cause. The envelope
// differs by vendor ({"error":{"message":…}} for OpenAI/Anthropic/Gemini,
// {"error":"…"} and {"message":…} for gateways), so all three are tried before
// falling back to the raw body — inventing a message would hide the reason the
// user is trying to read.
func providerProbeError(status int, body []byte) error {
	var payload struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	msg := ""
	if json.Unmarshal(body, &payload) == nil {
		if len(payload.Error) > 0 {
			var asObject struct {
				Message string `json:"message"`
			}
			var asString string
			switch {
			case json.Unmarshal(payload.Error, &asObject) == nil && asObject.Message != "":
				msg = asObject.Message
			case json.Unmarshal(payload.Error, &asString) == nil:
				msg = asString
			}
		}
		if msg == "" {
			msg = payload.Message
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("HTTP %d: %s", status, clipProbeText(msg, 300))
}

// clipProbeText collapses whitespace and truncates, so an HTML error page or a
// verbose vendor message cannot push the rest of the dialog off screen.
func clipProbeText(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func probeProviderModels(ctx context.Context, p *model.Provider) ([]string, int, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	candidates := []string{base + "/models"}
	if !strings.HasSuffix(base, "/v1") {
		candidates = append(candidates, base+"/v1/models")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	lastStatus := 0
	for _, url := range candidates {
		//nolint:gosec // G704: the URL is the user-configured provider endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return nil, 0, err
		}
		applyProviderAuth(req, p)

		//nolint:gosec // G704: the URL is the user-configured provider endpoint
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return parseProviderModelIDs(body), resp.StatusCode, nil
		}
		lastErr = errors.New(strings.TrimSpace(string(body)))
		if lastErr.Error() == "" {
			lastErr = errors.New(resp.Status)
		}
	}
	return nil, lastStatus, lastErr
}

// parseProviderModelIDs accepts both OpenAI ({"data":[{"id":...}]}) and Gemini
// ({"models":[{"name":...}]}) shapes, and tolerates anything else by returning
// an empty list (the probe still counts as reachable).
func parseProviderModelIDs(body []byte) []string {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	ids := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, m := range payload.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	for _, m := range payload.Models {
		if m.Name != "" {
			ids = append(ids, m.Name)
		}
	}
	return ids
}

// serveProviderModels returns the model ids a saved provider advertises. It is
// the data source for the summary-model picker, so a gateway that does not
// publish /models yields an empty list plus the reason — the caller falls back
// to a free-text input rather than blocking configuration.
func serveProviderModels(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	p, err := service.GetProvider(id)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	if len(p.Models) > 0 {
		// The provider curates its own list; it wins over the probe so the
		// picker cannot drift from what the user saved.
		writeJSON(w, http.StatusOK, map[string]any{jsonKeyModels: p.Models, "source": "curated", jsonKeyStatus: http.StatusOK})
		return
	}
	if p.BaseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{jsonKeyModels: []string{}})
		return
	}
	models, status, probeErr := probeProviderModels(r.Context(), p)
	source := "live"
	if len(models) == 0 {
		// The endpoint does not publish a catalog (many Anthropic-compatible
		// gateways do not). Fall back to the authoritative snapshot so the
		// picker still has something to offer; the caller sees which it got.
		if cp := model.CatalogProviderForPreset(p.PresetID); cp != nil {
			for _, m := range cp.Models {
				models = append(models, m.ID)
			}
			if len(models) > 0 {
				source = "catalog"
			}
		}
	}
	result := map[string]any{jsonKeyModels: models, jsonKeyStatus: status, "source": source}
	if probeErr != nil && len(models) == 0 {
		result[watchMsgError] = probeErr.Error()
	}
	writeJSON(w, http.StatusOK, result)
}
