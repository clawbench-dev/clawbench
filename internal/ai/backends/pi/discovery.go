package pi

import (
	"clawbench/internal/model"
)

func init() {
	// Pi prints its model table to stderr, not stdout.
	model.RegisterModelSource(model.NewCLISource("pi", model.CLIOptions{
		Command:       "pi",
		Args:          []string{"--list-models"},
		CombineStderr: true,
		Parse:         model.ParseTabular,
	}))
}
