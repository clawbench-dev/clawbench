package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateProbe records whether the wrapped handler ran.
func gateProbe() (http.HandlerFunc, *bool) {
	ran := false
	return func(w http.ResponseWriter, _ *http.Request) {
		ran = true
		w.WriteHeader(http.StatusOK)
	}, &ran
}

func TestGate_RejectsWhenDisabled(t *testing.T) {
	p := &Plugin{ID: "mail", Enabled: func() bool { return false }, APIPrefixes: []string{"/api/mail/"}}
	next, ran := gateProbe()

	var gotStatus int
	var gotKey string
	h := Gate(p, next, func(w http.ResponseWriter, _ *http.Request, status int, msgKey string) {
		gotStatus, gotKey = status, msgKey
		w.WriteHeader(status)
	})

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/mail/send", http.NoBody))

	assert.False(t, *ran, "the handler must not run while the feature is off")
	assert.Equal(t, http.StatusConflict, gotStatus, "default status is 409 (allowed, but switched off)")
	assert.Equal(t, "MailDisabled", gotKey)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestGate_UsesPluginStatusAndKey(t *testing.T) {
	p := &Plugin{
		ID:             "terminal",
		Enabled:        func() bool { return false },
		DisabledStatus: http.StatusServiceUnavailable,
		DisabledMsgKey: "TerminalDisabled",
	}
	next, ran := gateProbe()

	var gotStatus int
	var gotKey string
	h := Gate(p, next, func(w http.ResponseWriter, _ *http.Request, status int, msgKey string) {
		gotStatus, gotKey = status, msgKey
		w.WriteHeader(status)
	})
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/terminal/ws", http.NoBody))

	assert.False(t, *ran)
	assert.Equal(t, http.StatusServiceUnavailable, gotStatus, "terminal reports 503 (runtime not up), not 409")
	assert.Equal(t, "TerminalDisabled", gotKey)
}

func TestGate_PassesWhenEnabled(t *testing.T) {
	p := &Plugin{ID: "mail", Enabled: func() bool { return true }}
	next, ran := gateProbe()

	w := httptest.NewRecorder()
	Gate(p, next, nil)(w, httptest.NewRequest(http.MethodGet, "/api/mail/send", http.NoBody))

	assert.True(t, *ran)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGate_NilEnabledIsAlwaysOn(t *testing.T) {
	p := &Plugin{ID: "stats"}
	next, ran := gateProbe()

	Gate(p, next, nil)(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/usage/stats", http.NoBody))
	assert.True(t, *ran, "a plugin without an Enabled func can never be gated")
}

func TestGate_HonoursMethodScopedExemption(t *testing.T) {
	p := &Plugin{
		ID:          "forge",
		Enabled:     func() bool { return false },
		APIPrefixes: []string{"/api/forge/"},
		// The frontend reads the binding to decide "not bound", so GET must keep
		// answering while the integration is off; writing a binding must not.
		GateExempt: []string{"GET /api/forge/binding"},
	}
	next, ran := gateProbe()
	h := Gate(p, next, nil)

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/forge/binding", http.NoBody))
	assert.True(t, *ran, "GET /api/forge/binding must reach the handler while forge is off")
	assert.Equal(t, http.StatusOK, w.Code)

	*ran = false
	w = httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodPost, "/api/forge/binding", http.NoBody))
	assert.False(t, *ran, "POST /api/forge/binding must still be refused")
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestGate_FallbackBodyWithoutWriter(t *testing.T) {
	p := &Plugin{ID: "share", Enabled: func() bool { return false }}
	next, ran := gateProbe()

	w := httptest.NewRecorder()
	Gate(p, next, nil)(w, httptest.NewRequest(http.MethodPost, "/api/share", http.NoBody))

	assert.False(t, *ran)
	require.Equal(t, http.StatusConflict, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ShareDisabled", body["msgKey"])
	assert.Equal(t, float64(http.StatusConflict), body["code"])
}
