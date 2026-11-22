package kimi

import (
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// ConfigTOMLContent renders kimi's config.toml.
//
// The provider type is chosen from the provider's protocol: kimi reads
// "openai_legacy" for OpenAI-compatible endpoints and "anthropic" for
// Anthropic-compatible ones. The key is written in clear text because kimi's
// config has no ${ENV} reference syntax (verified: a literal "${VAR}" is sent
// as the key and the endpoint answers 401).
//
// Registered as model.ProviderEnvSpec.ConfigDirRender.
func ConfigTOMLContent(p *model.Provider) ([]byte, error) {
	providerType := "openai_legacy"
	if p.Protocol == model.ProviderProtocolAnthropic {
		providerType = "anthropic"
	}
	id := model.ProviderConfigProviderID("kimi")
	models := model.ProviderModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by ClawBench for one agent. Edits are overwritten.\n")
	fmt.Fprintf(&b, "default_model = %s\n\n", tomlString(models[0]))

	for _, m := range models {
		fmt.Fprintf(&b, "[models.%s]\n", tomlKey(m))
		fmt.Fprintf(&b, "provider = %s\n", tomlString(id))
		fmt.Fprintf(&b, "model = %s\n", tomlString(m))
		fmt.Fprintf(&b, "max_context_size = 128000\n\n")
	}

	fmt.Fprintf(&b, "[providers.%s]\n", tomlKey(id))
	fmt.Fprintf(&b, "type = %s\n", tomlString(providerType))
	fmt.Fprintf(&b, "base_url = %s\n", tomlString(p.BaseURL))
	fmt.Fprintf(&b, "api_key = %s\n", tomlString(p.APIKey))
	return []byte(b.String()), nil
}

// tomlKey renders a TOML bare key when possible and a quoted key otherwise.
// Model ids routinely contain dots ("deepseek-v4.1-flash"), which TOML reads as
// a nested table — an unquoted "models.deepseek-v4.1-flash" would silently
// create three levels and the model would not be found.
func tomlKey(s string) string {
	if s == "" {
		return tomlString(s)
	}
	for _, r := range s {
		bare := r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !bare {
			return tomlString(s)
		}
	}
	return s
}

// tomlString quotes a TOML basic string, escaping the characters TOML reserves.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
