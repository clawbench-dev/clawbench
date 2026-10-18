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
