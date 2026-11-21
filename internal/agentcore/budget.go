package agentcore

import "sync"

// TurnBudget is the spend ledger shared by one turn and every sub-agent it
// spawns.
//
// Sharing is the point: a per-loop budget would let four sub-agents each spend
// the user's MaxRounds, so a cap set to stop a runaway turn would multiply
// instead of binding. Rounds and tool calls are counted here, and the
// sub-agent cap lives here too so a child cannot reset it.
//
// The mutex is for the future (parallel sub-agents). The current loop is
// sequential, but a shared struct without one would be a trap the first time
// someone adds concurrency.
type TurnBudget struct {
	mu        sync.Mutex
	rounds    int
	toolCalls int
	subagents int
}

// NewTurnBudget creates an empty ledger.
func NewTurnBudget() *TurnBudget { return &TurnBudget{} }

// AddRound counts one model round and returns its 1-based ordinal.
//
// The ordinal is also the tool-call id prefix for gateways that omit ids (see
// roundAccumulator.result), so sharing it keeps parent and child ids from
// colliding.
func (b *TurnBudget) AddRound() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rounds++
	return b.rounds
}

// AddToolCall counts one tool invocation.
func (b *TurnBudget) AddToolCall() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolCalls++
}

// TakeSubagent reserves one sub-agent slot and returns its 1-based ordinal.
//
// It reports false when the cap is reached. A limit of zero means unlimited,
// matching MaxRounds and MaxToolCalls. A refused reservation must not count:
// the model may keep asking after being told no, and each refusal would
// otherwise push the cap further away.
func (b *TurnBudget) TakeSubagent(limit int) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit > 0 && b.subagents >= limit {
		return 0, false
	}
	b.subagents++
	return b.subagents, true
}

// Rounds reports the model calls spent so far.
func (b *TurnBudget) Rounds() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rounds
}

// ToolCalls reports the tool invocations spent so far.
func (b *TurnBudget) ToolCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.toolCalls
}
