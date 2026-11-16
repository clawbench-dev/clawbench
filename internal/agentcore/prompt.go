package agentcore

import (
	"fmt"
	"strings"

	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// buildSystemPrompt assembles the system prompt for a run.
//
// The shape follows pi's (see docs: pi's `buildSystemPromptSections`): a plain
// preamble, then one XML-ish section per concern. Sections are wrapped rather
// than flattened into prose because a model reads `<rules>` as a list to follow
// and prose as something to summarize — the same text behaves differently.
//
// Section order is deliberate:
//
//  1. preamble — the agent's own instructions. Unwrapped and first, because it
//     is what the user wrote and it must outrank the mechanical text below it.
//  2. tools — one line per tool, so the model can CHOOSE between them. Bare
//     names (the first version) tell it nothing, and it then shells out to
//     bash for work a dedicated tool does better.
//  3. rules — how to use them, including what is NOT available here.
//  4. environment — where it is and what it is confined to.
func buildSystemPrompt(agentPrompt, workDir string, toolList []tools.Tool, policy model.ToolPolicy) string {
	var b strings.Builder

	if s := strings.TrimSpace(agentPrompt); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}

	b.WriteString(section("tools", renderToolList(toolList)))
	b.WriteString(section("rules", renderRules(toolList, policy, workDir != "")))
	b.WriteString(section("environment", renderEnvironment(workDir)))
	b.WriteString(section("output", renderOutputRules()))

	return strings.TrimRight(b.String(), "\n")
}

// section wraps body in a named tag. Empty bodies produce nothing, so a section
// with no content never leaves a dangling empty tag for the model to interpret.
func section(name, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return "<" + name + ">\n" + body + "\n</" + name + ">\n\n"
}

// renderToolList renders one line per tool: its name and what it is for.
func renderToolList(toolList []tools.Tool) string {
	if len(toolList) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range toolList {
		snippet := t.Snippet()
		if snippet == "" {
			b.WriteString("- " + t.Name() + "\n")
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", t.Name(), snippet)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderRules collects the usage rules.
//
// The denied-capability line is stated plainly because a MISSING tool reads to
// the model as "I forgot" rather than "I am not allowed" — without it the agent
// spends rounds hunting for a way to write a file that will never appear.
func renderRules(toolList []tools.Tool, policy model.ToolPolicy, hasWorkDir bool) string {
	var rules []string
	seen := make(map[string]bool, 8)
	add := func(rule string) {
		rule = strings.TrimSpace(rule)
		if rule == "" || seen[rule] {
			return
		}
		seen[rule] = true
		rules = append(rules, rule)
	}

	// Per-tool advice, in the order the tools are declared.
	for _, t := range toolList {
		for _, g := range t.Guidelines() {
			add(g)
		}
	}

	if hasWorkDir {
		add("Every file path must stay inside the working directory. Paths outside it are refused, so do not try.")
	} else {
		add("No working directory is set, so the file tools cannot be used at all.")
	}

	// Capability denials. Phrased as "cannot" rather than "do not", because the
	// restriction is not a preference the model may reason its way around.
	var denied []string
	if !policy.Has(model.CapFSRead) {
		denied = append(denied, "read files")
	}
	if !policy.Has(model.CapFSWrite) {
		denied = append(denied, "write or modify files")
	}
	if !policy.Has(model.CapExec) {
		denied = append(denied, "run shell commands")
	}
	if len(denied) > 0 {
		add(fmt.Sprintf("You cannot %s in this session. If the task needs it, say so plainly "+
			"instead of pretending it happened.", joinWithOr(denied)))
	}

	if len(toolList) == 0 {
		add("You have no tools in this session. Answer from what you already know and say when " +
			"an answer would need a file or a command.")
	}

	for i, r := range rules {
		rules[i] = "- " + r
	}
	return strings.Join(rules, "\n")
}

// joinWithOr renders a list as "a, b or c".
//
// Used for the capability denials so three missing capabilities do not read as
// one long compound noun ("write or modify files, run shell commands") — the
// model must be able to see that these are separate restrictions.
func joinWithOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
	}
}

// renderEnvironment states where the agent is working.
func renderEnvironment(workDir string) string {
	if workDir == "" {
		return ""
	}
	// Forward slashes on every platform: a Windows path in the prompt is read
	// by the model as an escape sequence far more often than as a real path.
	return "Working directory: " + strings.ReplaceAll(workDir, `\`, "/")
}

// renderOutputRules states the response conventions.
//
// These are the two rules pi ships unconditionally, and they matter more for a
// tool-using agent than for a chat one: a long narration of file contents is
// worse than a short answer that names the file, and an agent that writes
// "I updated X" without having called write is actively misleading.
func renderOutputRules() string {
	return strings.Join([]string{
		"- Be concise. Report what you did, not a transcript of your thinking.",
		"- Name file paths explicitly when you refer to files.",
		"- Never claim to have read, run or changed something you did not actually call a tool for.",
	}, "\n")
}
