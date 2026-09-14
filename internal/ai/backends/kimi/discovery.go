package kimi

import (
	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.StaticSource("kimi", "kimi", model.KimiCatalog))
}
