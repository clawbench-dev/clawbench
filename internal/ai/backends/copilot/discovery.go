package copilot

import (
	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.StaticSource("copilot", "copilot", model.CopilotCatalog))
}
