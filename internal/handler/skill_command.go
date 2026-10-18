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
//
// The reference may appear ANYWHERE in the message, not only at the start: the
// user's natural writing order is "describe the problem, then name the skill",
// and "#" is ClawBench's own protocol (unlike "/", which agents route by
// prefix), so its trigger position is ours to define. Detection stays
// token-based, so prose hashes ("issue #12"), mid-word hashes ("C#") and URL
// fragments ("...#section") are unaffected.

// skillInjectTemplate is the instruction injected for each "#<skill-name>"
// reference found in the message.
//
// Placeholders: {{SKILL_NAME}}, {{SKILL_PATH}}
const skillInjectTemplate = `[The user explicitly selected the skill "{{SKILL_NAME}}" for this request. Read its SKILL.md at the path below and follow its instructions.]

Skill: {{SKILL_NAME}}
SKILL.md: {{SKILL_PATH}}
`

// skillRefPunct terminates a "#name" token, so "#pdf," and "#pdf。然后" still
// resolve to "pdf".
//
// Chinese prose has no spaces between sentences, so a reference is very often
// followed immediately by punctuation ("用 #pdf，然后总结") — trailing-only
// trimming would leave "pdf，然后总结" and resolve nothing. The name therefore
// ENDS at the first punctuation character, exactly like it ends at whitespace.
//
// Only the terminator set is shared with the frontend (completionMatch.ts
// REF_TOKEN_END_RE): inner characters stay part of the name ("user/skill",
// "web-search"), and a leading "#" is what makes it a reference at all.
const skillRefPunct = ",.;:!?)]}，。；：！？）】」』"

// skillRefName returns the name part of a "#<name>" token, cut at the first
// punctuation character. Returns "" when the token carries no name.
func skillRefName(token string) string {
	name := strings.TrimPrefix(token, "#")
	if i := strings.IndexAny(name, skillRefPunct); i >= 0 {
		name = name[:i]
	}
	return name
}

// skillRefCandidates returns every "#<name>" token in msg, in order.
//
// A reference starts a token — the beginning of the message or a whitespace
// boundary — and its name runs to the next whitespace or punctuation.
// `strings.Fields` is the tokenizer, so any Unicode whitespace separates
// tokens, matching the frontend's JS `.trim()`/`\s` semantics.
//
// Whether a candidate actually names a skill is the caller's decision (it needs
// the project path): an unknown name is ordinary text, and a message mixing
// known and unknown hashes ("#pdf fix issue #12") must still run the known one.
func skillRefCandidates(msg string) []string {
	var names []string
	for _, token := range strings.Fields(msg) {
		if !strings.HasPrefix(token, "#") {
			continue
		}
		name := skillRefName(token)
		// A name may not be empty ("#" / "# ...") or contain "#" ("##",
		// "#a#b"): neither can name a skill.
		if name == "" || strings.Contains(name, "#") {
			continue
		}
		names = append(names, name)
	}
	return names
}

// processSkillCommand renders the injection for every resolvable
// "#<skill-name>" reference in rawMsg, or returns rawMsg unchanged when the
// message has none.
//
// "Unchanged" is the same contract processClawbenchCommand uses: the caller
// treats an unchanged result as "no injection". Each resolved reference
// contributes its own template block, in the order it appears. Unknown names
// deliberately fall through to a normal message rather than erroring — the
// user may have typed a "#" for another purpose, and the text is still worth
// sending.
//
// A repeated reference is injected once: asking for the same skill twice adds
// no instruction, only prompt weight.
func processSkillCommand(rawMsg, projectPath string) string {
	var blocks []string
	seen := make(map[string]bool)
	for _, name := range skillRefCandidates(rawMsg) {
		s, found := skill.Global().ResolveForProject(name, projectPath)
		if !found {
			continue
		}
		key := strings.ToLower(s.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		blocks = append(blocks, renderSkillInjection(s))
	}
	if len(blocks) == 0 {
		return rawMsg
	}
	return strings.Join(blocks, "\n")
}

// renderSkillInjection fills skillInjectTemplate for one resolved skill.
func renderSkillInjection(s skill.Skill) string {
	tmpl := strings.ReplaceAll(skillInjectTemplate, "{{SKILL_NAME}}", promptLine(s.Name))
	return strings.ReplaceAll(tmpl, "{{SKILL_PATH}}", promptLine(s.Path))
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
