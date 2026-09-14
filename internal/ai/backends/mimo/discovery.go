package mimo

import (
	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.StaticSource("mimo", "mimo", model.MimoCatalog))
}
