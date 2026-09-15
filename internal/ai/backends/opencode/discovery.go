package opencode

import (
	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.NewCLISource("opencode", model.CLIOptions{
		Command: "opencode",
		Args:    []string{"models"},
		Parse:   model.ParseProviderModel,
	}))
}
