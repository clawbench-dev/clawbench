package backends

import (
	"clawbench/internal/ai"
)

func init() {
	// Wire up the ACP lookup function variables in internal/ai so that
	// ACP event mapping code can query backend-specific data without
	// importing the backends package (avoiding import cycles).
	ai.LookupACPRemapsFn = LookupACPRemaps
	ai.LookupACPToolCallIDPrefixesFn = LookupACPToolCallIDPrefixes
}
