package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// MigrateAISummaryProvider turns a legacy ai_summary endpoint into a providers
// row and rewrites config.yaml to reference it.
//
// The summary model is now configured only by referencing a provider, so an
// upgrade must not silently drop a working endpoint. The legacy fields stay
// readable (see model.AISummaryConfig.SummaryEndpoint), which also means a
// failed write leaves the user with a working configuration rather than a
// broken one — the migration retries on the next start.
func MigrateAISummaryProvider() error {
	cfg := &model.ConfigInstance.AISummary
	if cfg.ProviderID != nil || cfg.API.BaseURL == "" {
		return nil
	}

	protocol := model.ProviderProtocolOpenAI
	if cfg.Format == model.ProviderProtocolAnthropic || (cfg.Format == "" && looksAnthropic(cfg.API.BaseURL)) {
		protocol = model.ProviderProtocolAnthropic
	}

	provider, err := createMigratedProvider(cfg.API.BaseURL, cfg.API.Key, protocol)
	if err != nil {
		return err
	}

	cfg.ProviderID = &provider.ID
	cfg.Format = ""
	cfg.API = model.APIConfig{}

	if err := rewriteAISummaryConfig(cfg.ProviderID, cfg.Model); err != nil {
		return err
	}
	slog.Info("migrated ai_summary endpoint to a model provider",
		"provider_id", provider.ID, "provider", provider.Name)
	return nil
}

// createMigratedProvider creates the providers row, retrying with a suffix when
// the display name is already taken (a user may have created one by hand).
func createMigratedProvider(baseURL, key, protocol string) (*model.Provider, error) {
	// English on purpose: provider names are user-editable display strings, and
	// this one is created without a request locale (the CJK guard forbids
	// hardcoded user-visible Chinese).
	const base = "AI Summary"
	for i := range 50 {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s %d", base, i+1)
		}
		p, err := service.CreateProvider(model.Provider{
			Name:     name,
			Protocol: protocol,
			BaseURL:  baseURL,
			APIKey:   key,
			AuthMode: model.ProviderAuthAuto,
		})
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, service.ErrProviderNameTaken) {
			return nil, fmt.Errorf("migrate ai_summary provider: %w", err)
		}
	}
	return nil, fmt.Errorf("migrate ai_summary provider: no free name")
}

func looksAnthropic(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "anthropic")
}

// rewriteAISummaryConfig replaces the whole ai_summary block so the legacy key
// does not survive in the file. Other sections are preserved byte-for-byte in
// meaning (the file is re-marshaled, so comments are lost — the same trade-off
// writeConfigYAML already makes).
func rewriteAISummaryConfig(providerID *int64, modelName string) error {
	configDir := filepath.Join(model.DataDir, "config")
	configPath := filepath.Join(configDir, "config.yaml")

	raw := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("migrate ai_summary: parse config.yaml: %w", err)
		}
	}
	if providerID != nil {
		block := map[string]any{"provider_id": *providerID}
		if modelName != "" {
			block["model"] = modelName
		}
		raw["ai_summary"] = block
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("migrate ai_summary: marshal config.yaml: %w", err)
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	backupConfigYAML(configPath, configPath+".bak", raw, false)

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("migrate ai_summary: write config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("migrate ai_summary: replace config: %w", err)
	}
	return nil
}
