package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// providerResponse is the wire shape of a provider. The API key is never
// returned — the client only learns whether one is stored, matching the Forge
// credentials contract.
type providerResponse struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Protocol   string   `json:"protocol"`
	BaseURL    string   `json:"baseUrl"`
	AuthMode   string   `json:"authMode"`
	HasKey     bool     `json:"hasKey"`
	AgentCount int      `json:"agentCount"`
	Agents     []string `json:"agents,omitempty"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	UpdatedAt  string   `json:"updatedAt,omitempty"`
}

func toProviderResponse(p *model.Provider, usage map[int64]int, agents []string) providerResponse {
	return providerResponse{
		ID:         p.ID,
		Name:       p.Name,
		Protocol:   p.Protocol,
		BaseURL:    p.BaseURL,
		AuthMode:   p.AuthMode,
		HasKey:     p.APIKey != "",
		AgentCount: usage[p.ID],
		Agents:     agents,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
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
	out := make([]providerResponse, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProviderResponse(p, usage, nil))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"providers": out})
}

type providerCreateRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	AuthMode string `json:"authMode"`
}

func serveProviderCreate(w http.ResponseWriter, r *http.Request) {
	var req providerCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := service.CreateProvider(model.Provider{
		Name:     req.Name,
		Protocol: req.Protocol,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
		AuthMode: req.AuthMode,
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
	switch {
	case path == "presets":
		serveProviderPresets(w, r)
		return
	case path == "test":
		serveProviderTest(w, r)
		return
	case path == "" || strings.Contains(path, "/"):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toProviderResponse(p, usage, names))
	case http.MethodPut, http.MethodPatch:
		serveProviderUpdate(w, r, id)
	case http.MethodDelete:
		serveProviderDelete(w, r, id)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

type providerUpdateRequest struct {
	Name        *string `json:"name"`
	Protocol    *string `json:"protocol"`
	BaseURL     *string `json:"baseUrl"`
	APIKey      *string `json:"apiKey"`
	AuthMode    *string `json:"authMode"`
	ClearAPIKey bool    `json:"clearApiKey"`
}

func serveProviderUpdate(w http.ResponseWriter, r *http.Request, id int64) {
	var req providerUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := service.UpdateProvider(id, service.ProviderPatch{
		Name:        req.Name,
		Protocol:    req.Protocol,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		AuthMode:    req.AuthMode,
		ClearAPIKey: req.ClearAPIKey,
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
	ID int64 `json:"id"`
}

// serveProviderTest performs a best-effort GET {base}/models against the saved
// provider. It is a diagnostic, not a validation gate: a provider that fails
// here may still work for chat (some gateways do not expose /models), so the
// response reports the outcome without changing any state.
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
	if p.BaseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, watchMsgError: "provider has no base URL"})
		return
	}

	models, status, probeErr := probeProviderModels(r.Context(), p)
	result := map[string]any{"ok": probeErr == nil, jsonKeyStatus: status}
	if probeErr != nil {
		result[watchMsgError] = probeErr.Error()
	} else {
		result["models"] = models
	}
	writeJSON(w, http.StatusOK, result)
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return nil, 0, err
		}
		switch p.Protocol {
		case model.ProviderProtocolAnthropic:
			if p.AuthMode == model.ProviderAuthBearer {
				req.Header.Set("Authorization", "Bearer "+p.APIKey)
			} else {
				req.Header.Set("x-api-key", p.APIKey)
			}
			req.Header.Set("anthropic-version", "2023-06-01")
		case model.ProviderProtocolGemini:
			req.Header.Set("x-goog-api-key", p.APIKey)
		default:
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}

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
