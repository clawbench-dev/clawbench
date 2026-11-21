package config

import "clawbench/internal/model"

// Terminal 的配置声明。
var Terminal = &Feature{
	ID: "terminal",
	Fields: map[string]bool{
		"terminal.enabled":      true,
		"terminal.idle_timeout": true, // 0 表示不断开
		"terminal.max_sessions": true,
		"terminal.buffer_lines": true,
	},
	Defaults: defaultTerminal,
	Apply:    applyTerminal,
}

func applyTerminal(cfg *model.Config, patch map[string]any) {
	terminal, ok := patch["terminal"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := terminal["enabled"].(bool); ok {
		cfg.Terminal.Enabled = v
	}
	if v, ok := terminal["idle_timeout"].(string); ok {
		cfg.Terminal.IdleTimeout = v
	}
	if v, ok := terminal["max_sessions"].(float64); ok {
		cfg.Terminal.MaxSessions = int(v)
	}
	if v, ok := terminal["buffer_lines"].(float64); ok {
		cfg.Terminal.BufferLines = int(v)
	}
}

// Terminal. Bool zero-value trap: the master switch defaults to true when
// absent. The tuning numbers are clamped to usable values.
func defaultTerminal(cfg *model.Config, present func(string) bool) {
	if !present("terminal.enabled") {
		cfg.Terminal.Enabled = true
	}
	if cfg.Terminal.IdleTimeout == "" {
		cfg.Terminal.IdleTimeout = "0" // 0 = never timeout; PTY lives until process exits or user closes
	}
	if cfg.Terminal.BufferLines <= 0 {
		cfg.Terminal.BufferLines = 2000
	}
	if cfg.Terminal.MaxLineBytes <= 0 {
		cfg.Terminal.MaxLineBytes = 65536 // 64KB per line
	}
	if cfg.Terminal.MaxBufferMB <= 0 {
		cfg.Terminal.MaxBufferMB = 4
	}
	if cfg.Terminal.MaxSessions <= 0 {
		cfg.Terminal.MaxSessions = 10
	}
}
