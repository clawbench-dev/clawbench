package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostFieldCarriesCredit_ComesFromTheRegisteredBehavior(t *testing.T) {
	orig := LookupACPBehaviorFn
	t.Cleanup(func() { LookupACPBehaviorFn = orig })

	behaviors := map[string]*ACPBehavior{
		"credit-backend": {CostFieldCarriesCredit: true},
		"money-backend":  {ExtractMeta: func(map[string]any) *MetaExtraction { return nil }},
	}
	LookupACPBehaviorFn = func(id string) *ACPBehavior { return behaviors[id] }

	assert.True(t, costFieldCarriesCredit("credit-backend"),
		"a backend declaring the flag must have its cost field treated as credit")
	assert.False(t, costFieldCarriesCredit("money-backend"),
		"a backend with a behavior but no flag reports real money")
	assert.False(t, costFieldCarriesCredit(""),
		"an empty backend id must not resolve to anything")
}
