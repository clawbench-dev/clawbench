package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
)

// SettingsJSONContent renders Claude Code's settings.json.
//
// Only the `env` block is ClawBench's: that is where the CLI reads the endpoint
// and credential from (the ACP adapter merges it into the session). Everything
// else in the file — permissions, model overrides, plugins — belongs to the
// user, so it is carried over from the user's own settings.json.
//
// Carrying it over matters: the isolated directory is a NEW directory, so a file
// holding only `env` would silently drop the user's approval rules and plugin
// list for this agent, which reads as "ClawBench turned my permissions off".
//
// The key is written in clear text: verified that a "${VAR}" reference is sent
// verbatim (the endpoint answered 401), so there is no reference syntax to use.
//
// Registered as model.ProviderEnvSpec.ConfigDirRender.
func SettingsJSONContent(p *model.Provider) ([]byte, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		// Without a base URL there is nothing to redirect: the CLI would keep
		// talking to its own default endpoint while the binding looked applied.
		return nil, fmt.Errorf("provider %q has no base URL, which claude needs", p.Name)
	}

	doc := make(map[string]any)
	if base, ok := readUserSettings(); ok {
		for k, v := range base {
			doc[k] = v
		}
	}

	env := map[string]any{"ANTHROPIC_BASE_URL": p.BaseURL}
	// Keep any other variables the user had in their env block (a proxy, a
	// feature flag): the endpoint and credential are the only keys ClawBench
	// owns, and replacing the whole block would turn those off.
	if prev, ok := doc["env"].(map[string]any); ok {
		for k, v := range prev {
			if k == "ANTHROPIC_BASE_URL" || k == "ANTHROPIC_API_KEY" || k == "ANTHROPIC_AUTH_TOKEN" {
				continue
			}
			env[k] = v
		}
	}
	// Which variable carries the credential follows the provider's auth mode: a
	// gateway that only accepts x-api-key must not be sent a Bearer token.
	if p.AuthMode == model.ProviderAuthBearer {
		env["ANTHROPIC_AUTH_TOKEN"] = p.APIKey
	} else {
		env["ANTHROPIC_API_KEY"] = p.APIKey
	}
	doc["env"] = env

	return json.MarshalIndent(doc, "", "  ")
}

// readUserSettings loads the user's own ~/.claude/settings.json so the
// per-agent copy can preserve everything ClawBench does not own.
//
// The directory comes from this backend's own UserConfigDir declaration, so
// there is no second place to keep the path in sync.
//
// A missing or unreadable file is not an error: it only means there is nothing
// to carry over, and the isolated file still gets the env block that makes the
// provider work.
func readUserSettings() (map[string]any, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, false
	}
	rel := model.ProviderUserConfigDir("claude")
	if rel == "" {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(home, rel, "settings.json"))
	if err != nil {
		return nil, false
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	return doc, true
}
