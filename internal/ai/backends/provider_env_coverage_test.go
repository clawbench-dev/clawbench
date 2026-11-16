package backends_test

import (
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	// Register every backend so the coverage assertion sees the real set.
	_ "clawbench/internal/ai/backends/all"
)

// registeredAtInit snapshots the backend registry during package init.
//
// It cannot be read inside the test: registry_test.go calls ResetForTest, and
// Go runs tests in one process in file order, so by the time this test runs the
// registry may legitimately be empty. Capturing at init is what makes the
// assertion independent of test order.
var registeredAtInit = func() map[string]bool {
	m := make(map[string]bool)
	for _, p := range backends.All() {
		m[p.ID] = true
	}
	return m
}()

// TestProviderEnvTableCoversEveryBackend pins the two directions that matter:
// a newly registered backend must be classified (even if the classification is
// "cannot take a provider"), and the table must not keep entries for backends
// that no longer exist.
func TestProviderEnvTableCoversEveryBackend(t *testing.T) {
	if len(registeredAtInit) == 0 {
		t.Fatal("no backends were registered at init; the coverage check would be vacuous")
	}
	for id := range registeredAtInit {
		if !model.HasProviderEnvEntry(id) {
			t.Errorf("backend %q has no provider-env entry; set ProviderEnv on its "+
				"BackendPlugin (an empty spec is a valid 'cannot take a provider' decision)", id)
		}
	}

	for _, id := range model.ProviderEnvBackends() {
		if registeredAtInit[id] || id == "custom-acp" {
			continue
		}
		t.Errorf("provider-env table has an entry for unknown backend %q", id)
	}
}

// TestProviderEnvTableIsNotEmpty guards against a silent regression where
// every entry is emptied and the feature quietly does nothing.
func TestProviderEnvTableIsNotEmpty(t *testing.T) {
	supported := 0
	for _, id := range model.ProviderEnvBackends() {
		if model.ProviderEnvSupported(id, "") {
			supported++
		}
	}
	if supported < 10 {
		t.Fatalf("only %d backends accept a provider; the table looks emptied", supported)
	}
}
