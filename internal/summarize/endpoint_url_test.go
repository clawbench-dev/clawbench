package summarize

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BuildEndpointURL accepts every shape a user might configure for base_url.
// The bare-host cases are the regression: the connectivity test completed them
// while the real request posted to the host verbatim, so "test connection"
// passed and the actual call 404'd.
func TestBuildEndpointURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		defaultPath string
		expected    string
	}{
		{"bare host", "https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"host with /v1", "https://api.openai.com/v1", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"already complete", "https://api.openai.com/v1/chat/completions", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"trailing slash", "https://api.openai.com/v1/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"trailing slash on host", "https://api.openai.com/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		// A gateway mounted under a prefix: "/api/v1" is a leading run of the
		// target segments, so only the remainder is appended.
		{"prefixed base", "https://openrouter.ai/api/v1", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		// Anthropic equivalents.
		{"Anthropic bare host", "https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic with /v1", "https://api.anthropic.com/v1", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic already complete", "https://api.anthropic.com/v1/messages", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		// A provider-specific endpoint typed in full must be left alone —
		// appending would produce a guaranteed 404.
		{"custom endpoint preserved", "https://api.minimax.chat/v1/text/chatcompletion_v2", "/v1/chat/completions", "https://api.minimax.chat/v1/text/chatcompletion_v2"},
		{"custom anthropic endpoint preserved", "https://host/anthropic/v1/messages", "/v1/messages", "https://host/anthropic/v1/messages"},
		// A path with no slash in defaultPath still gets a separator.
		{"path without leading slash", "https://api.example.com", "chat", "https://api.example.com/chat"},
		{"empty base", "", "/v1/chat/completions", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BuildEndpointURL(tt.baseURL, tt.defaultPath))
		})
	}
}

// The constructors must normalize the URL, not just trim it: a bare base_url
// has to resolve to the full endpoint. This is the exact configuration that
// produced "test connection: OK / real call: 404".
func TestNewOpenAI_NormalizesBareBaseURL(t *testing.T) {
	s := NewOpenAI("https://rd-oneapi.uniview.com", "key", "")
	assert.Equal(t, "https://rd-oneapi.uniview.com/v1/chat/completions", s.BaseURL)
}

func TestNewAnthropic_NormalizesBareBaseURL(t *testing.T) {
	s := NewAnthropic("https://api.anthropic.com", "key", "")
	assert.Equal(t, "https://api.anthropic.com/v1/messages", s.BaseURL)
}

// A full custom endpoint survives construction unchanged.
func TestNewOpenAI_PreservesCustomEndpoint(t *testing.T) {
	s := NewOpenAI("https://api.minimax.chat/v1/text/chatcompletion_v2", "key", "")
	assert.Equal(t, "https://api.minimax.chat/v1/text/chatcompletion_v2", s.BaseURL)
}

// End-to-end through the real path a title generation takes: a bare base_url
// must reach the provider's completion endpoint, not the site root. Before the
// fix the request landed on "/" and the model returned 404.
func TestNewAISummarizer_BareBaseURLReachesCompletionEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"标题"}}]}`))
	}))
	defer srv.Close()

	// httptest gives "http://127.0.0.1:PORT" — a bare host, the failing shape.
	require.True(t, strings.HasPrefix(srv.URL, "http://127.0.0.1:"))
	require.NotContains(t, strings.TrimPrefix(srv.URL, "http://"), "/")

	s := NewOpenAI(srv.URL, "key", "gpt-4o-mini")
	_, err := GenerateSessionTitle(context.Background(), s, []string{"登录总是超时"}, "zh")
	require.NoError(t, err)
	assert.Equal(t, "/v1/chat/completions", gotPath,
		"a bare base_url must be completed to the completion endpoint")
}

// Same guarantee for the Anthropic format, where the path differs.
func TestNewAnthropic_BareBaseURLReachesMessagesEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Title"}]}`))
	}))
	defer srv.Close()

	s := NewAnthropic(srv.URL, "key", "claude-3-haiku")
	_, err := GenerateSessionTitle(context.Background(), s, []string{"login times out"}, "en")
	require.NoError(t, err)
	assert.Equal(t, "/v1/messages", gotPath)
}

// The connectivity test and the summarizer constructors must resolve the same
// configuration to the same URL. They are separate call sites of one shared
// builder, so this pins that they cannot drift apart again.
func TestBuildEndpointURL_TestAndCallAgree(t *testing.T) {
	for _, base := range []string{
		"https://api.openai.com",
		"https://api.openai.com/v1",
		"https://api.openai.com/v1/chat/completions",
	} {
		// What the connectivity test probes.
		probed := BuildEndpointURL(base, OpenAIChatCompletionsPath)
		// What the real summarizer posts to.
		posted := NewOpenAI(base, "k", "").BaseURL
		assert.Equal(t, probed, posted, "test and real call must agree for %q", base)
	}
}
