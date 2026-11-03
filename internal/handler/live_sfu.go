package handler

// Relay (LiveKit) configuration endpoints.
//
// The relay is a property of this INSTALLATION, not of a meeting: one endpoint,
// one credential pair, shared by every meeting that opts into it. The create
// dialog is where it is edited because that is where the decision to use it is
// made — but the values apply to all of them, which the UI says out loud.
//
// The secret is write-only. No response below ever carries it; the client is
// told `hasSecret` and nothing more.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/service"
)

type liveSFUResponse struct {
	URL        string `json:"url"`
	HasSecret  bool   `json:"hasSecret"`
	Configured bool   `json:"configured"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

type liveSFUPatchBody struct {
	URL string `json:"url"`
	// Nil means "leave what is stored". The UI never receives the secret, so a
	// save that only changes the URL must not blank the credential.
	APIKey    *string `json:"apiKey"`
	APISecret *string `json:"apiSecret"`
	Clear     bool    `json:"clear"`
}

func toLiveSFUResponse(c *service.LiveSFUConfig) liveSFUResponse {
	return liveSFUResponse{
		URL:        c.URL,
		HasSecret:  c.APIKey != "",
		Configured: c.Configured(),
		UpdatedAt:  c.UpdatedAt,
	}
}

// ServeLiveSFUConfig handles GET (read, secret-free) and PATCH (edit).
func ServeLiveSFUConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := service.GetLiveSFUConfig()
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusOK, toLiveSFUResponse(cfg))
	case http.MethodPatch:
		var body liveSFUPatchBody
		if !decodeJSON(w, r, &body) {
			return
		}
		cfg, err := service.SaveLiveSFUConfig(service.LiveSFUPatch{
			URL:       body.URL,
			APIKey:    body.APIKey,
			APISecret: body.APISecret,
			Clear:     body.Clear,
		})
		if err != nil {
			if errors.Is(err, service.ErrLiveSFUInvalid) {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest", map[string]any{"Reason": err.Error()})
				return
			}
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusOK, toLiveSFUResponse(cfg))
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// liveSFUTestBody carries DRAFT values: the point of the button is to find out
// whether they work before they are saved.
//
// An empty apiKey/apiSecret falls back to what is stored, so re-testing after a
// URL change does not force the operator to paste the secret again.
type liveSFUTestBody struct {
	URL       string `json:"url"`
	APIKey    string `json:"apiKey"`
	APISecret string `json:"apiSecret"`
}

// ServeLiveSFUTest probes a relay endpoint and answers in the same shape as
// /api/config/test-connectivity: {success, message}.
//
// It checks reachability AND credentials in one call (ListRooms). A bare TCP
// probe would report success with a wrong secret, and the failure would only
// surface at the first meeting — as "cannot join", with no hint why.
func ServeLiveSFUTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	var body liveSFUTestBody
	if !decodeJSON(w, r, &body) {
		return
	}
	stored, err := service.GetLiveSFUConfig()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	url := strings.TrimSpace(body.URL)
	if url == "" {
		url = stored.URL
	}
	key := strings.TrimSpace(body.APIKey)
	if key == "" {
		key = stored.APIKey
	}
	secret := strings.TrimSpace(body.APISecret)
	if secret == "" {
		secret = stored.APISecret
	}
	if url == "" || key == "" || secret == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": "中继地址、API Key、API Secret 都需要填写",
		})
		return
	}

	cfg := liveshare.LiveKitConfig{
		URL:       url,
		APIURL:    service.LiveSFUConfig{URL: url}.APIBaseURL(),
		APIKey:    key,
		APISecret: secret,
	}
	ctx, cancel := context.WithTimeout(r.Context(), relayProbeTimeout)
	defer cancel()

	rooms, err := cfg.ListRooms(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": describeRelayError(url, err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("连接成功，当前 %d 个房间", rooms),
	})
}

// relayProbeTimeout bounds the button. Long enough for a slow link, short
// enough that a black-holed address reports a failure instead of hanging the
// dialog forever.
const relayProbeTimeout = 10 * time.Second

// describeRelayError turns the client's two error kinds into something a user
// can act on: "cannot reach" and "reached but refused" need different fixes.
func describeRelayError(url string, err error) string {
	switch {
	case errors.Is(err, liveshare.ErrRelayUnreachable):
		return "无法连接 " + url + "：" + err.Error()
	case errors.Is(err, liveshare.ErrRelayRejected):
		return "LiveKit 拒绝了请求（地址可达，但密钥或权限不对）：" + err.Error()
	default:
		return "连接失败：" + err.Error()
	}
}
