package trae

import "clawbench/internal/model"

// discoverTraeModels always reports that the model list is unknown.
//
// `traecli models --json` exists, but it answers `[]` without a credential and
// the list depends on the enterprise account. The JSON shape of a populated
// answer has never been observed, and a guessed field name would produce an
// empty (or wrong) picker — worse than an honest "unknown". Returning a reason
// makes the UI say so instead of offering ids that fail on the first turn.
//
// TODO(trae/models): once an enterprise account is available, capture
// `traecli models --json` and parse it here; the picker then fills itself.
func discoverTraeModels() ([]model.AgentModel, string) {
	return nil, "trae: model list unknown (no verified discovery source for traecli)"
}
