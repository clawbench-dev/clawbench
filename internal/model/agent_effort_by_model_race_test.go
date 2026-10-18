package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetThinkingEffortForModel_ReplacesMapInsteadOfMutating pins the
// copy-on-write contract that keeps readers race-free.
//
// UpdateAgent publishes a SHALLOW copy of the Agent, so mutating the map in
// place would write the same map header that every reader holding the previous
// *Agent is iterating (readers deliberately do not take agentsMu). Go turns
// that into "concurrent map read and map write", which is unrecoverable.
//
// A replaced map leaves old readers with a frozen snapshot: their view is stale
// but consistent, and a reload publishes the new one.
func TestSetThinkingEffortForModel_ReplacesMapInsteadOfMutating(t *testing.T) {
	original := map[string]string{"glm-5.3": "max"}
	agent := &Agent{ThinkingEffortByModel: original}

	agent.SetThinkingEffortForModel("deepseek-v4-pro", "high")

	assert.Equal(t, map[string]string{"glm-5.3": "max"}, original,
		"the previously published map must not be mutated in place")
	assert.Equal(t, "high", agent.ThinkingEffortForModel("deepseek-v4-pro"))
	assert.Equal(t, "max", agent.ThinkingEffortForModel("glm-5.3"))
}

// TestSetThinkingEffortForModel_ConcurrentReads mirrors the PRODUCTION shape:
// UpdateAgent copies the struct under agentsMu, mutates the copy, and only then
// publishes it. The reader keeps the previously published pointer.
//
// Run with -race: mutating the shared map in place (the pre-fix behavior)
// makes this fail with "concurrent map read and map write", which Go cannot
// recover from.
func TestSetThinkingEffortForModel_ConcurrentReads(t *testing.T) {
	published := &Agent{ThinkingEffortByModel: map[string]string{"glm-5.3": "max"}}
	readerSnapshot := published

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			// Same read the service/ai layers do: resolve the pointer, then read.
			_ = readerSnapshot.ThinkingEffortForModel("glm-5.3")
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			// UpdateAgent's shape: shallow copy, mutate the copy, publish.
			updated := *published
			updated.SetThinkingEffortForModel("glm-5.3", "low")
			published = &updated
		}
	}()
	wg.Wait()

	// The reader's snapshot is frozen, not corrupted.
	assert.Equal(t, "max", readerSnapshot.ThinkingEffortForModel("glm-5.3"))
}

// TestThinkingEffortForModel_Priority covers the resolution order and the two
// degenerate inputs. It is the read side of issue #552: the per-model pick must
// win over the model-independent default, and an empty model id must skip the
// per-model lookup instead of matching the "" key.
func TestThinkingEffortForModel_Priority(t *testing.T) {
	agent := &Agent{
		ThinkingEffort:          "low",  // agent default
		PreferredThinkingEffort: "high", // user's Settings default
		ThinkingEffortByModel:   map[string]string{"glm-5.3": "max"},
	}

	assert.Equal(t, "max", agent.ThinkingEffortForModel("glm-5.3"),
		"the per-model pick must win over the global default")
	assert.Equal(t, "high", agent.ThinkingEffortForModel("deepseek-v4-pro"),
		"a model without a pick falls back to the Settings default")
	assert.Equal(t, "high", agent.ThinkingEffortForModel(""),
		"an empty model id must not look up the map")

	// A nil map (never loaded / no picks yet) must not panic.
	bare := &Agent{ThinkingEffort: "low"}
	assert.Equal(t, "low", bare.ThinkingEffortForModel("glm-5.3"))

	// An entry with an empty level is treated as "no pick", so a stale blank
	// value cannot shadow the default.
	blank := &Agent{ThinkingEffort: "low", ThinkingEffortByModel: map[string]string{"glm-5.3": ""}}
	assert.Equal(t, "low", blank.ThinkingEffortForModel("glm-5.3"))
}

// TestSetThinkingEffortForModel_ClearsOnEmptyLevel pins the delete path: an
// empty level removes the entry so the global default applies again, rather
// than storing a blank override that would read as "no pick" forever.
func TestSetThinkingEffortForModel_ClearsOnEmptyLevel(t *testing.T) {
	agent := &Agent{ThinkingEffort: "low", ThinkingEffortByModel: map[string]string{"glm-5.3": "max"}}

	agent.SetThinkingEffortForModel("glm-5.3", "")
	assert.NotContains(t, agent.ThinkingEffortByModel, "glm-5.3")
	assert.Equal(t, "low", agent.ThinkingEffortForModel("glm-5.3"))

	// An empty model id is a no-op: it would otherwise create a "" key that
	// every model-less lookup shares.
	agent.SetThinkingEffortForModel("", "max")
	assert.NotContains(t, agent.ThinkingEffortByModel, "")
}
