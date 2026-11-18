package endpointurl

import "testing"

// TestResolve pins which URL a configured base resolves to. Each group below is
// a shape a real provider published and that a per-consumer copy of these rules
// once got wrong.
func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		defaultPath string
		expected    string
	}{
		// Bare hosts and partial bases.
		{"bare host", "https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"host with /v1", "https://api.openai.com/v1", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"trailing slash", "https://api.openai.com/v1/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"trailing slash on host", "https://api.openai.com/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"already complete", "https://api.openai.com/v1/chat/completions", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"prefixed base", "https://openrouter.ai/api/v1", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"no scheme", "api.example.com", "/v1/messages", "api.example.com/v1/messages"},
		{"empty base", "", "/v1/chat/completions", ""},
		{"whitespace base", "   ", "/v1/messages", ""},
		{"path without leading slash", "https://api.example.com", "chat", "https://api.example.com/chat"},

		// A bare version segment is a base for the same endpoint shape under a
		// different API version (Volcengine Ark publishes "/api/v3").
		{"api v3 namespace", "https://ark.cn-beijing.volces.com/api/v3", "/v1/chat/completions", "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
		{"api v3 namespace trailing slash", "https://ark.cn-beijing.volces.com/api/v3/", "/v1/chat/completions", "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
		{"bare v3", "https://host/v3", "/v1/chat/completions", "https://host/v3/chat/completions"},
		{"prefixed v2", "https://host/openai/v2", "/v1/chat/completions", "https://host/openai/v2/chat/completions"},
		{"v1beta", "https://api.example.com/v1beta", "/v1/messages", "https://api.example.com/v1beta/messages"},
		{"v3 complete endpoint preserved", "https://host/api/v3/chat/completions", "/v1/chat/completions", "https://host/api/v3/chat/completions"},
		{"api v3 anthropic", "https://gw.example.com/api/v3", "/v1/messages", "https://gw.example.com/api/v3/messages"},
		{"v3 anthropic complete preserved", "https://gw.example.com/api/v3/messages", "/v1/messages", "https://gw.example.com/api/v3/messages"},
		// A vendor namespace that merely starts with "v" is not a version.
		{"vendor name with v", "https://host/vecli", "/v1/messages", "https://host/vecli/v1/messages"},

		// Anthropic equivalents.
		{"Anthropic bare host", "https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic with /v1", "https://api.anthropic.com/v1", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic already complete", "https://api.anthropic.com/v1/messages", "/v1/messages", "https://api.anthropic.com/v1/messages"},

		// A gateway mounted under a versionless namespace is a BASE, not an
		// endpoint: MiniMax publishes "/anthropic" and serves
		// "/anthropic/v1/messages". Using it verbatim 404s every call — this is
		// what broke the builtin agent while "test connection" passed.
		{"anthropic gateway namespace", "https://api.minimax.cn/anthropic", "/v1/messages", "https://api.minimax.cn/anthropic/v1/messages"},
		{"openai gateway namespace", "https://gateway.example.com/openai", "/v1/chat/completions", "https://gateway.example.com/openai/v1/chat/completions"},
		{"namespace with version", "https://host/anthropic/v1", "/v1/messages", "https://host/anthropic/v1/messages"},
		{"versionless word", "https://x.example.com/chat", "/v1/messages", "https://x.example.com/chat/v1/messages"},

		// A provider-specific endpoint typed in full must be left alone —
		// appending would produce a guaranteed 404.
		{"custom endpoint preserved", "https://api.minimax.chat/v1/text/chatcompletion_v2", "/v1/chat/completions", "https://api.minimax.chat/v1/text/chatcompletion_v2"},
		{"custom anthropic endpoint preserved", "https://host/anthropic/v1/messages", "/v1/messages", "https://host/anthropic/v1/messages"},
		{"versioned path with action preserved", "https://x.example.com/v1/chat", "/v1/messages", "https://x.example.com/v1/chat"},

		// An endpoint without the version prefix (Azure-style) is complete too.
		{"azure deployment endpoint", "https://x.openai.azure.com/openai/deployments/d/chat/completions", "/v1/chat/completions", "https://x.openai.azure.com/openai/deployments/d/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.baseURL, tt.defaultPath); got != tt.expected {
				t.Fatalf("Resolve(%q, %q) = %q, want %q", tt.baseURL, tt.defaultPath, got, tt.expected)
			}
		})
	}
}

// TestResolve_IsNotFooledByVendorNamespace pins the boundary of the version
// rule: "/vecli" starts with "v" but carries no digits, so it is a mount point
// and must not have its segment replaced by the endpoint path.
func TestResolve_IsNotFooledByVendorNamespace(t *testing.T) {
	if got := Resolve("https://host/vecli", "/v1/messages"); got != "https://host/vecli/v1/messages" {
		t.Fatalf("got %q", got)
	}
}
