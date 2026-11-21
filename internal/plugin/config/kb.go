package config

import (
	"errors"
	"math"

	"clawbench/internal/model"
)

// KB 的配置声明。
var KB = &Feature{
	ID: "kb",
	Fields: map[string]bool{
		"kb.enabled":               true,
		"kb.auto_inject":           true,
		"kb.inject_top_n":          true,
		"kb.inject_max_chars":      true, // 防止一篇长文吃掉整个上下文窗口
		"kb.inject_min_similarity": true,
		"kb.max_file_mb":           true,
		"kb.max_text_mb":           true,
		"kb.poll_interval":         true,
		"kb.embed_batch_size":      true,
	},
	Validate: validateKB,
	Defaults: defaultKB,
	Apply:    applyKB,
}

func validateKB(patch map[string]any) error {
	kbVal, ok := patch["kb"].(map[string]any)
	if !ok {
		return nil
	}
	// 表驱动：每加一个边界只加一行，避免把一个函数堆成 19 条分支。
	// max 用 math.Inf(1) 表示"只有下限"。
	bounds := []struct {
		key      string
		min, max float64
		message  string
	}{
		{"inject_top_n", 1, 20, "kb.inject_top_n must be between 1 and 20"},
		{"inject_max_chars", 200, math.Inf(1), "kb.inject_max_chars must be at least 200"},
		{"inject_min_similarity", 0, 1, "kb.inject_min_similarity must be between 0 and 1"},
		{"max_file_mb", 1, 1024, "kb.max_file_mb must be between 1 and 1024"},
		{"max_text_mb", 1, 512, "kb.max_text_mb must be between 1 and 512"},
		{"embed_batch_size", 1, 128, "kb.embed_batch_size must be between 1 and 128"},
	}
	for _, b := range bounds {
		if v, ok := kbVal[b.key].(float64); ok && (v < b.min || v > b.max) {
			return errors.New(b.message)
		}
	}
	return nil
}

func applyKB(cfg *model.Config, patch map[string]any) {
	kbVal, ok := patch["kb"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := kbVal["enabled"].(bool); ok {
		cfg.KB.Enabled = v
	}
	if v, ok := kbVal["auto_inject"].(bool); ok {
		cfg.KB.AutoInject = v
	}
	if v, ok := kbVal["inject_top_n"].(float64); ok {
		cfg.KB.InjectTopN = int(v)
	}
	if v, ok := kbVal["inject_max_chars"].(float64); ok {
		cfg.KB.InjectMaxChars = int(v)
	}
	if v, ok := kbVal["inject_min_similarity"].(float64); ok {
		cfg.KB.InjectMinSimilarity = v
	}
	if v, ok := kbVal["max_file_mb"].(float64); ok {
		cfg.KB.MaxFileMB = int(v)
	}
	if v, ok := kbVal["max_text_mb"].(float64); ok {
		cfg.KB.MaxTextMB = int(v)
	}
	if v, ok := kbVal["poll_interval"].(string); ok {
		cfg.KB.PollInterval = v
	}
	if v, ok := kbVal["embed_batch_size"].(float64); ok {
		cfg.KB.EmbedBatchSize = int(v)
	}
}

// KB (knowledge base). Bool zero-value trap: the master switch defaults to true
// when absent. Auto-inject is opt-in, so its false zero value is the intended
// default and there is deliberately no presence branch for it.
func defaultKB(cfg *model.Config, present func(string) bool) {
	if !present("kb.enabled") {
		cfg.KB.Enabled = true
	}
	if cfg.KB.InjectTopN <= 0 {
		cfg.KB.InjectTopN = 4
	}
	if cfg.KB.InjectMaxChars <= 0 {
		cfg.KB.InjectMaxChars = 2000
	}
	if cfg.KB.InjectMinSimilarity <= 0 {
		cfg.KB.InjectMinSimilarity = 0.35
	}
	if cfg.KB.MaxFileMB <= 0 {
		cfg.KB.MaxFileMB = 50
	}
	if cfg.KB.MaxTextMB <= 0 {
		cfg.KB.MaxTextMB = 20
	}
	if cfg.KB.PollInterval == "" {
		cfg.KB.PollInterval = "2s"
	}
	if cfg.KB.EmbedBatchSize <= 0 {
		cfg.KB.EmbedBatchSize = 16
	}
}
