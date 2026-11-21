// Package kb 是知识库功能的插件声明。
package kb

import (
	"clawbench/internal/kb"
	"clawbench/internal/model"
	"clawbench/internal/plugin/config"
	"clawbench/internal/plugins"
)

// Register 把知识库功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "kb",
		Section:   "kb",
		Enabled:   func() bool { return model.ConfigInstance.KB.Enabled },
		DefaultOn: true,
		// 运行时可用性：知识库的存储/索引器没起来（初始化失败）时，
		// 功能是开着的但跑不了，前端要灰显而不是隐藏。
		Available:    kb.Available,
		APIPrefixes:  []string{"/api/kb/"},
		MountRoutes:  MountKBRoutes,
		ConfigFields: config.Fields("kb"),
		// 热更新：索引器与嵌入客户端按新配置重建（与 rag 共享嵌入端点）。
		Reconfigure: func(cfg model.Config) { kb.Reconfigure(cfg.KB, cfg.RAG) },
	})
}
