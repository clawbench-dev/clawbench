package qodercn

import (
	"clawbench/internal/ai/backends/qoder"
	"clawbench/internal/model"
)

// QoderCNConfigRoot is the CN build's user-level config root.
//
// The two distributions are separate installs with separate credentials and
// separate model catalogs, so each reads only its own root. Pointing the CN
// binary at the international root is actively destructive: it loads the
// foreign token, rejects it, and clears it (see the file header of
// internal/ai/qoder_cn_acp_probe_integration_test.go).
const QoderCNConfigRoot = "~/.qoder-cn"

func init() {
	model.RegisterModelSource(model.PluginSource("qodercn", func() ([]model.AgentModel, string) {
		return qoder.DiscoverModelsIn("qodercn", QoderCNConfigRoot)
	}))
}
