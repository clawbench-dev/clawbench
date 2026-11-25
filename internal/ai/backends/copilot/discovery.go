package copilot

import "clawbench/internal/model"

// discoverCopilotModels reports that the model list is unknown before a session
// exists.
//
// There is no `copilot models` subcommand, and the catalog is resolved on
// GitHub's side, behind an account or a BYOK provider: the CLI only learns it at
// `session/new`, whose response carries `models.availableModels` (and a `model`
// config option afterwards). That response is what the chat path displays once
// the first session exists; probing it here would mean opening a second session
// just to read it.
//
// Returning an empty list is deliberate. A guessed catalog would be worse: the
// picker would offer ids this CLI cannot select, and an id it cannot select
// silently runs GitHub's default model, so the user would see one model and the
// turn would run another.
func discoverCopilotModels() ([]model.AgentModel, string) {
	return nil, "copilot: the model list is reported over ACP (session/new); nothing to probe before the first session"
}
