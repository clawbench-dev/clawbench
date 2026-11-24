package trae

import "clawbench/internal/model"

// discoverTraeModels always reports that the model list is unknown.
//
// TraeCode CLI ships no public model list: the models depend on the
// enterprise deployment and the official site publishes no capability matrix.
// A static catalog would therefore be a guess, and a guessed id is worse than
// no id — the picker would offer a model the agent cannot select. Returning an
// empty list with a reason makes the UI show "unknown" instead of a list that
// silently fails on the first turn.
//
// UNVERIFIED: if a real `traecli` turns out to expose the list (a `models`
// subcommand, or a `model` config option in `session/new`), replace this with
// a plugin probe that reads it.
func discoverTraeModels() ([]model.AgentModel, string) {
	return nil, "trae: model list unknown (no verified discovery source for traecli)"
}
