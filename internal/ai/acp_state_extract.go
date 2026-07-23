package ai

import (
	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACP state extraction — extract mode/thinking/model/config state from ACP responses
// ---------------------------------------------------------------------------

// extractACPModeState extracts ModeState from an ACP NewSessionResponse.
// Returns nil if no modes are available.
func extractACPModeState(sessResp *acp.NewSessionResponse) *ModeState {
	if sessResp == nil {
		return nil
	}
	return extractModeStateFromModes(sessResp.Modes)
}

// extractACPModeStateFromResume extracts ModeState from a ResumeSessionResponse.
func extractACPModeStateFromResume(resumeResp *acp.ResumeSessionResponse) *ModeState {
	if resumeResp == nil {
		return nil
	}
	return extractModeStateFromModes(resumeResp.Modes)
}

func extractModeStateFromModes(modes *acp.SessionModeState) *ModeState {
	if modes == nil {
		return nil
	}

	modeState := &ModeState{
		CurrentModeID: string(modes.CurrentModeId),
	}

	for _, m := range modes.AvailableModes {
		modeState.AvailableModes = append(modeState.AvailableModes, ModeDef{
			ID:   string(m.Id),
			Name: m.Name,
		})
	}

	if len(modeState.AvailableModes) == 0 && modeState.CurrentModeID == "" {
		return nil
	}

	return modeState
}

// extractACPConfigOptions extracts mode-relevant ConfigOptionState from an ACP NewSessionResponse.
// Returns nil if no mode-relevant config options are available.
func extractACPConfigOptions(sessResp *acp.NewSessionResponse) *ConfigOptionState {
	if sessResp == nil {
		return nil
	}
	return extractConfigOptionsFromOpts(sessResp.ConfigOptions)
}

// extractACPConfigOptionsFromResume extracts mode-relevant ConfigOptionState from a ResumeSessionResponse.
func extractACPConfigOptionsFromResume(resumeResp *acp.ResumeSessionResponse) *ConfigOptionState {
	if resumeResp == nil {
		return nil
	}
	return extractConfigOptionsFromOpts(resumeResp.ConfigOptions)
}

func extractConfigOptionsFromOpts(opts []acp.SessionConfigOption) *ConfigOptionState {
	if len(opts) == 0 {
		return nil
	}

	for _, opt := range opts {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryMode {
			configState := &ConfigOptionState{
				ConfigID:  string(sel.Id),
				CurrentID: string(sel.CurrentValue),
			}

			optDef := ConfigOptionDef{
				ID:       string(sel.Id),
				Name:     sel.Name,
				Category: "mode",
			}

			mapACPSelectOptions(sel.Options, &optDef)

			configState.Options = append(configState.Options, optDef)
			return configState
		}
	}

	return nil
}

// selectStateFromConfigState derives a SelectState from a ConfigOptionState
// for the given category. This is the generalized version of
// modeStateFromConfigState and thinkingEffortStateFromConfigState.
// Returns nil if the config state doesn't contain options for the given category.
func selectStateFromConfigState(cs *ConfigOptionState, category string) *SelectState {
	if cs == nil {
		return nil
	}
	for _, opt := range cs.Options {
		if opt.Category != category {
			continue
		}
		sel := &SelectState{
			CurrentID: cs.CurrentID,
			Category:  category,
		}
		for _, v := range opt.Values {
			sel.Available = append(sel.Available, SelectOptionDef(v))
		}
		if len(sel.Available) == 0 && sel.CurrentID == "" {
			return nil
		}
		return sel
	}
	return nil
}

// modeStateFromConfigState derives a ModeState from a ConfigOptionState that
// has Category "mode". This handles ACP v2 agents (like OpenCode) that expose
// modes via ConfigOptions instead of the legacy Modes field.
// Returns nil if the config state doesn't contain mode options.
func modeStateFromConfigState(cs *ConfigOptionState) *ModeState {
	sel := selectStateFromConfigState(cs, "mode")
	if sel == nil {
		return nil
	}
	return sel.ToModeState()
}

// thinkingEffortStateFromConfigState derives a ThinkingEffortState from a
// ConfigOptionState that has Category "thought_level". This handles ACP v2
// agents that expose thinking effort via ConfigOptions. Returns nil if the
// config state doesn't contain thought_level options.
func thinkingEffortStateFromConfigState(cs *ConfigOptionState) *ThinkingEffortState {
	sel := selectStateFromConfigState(cs, "thought_level")
	if sel == nil {
		return nil
	}
	return sel.ToThinkingEffortState()
}

// extractACPThinkingEffort extracts ThinkingEffortState from an ACP NewSessionResponse.
// Looks for config options with Category "thought_level". Returns nil if none found.
func extractACPThinkingEffort(sessResp *acp.NewSessionResponse) *ThinkingEffortState {
	if sessResp == nil {
		return nil
	}
	return extractThinkingEffortFromOpts(sessResp.ConfigOptions)
}

// extractACPThinkingEffortFromResume extracts ThinkingEffortState from a ResumeSessionResponse.
func extractACPThinkingEffortFromResume(resumeResp *acp.ResumeSessionResponse) *ThinkingEffortState {
	if resumeResp == nil {
		return nil
	}
	return extractThinkingEffortFromOpts(resumeResp.ConfigOptions)
}

func extractThinkingEffortFromOpts(opts []acp.SessionConfigOption) *ThinkingEffortState {
	if len(opts) == 0 {
		return nil
	}

	for _, opt := range opts {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryThoughtLevel {
			return buildThinkingEffortStateFromSelect(sel)
		}
	}

	return nil
}

// extractACPModelList extracts ModelListState from an ACP NewSessionResponse.
// Looks for config options with Category "model". Returns nil if none found.
func extractACPModelList(sessResp *acp.NewSessionResponse) *ModelListState {
	if sessResp == nil {
		return nil
	}
	return extractModelListFromOpts(sessResp.ConfigOptions)
}

// extractACPModelListFromResume extracts ModelListState from a ResumeSessionResponse.
func extractACPModelListFromResume(resumeResp *acp.ResumeSessionResponse) *ModelListState {
	if resumeResp == nil {
		return nil
	}
	return extractModelListFromOpts(resumeResp.ConfigOptions)
}

func extractModelListFromOpts(opts []acp.SessionConfigOption) *ModelListState {
	if len(opts) == 0 {
		return nil
	}

	for _, opt := range opts {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryModel {
			return buildModelListStateFromSelect(sel)
		}
	}

	return nil
}

// buildConfigOptionStateFromSelect builds a ConfigOptionState from an ACP SessionConfigOptionSelect.
func buildConfigOptionStateFromSelect(sel *acp.SessionConfigOptionSelect, category string) *ConfigOptionState {
	configState := &ConfigOptionState{
		ConfigID:  string(sel.Id),
		CurrentID: string(sel.CurrentValue),
	}

	optDef := ConfigOptionDef{
		ID:       string(sel.Id),
		Name:     sel.Name,
		Category: category,
	}

	mapACPSelectOptions(sel.Options, &optDef)
	configState.Options = append(configState.Options, optDef)
	return configState
}

// buildThinkingEffortStateFromSelect builds a ThinkingEffortState from an ACP thought_level config option.
func buildThinkingEffortStateFromSelect(sel *acp.SessionConfigOptionSelect) *ThinkingEffortState {
	state := &ThinkingEffortState{
		CurrentID: string(sel.CurrentValue),
	}

	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			state.AvailableLevels = append(state.AvailableLevels, ThinkingEffortDef{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if sel.Options.Grouped != nil {
		for _, g := range *sel.Options.Grouped {
			for _, v := range g.Options {
				state.AvailableLevels = append(state.AvailableLevels, ThinkingEffortDef{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}

	if len(state.AvailableLevels) == 0 && state.CurrentID == "" {
		return nil
	}

	return state
}

// buildModelListStateFromSelect builds a ModelListState from an ACP SessionConfigOptionSelect
// with Category "model". Returns nil if no models are available.
func buildModelListStateFromSelect(sel *acp.SessionConfigOptionSelect) *ModelListState {
	state := &ModelListState{
		CurrentModelID: string(sel.CurrentValue),
	}

	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			state.Models = append(state.Models, model.AgentModel{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if sel.Options.Grouped != nil {
		for _, g := range *sel.Options.Grouped {
			for _, v := range g.Options {
				state.Models = append(state.Models, model.AgentModel{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}

	if len(state.Models) == 0 && state.CurrentModelID == "" {
		return nil
	}

	return state
}

// buildSelectStateFromACPSelect builds a SelectState from an ACP SessionConfigOptionSelect
// for the given category. Returns an empty SelectState if no options or current value
// are available. Handles both Ungrouped and Grouped option layouts (flattens groups).
func buildSelectStateFromACPSelect(sel *acp.SessionConfigOptionSelect, category string) SelectState {
	s := SelectState{
		CurrentID: string(sel.CurrentValue),
		Category:  category,
	}

	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			s.Available = append(s.Available, SelectOptionDef{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if sel.Options.Grouped != nil {
		for _, g := range *sel.Options.Grouped {
			for _, v := range g.Options {
				s.Available = append(s.Available, SelectOptionDef{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}

	return s
}

// mapACPSelectOptions extracts ConfigOptionValue entries from ACP SessionConfigSelectOptions.
func mapACPSelectOptions(opts acp.SessionConfigSelectOptions, optDef *ConfigOptionDef) {
	if opts.Ungrouped != nil {
		for _, v := range *opts.Ungrouped {
			optDef.Values = append(optDef.Values, ConfigOptionValue{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if opts.Grouped != nil {
		for _, g := range *opts.Grouped {
			for _, v := range g.Options {
				optDef.Values = append(optDef.Values, ConfigOptionValue{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}
}
