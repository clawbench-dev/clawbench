package handler

import (
	"testing"

	"clawbench/internal/kb"
	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// setupKBHandlerTest installs a temporary knowledge base and restores it.
//
// 这个函数原先定义在 kb_test.go 里，随知识库搬进 internal/plugin/kb 一起
// 移走了。但 handler 里还有不依赖 kb 接口、只需要 kb 状态的用例
// （/cb-kbsearch 的命令预检），所以在这里保留一份最小实现。
func setupKBHandlerTest(t *testing.T) *testEnv {
	t.Helper()
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	oldDir := model.DataDir
	oldCfg := model.ConfigInstance.KB
	model.DataDir = t.TempDir()
	model.ConfigInstance.KB = model.KBConfig{
		Enabled:        true,
		AutoInject:     false,
		InjectTopN:     4,
		InjectMaxChars: 2000,
		MaxFileMB:      50,
		MaxTextMB:      20,
		PollInterval:   "20ms",
		EmbedBatchSize: 4,
	}
	t.Cleanup(func() {
		kb.Shutdown()
		model.DataDir = oldDir
		model.ConfigInstance.KB = oldCfg
	})

	require.NoError(t, kb.Init(model.ConfigInstance.KB, model.RAGConfig{
		VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64,
	}))
	kb.StartIndexer()
	return env
}
