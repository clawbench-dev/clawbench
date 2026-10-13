package handler

import (
	"strings"

	"clawbench/internal/skill"
)

// skill_command.go handles the user's explicit "#<skill-name>" reference in a
// chat message.
//
// Unlike the automatic skill table (service.AppendSkillsSection, gated by
// Skills.Enabled), an explicit reference is a direct user instruction: it is
// injected regardless of the switch, which is why its resolution does NOT go
// through Registry.InjectedForProject.
//
// A skill is not an executable command — the agent "runs" it by reading its
// SKILL.md. So the injection is an instruction to read that file, carrying the
// resolved absolute path (an agent does not own the skill's directory and has
// no other way to find it). The path, not the body, is injected: the model
// reads on demand, keeping the prompt small — the same contract the system
// prompt table uses.

// skillInjectTemplate is the instruction injected when the user sends a
// message beginning with "#<skill-name>".
//
// Placeholders: {{SKILL_NAME}}, {{SKILL_PATH}}
const skillInjectTemplate = `[The user explicitly selected the skill "{{SKILL_NAME}}" for this request. Read its SKILL.md at the path below and follow its instructions.]

Skill: {{SKILL_NAME}}
SKILL.md: {{SKILL_PATH}}
`

// parseSkillCommand reports whether rawMsg is an explicit skill reference and
// returns the referenced name (without the leading "#").
//
// Syntax: the message must START with "#<name>" — the "#" is the first
// non-whitespace character. A "#" later in the body is plain text and never
// triggers (so "#tag" in prose, "issue #12" and the like are unaffected).
//
// Exactly ONE reference is allowed: a second "#<name>" token immediately after
// the first makes the whole message a non-command (fail-closed). Two
// references have no defined priority, and silently picking one would inject a
// skill the user did not intend.
//
// The name runs to the next whitespace and must be non-empty and free of "#".
func parseSkillCommand(rawMsg string) (string, bool) {
	trimmed := strings.TrimLeft(rawMsg, " \t\r\n")
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}

	// Fields collapses runs of whitespace, so the token split matches what the
	// user sees regardless of tabs or multiple spaces.
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}

	name := strings.TrimPrefix(fields[0], "#")
	if name == "" || strings.Contains(name, "#") {
		return "", false
	}
	// A second "#..." token in the same leading run means more than one
	// reference: refuse rather than guess.
	if len(fields) > 1 && strings.HasPrefix(fields[1], "#") {
		return "", false
	}
	return name, true
}

// processSkillCommand renders the injection for a "#<skill-name>" message, or
// returns rawMsg unchanged when the message is not an explicit skill reference
// or the name resolves to no known skill.
//
// "Unchanged" is the same contract processClawbenchCommand uses: the caller
// treats an unchanged result as "no injection". An unknown name deliberately
// falls through to a normal message rather than erroring — the user may have
// typed a "#" for another purpose, and the text is still worth sending.
func processSkillCommand(rawMsg, projectPath string) string {
	name, ok := parseSkillCommand(rawMsg)
	if !ok {
		return rawMsg
	}
	s, found := skill.Global().ResolveForProject(name, projectPath)
	if !found {
		return rawMsg
	}

	tmpl := strings.ReplaceAll(skillInjectTemplate, "{{SKILL_NAME}}", promptLine(s.Name))
	tmpl = strings.ReplaceAll(tmpl, "{{SKILL_PATH}}", promptLine(s.Path))
	return tmpl
}

// promptLine neutralizes a value for single-line interpolation into a prompt:
// newlines (and other line-breaking control characters) are replaced with a
// space so a crafted skill name or path cannot forge extra prompt lines.
func promptLine(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// processCommandInjection is the single entry point for user-message prompt
// injection. It dispatches to the built-in "/cb-*" renderer first and to the
// explicit "#skill" reference otherwise.
//
// Every injection site (direct send, queue drain, group host prompt) must call
// THIS function rather than the two renderers directly: the sites used to
// disagree on which commands existed, and a single dispatcher is what keeps
// them from drifting as new command kinds are added.
//
// Contract matches the individual renderers: a non-command (or an unknown
// skill name) is returned unchanged, which the caller reads as "no injection".
func processCommandInjection(rawMsg, projectPath, sessionID string) (string, error) {
	if IsClawbenchCommand(rawMsg) {
		return processClawbenchCommand(rawMsg, projectPath, sessionID)
	}
	return processSkillCommand(rawMsg, projectPath), nil
}
