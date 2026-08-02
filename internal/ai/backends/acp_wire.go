package backends

import (
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

func init() {
	// Wire up the ACP lookup function variables in internal/ai so that
	// ACP event mapping code can query backend-specific data without
	// importing the backends package (avoiding import cycles).
	ai.LookupACPRemapsFn = LookupACPRemaps
	ai.LookupACPToolCallIDPrefixesFn = LookupACPToolCallIDPrefixes

	// Wire up the BackendSpec loader so model/discovery.go can build
	// BackendRegistry dynamically from backend plugins.
	model.LoadBackendSpecs = AllSpecsSorted

	// Wire up the CLI capability reporter so model can tell whether a backend
	// has a CLI implementation (grok and other ACP-only backends return false).
	model.BackendSupportsCLIFn = ai.BackendSupportsCLI
}
