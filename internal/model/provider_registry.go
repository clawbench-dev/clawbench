package model

import (
	"strings"
)

// ModelInfo represents a model returned from the /v1/models endpoint.
type ModelInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created int64  `json:"created"` // Unix timestamp from /v1/models
}

// ProviderSpec describes a built-in LLM provider.
// ChatEndpoint and ModelsEndpoint are separate because different providers
// have inconsistent path structures (e.g., DeepSeek /v1/chat/completions
// but /models derivation would miss /v1/).
type ProviderSpec struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	EnvVar         string `json:"envVar"`
	ChatEndpoint   string `json:"-"` // full URL for summarize API calls
	ModelsEndpoint string `json:"-"` // full URL for GET /v1/models (may be "" for Anthropic-format)
	APIFormat      string `json:"-"` // "openai" or "anthropic"
	SupportsCLI    bool   `json:"-"` // true = CLI can use this provider directly
}

// ProviderRegistry maps provider IDs to their specifications.
var ProviderRegistry = map[string]ProviderSpec{
	// --- OpenAI-compatible providers ---
	"openai": {
		ID: "openai", Name: "OpenAI", EnvVar: "OPENAI_API_KEY",
		ChatEndpoint:   "https://api.openai.com/v1/chat/completions",
		ModelsEndpoint: "https://api.openai.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"anthropic": {
		ID: "anthropic", Name: "Anthropic", EnvVar: "ANTHROPIC_API_KEY",
		ChatEndpoint:   "https://api.anthropic.com/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},
	"deepseek": {
		ID: "deepseek", Name: "DeepSeek", EnvVar: "DEEPSEEK_API_KEY",
		ChatEndpoint:   "https://api.deepseek.com/v1/chat/completions",
		ModelsEndpoint: "https://api.deepseek.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"groq": {
		ID: "groq", Name: "Groq", EnvVar: "GROQ_API_KEY",
		ChatEndpoint:   "https://api.groq.com/openai/v1/chat/completions",
		ModelsEndpoint: "https://api.groq.com/openai/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"openrouter": {
		ID: "openrouter", Name: "OpenRouter", EnvVar: "OPENROUTER_API_KEY",
		ChatEndpoint:   "https://openrouter.ai/api/v1/chat/completions",
		ModelsEndpoint: "https://openrouter.ai/api/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"cerebras": {
		ID: "cerebras", Name: "Cerebras", EnvVar: "CEREBRAS_API_KEY",
		ChatEndpoint:   "https://api.cerebras.ai/v1/chat/completions",
		ModelsEndpoint: "https://api.cerebras.ai/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"xai": {
		ID: "xai", Name: "xAI Grok", EnvVar: "XAI_API_KEY",
		ChatEndpoint:   "https://api.x.ai/v1/chat/completions",
		ModelsEndpoint: "https://api.x.ai/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"mistral": {
		ID: "mistral", Name: "Mistral", EnvVar: "MISTRAL_API_KEY",
		ChatEndpoint:   "https://api.mistral.ai/v1/chat/completions",
		ModelsEndpoint: "https://api.mistral.ai/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"fireworks": {
		ID: "fireworks", Name: "Fireworks", EnvVar: "FIREWORKS_API_KEY",
		ChatEndpoint:   "https://api.fireworks.ai/inference/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},
	"minimax": {
		ID: "minimax", Name: "MiniMax", EnvVar: "MINIMAX_API_KEY",
		ChatEndpoint:   "https://api.minimax.io/anthropic/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},
	"minimax-cn": {
		ID: "minimax-cn", Name: "MiniMax (China)", EnvVar: "MINIMAX_API_KEY",
		ChatEndpoint:   "https://api.minimaxi.com/anthropic/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},
	"kimi-coding": {
		ID: "kimi-coding", Name: "Kimi For Coding", EnvVar: "KIMI_API_KEY",
		ChatEndpoint:   "https://api.kimi.com/coding/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},
	"moonshotai": {
		ID: "moonshotai", Name: "Moonshot AI", EnvVar: "MOONSHOT_API_KEY",
		ChatEndpoint:   "https://api.moonshot.ai/v1/chat/completions",
		ModelsEndpoint: "https://api.moonshot.ai/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"moonshotai-cn": {
		ID: "moonshotai-cn", Name: "Moonshot AI (China)", EnvVar: "MOONSHOT_API_KEY",
		ChatEndpoint:   "https://api.moonshot.cn/v1/chat/completions",
		ModelsEndpoint: "https://api.moonshot.cn/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"xiaomi": {
		ID: "xiaomi", Name: "Xiaomi MiMo", EnvVar: "XIAOMI_API_KEY",
		ChatEndpoint:   "https://api.xiaomimimo.com/v1/chat/completions",
		ModelsEndpoint: "https://api.xiaomimimo.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"xiaomi-token-plan-cn": {
		ID: "xiaomi-token-plan-cn", Name: "Xiaomi MiMo Token Plan (China)", EnvVar: "XIAOMI_TOKEN_PLAN_CN_API_KEY",
		ChatEndpoint:   "https://token-plan-cn.xiaomimimo.com/v1/chat/completions",
		ModelsEndpoint: "https://token-plan-cn.xiaomimimo.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"xiaomi-token-plan-ams": {
		ID: "xiaomi-token-plan-ams", Name: "Xiaomi MiMo Token Plan (Amsterdam)", EnvVar: "XIAOMI_TOKEN_PLAN_AMS_API_KEY",
		ChatEndpoint:   "https://token-plan-ams.xiaomimimo.com/v1/chat/completions",
		ModelsEndpoint: "https://token-plan-ams.xiaomimimo.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"xiaomi-token-plan-sgp": {
		ID: "xiaomi-token-plan-sgp", Name: "Xiaomi MiMo Token Plan (Singapore)", EnvVar: "XIAOMI_TOKEN_PLAN_SGP_API_KEY",
		ChatEndpoint:   "https://token-plan-sgp.xiaomimimo.com/v1/chat/completions",
		ModelsEndpoint: "https://token-plan-sgp.xiaomimimo.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"zai": {
		ID: "zai", Name: "ZAI", EnvVar: "ZAI_API_KEY",
		ChatEndpoint:   "https://api.z.ai/api/coding/paas/v4/chat/completions",
		ModelsEndpoint: "https://api.z.ai/api/coding/paas/v4/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"huggingface": {
		ID: "huggingface", Name: "Hugging Face", EnvVar: "HF_API_KEY",
		ChatEndpoint:   "https://router.huggingface.co/v1/chat/completions",
		ModelsEndpoint: "https://router.huggingface.co/v1/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"opencode": {
		ID: "opencode", Name: "OpenCode Zen", EnvVar: "OPENCODE_API_KEY",
		ChatEndpoint:   "https://opencode.ai/zen/chat/completions",
		ModelsEndpoint: "https://opencode.ai/zen/models",
		APIFormat:      "openai", SupportsCLI: true,
	},
	"vercel-ai-gateway": {
		ID: "vercel-ai-gateway", Name: "Vercel AI Gateway", EnvVar: "AI_GATEWAY_API_KEY",
		ChatEndpoint:   "https://ai-gateway.vercel.sh/v1/messages",
		ModelsEndpoint: "",
		APIFormat:      "anthropic", SupportsCLI: true,
	},

	// --- Enterprise providers (manual config only) ---
	"amazon-bedrock": {
		ID: "amazon-bedrock", Name: "Amazon Bedrock", EnvVar: "",
		ChatEndpoint: "", ModelsEndpoint: "", APIFormat: "",
		SupportsCLI: true,
	},
	"azure-openai-responses": {
		ID: "azure-openai-responses", Name: "Azure OpenAI Responses", EnvVar: "AZURE_OPENAI_API_KEY",
		ChatEndpoint: "", ModelsEndpoint: "", APIFormat: "",
		SupportsCLI: true,
	},
	"cloudflare-ai-gateway": {
		ID: "cloudflare-ai-gateway", Name: "Cloudflare AI Gateway", EnvVar: "CLOUDFLARE_API_KEY",
		ChatEndpoint: "", ModelsEndpoint: "", APIFormat: "",
		SupportsCLI: true,
	},
	"cloudflare-workers-ai": {
		ID: "cloudflare-workers-ai", Name: "Cloudflare Workers AI", EnvVar: "CLOUDFLARE_API_KEY",
		ChatEndpoint: "", ModelsEndpoint: "", APIFormat: "",
		SupportsCLI: true,
	},
	"google-vertex": {
		ID: "google-vertex", Name: "Google Vertex AI", EnvVar: "",
		ChatEndpoint: "", ModelsEndpoint: "", APIFormat: "",
		SupportsCLI: true,
	},
}

// FindProviderSpec looks up a provider by ID. Returns nil if not found.
func FindProviderSpec(providerID string) *ProviderSpec {
	spec, ok := ProviderRegistry[providerID]
	if !ok {
		return nil
	}
	return &spec
}

// GetSummarizeModelHint returns a recommended model ID for summarization
// by scanning model IDs for cheap-model keywords.
// Falls back to the first model if no better match found.
func GetSummarizeModelHint(v1Models []ModelInfo) string {
	if len(v1Models) == 0 {
		return ""
	}

	// Keywords in priority order — match as hyphen-delimited segment
	// to avoid false positives (e.g., "mini" matching partial words)
	keywords := []string{"mini", "flash", "haiku", "lite", "small"}
	for _, kw := range keywords {
		for _, m := range v1Models {
			lowerID := strings.ToLower(m.ID)
			// Match keyword as a segment: preceded by hyphen, dot, slash, or start of string
			for _, sep := range []string{"-", ".", "/", "_"} {
				if strings.Contains(lowerID, sep+kw) {
					return m.ID
				}
			}
			// Also match if the ID starts with the keyword (e.g., "mini-max")
			if strings.HasPrefix(lowerID, kw) {
				return m.ID
			}
		}
	}
	// No keyword match, return first model
	return v1Models[0].ID
}
