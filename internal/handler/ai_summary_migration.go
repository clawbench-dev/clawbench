package handler

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/service"
)

// MigrateAISummaryProvider turns a legacy ai_summary endpoint into a providers
// row and rewrites config.yaml to reference it.
//
// The summary model is now configured only by referencing a provider, so an
// upgrade must not silently drop a working endpoint. The legacy fields stay
// readable (see model.AISummaryConfig.SummaryEndpoint), which also means a
// failed write leaves the user with a working configuration rather than a
// broken one — the provider created for the migration is deleted again and the
// migration retries on the next start.
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

	legacyFormat, legacyAPI := cfg.Format, cfg.API
	cfg.ProviderID = &provider.ID
	cfg.Format = ""
	cfg.API = model.APIConfig{}

	if err := rewriteAISummaryConfig(cfg.ProviderID, cfg.Model); err != nil {
		// The row is already committed, and the legacy fields are what makes
		// the next start retry. Leaving the row behind would make every retry
		// create another "AI Summary N" until the name suffix runs out.
		cfg.ProviderID = nil
		cfg.Format = legacyFormat
		cfg.API = legacyAPI
		if delErr := service.DeleteProvider(provider.ID); delErr != nil {
			slog.Error("failed to roll back the migrated ai_summary provider",
				"provider_id", provider.ID, "error", delErr)
		}
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
//
// The file it edits is the one the server actually loaded, resolved the same
// way as startup (platform.FindConfigPath): hardcoding <DataDir> used to create
// a second, ai_summary-only config.yaml next to a real one in the CWD, and that
// stub then won the next startup lookup — resetting every other setting.
func rewriteAISummaryConfig(providerID *int64, modelName string) error {
	configPath := platform.FindConfigPath(model.DataDir)
	existing, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Never create the file here. A brand-new config.yaml would hold
			// only ai_summary and shadow the real configuration, so keep the
			// binding in memory and leave persistence to the settings page.
			slog.Warn("ai_summary migration: no config.yaml to rewrite; keeping the provider binding in memory only",
				"path", configPath)
			return nil
		}
		return fmt.Errorf("migrate ai_summary: read config.yaml: %w", err)
	}

	raw := map[string]any{}
	if len(existing) > 0 {
		if unmarshalErr := yaml.Unmarshal(existing, &raw); unmarshalErr != nil {
			return fmt.Errorf("migrate ai_summary: parse config.yaml: %w", unmarshalErr)
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
