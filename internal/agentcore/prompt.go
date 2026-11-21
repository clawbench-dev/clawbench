package agentcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// ProjectRulesFile is the primary file whose contents are injected into the
// system prompt as standing project rules.
const ProjectRulesFile = "AGENTS.md"

// ProjectRulesFiles lists every rules filename we look for, in priority order.
//
// The FIRST file that exists wins and the rest are not read at all. Reading
// them all would be worse, not better: the same rule is often duplicated across
// both names, so a repo that kept its CLAUDE.md alongside a newer AGENTS.md
// would pay for the same instructions twice on every round, and any place the
// two disagree would hand the model a contradiction with no way to resolve it.
//
// AGENTS.md leads because it is the format the ecosystem is converging on
// (Codex, Cursor and Copilot all read it); CLAUDE.md follows because repos
// migrating from Claude Code commonly keep it; CODEBUDDY.md last because it is
// specific to one vendor. The ordering matches service.projectContextFiles —
// two lists that disagree would have an agent obeying one rules file for its
// prompt and a different one for its project context.
var ProjectRulesFiles = []string{ProjectRulesFile, "CLAUDE.md", "CODEBUDDY.md"}

// MaxProjectRulesBytes caps how much of the rules file is injected.
//
// A rules file is instructions, not data: at some point its bulk competes with
// the task itself, and every byte is paid for on EVERY round of EVERY turn
// (the system prompt is re-sent each time). 32 KiB is far more than a real
// rules file needs and small enough that a runaway one cannot crowd out the
// conversation. The cut is announced in-prompt so the model knows the rules it
// is reading are incomplete rather than assuming it has them all.
const MaxProjectRulesBytes = 32 << 10

// loadProjectRules reads the first existing rules file in ProjectRulesFiles
// order for injection into the system prompt.
//
// Returns "" when there is no workDir, no file from the list exists, the file
// is unreadable, or it is empty. Rules are a nice-to-have: a permissions error
// or a stray directory named AGENTS.md must not fail the turn, so every error
// path is a silent skip. A directory occupying a name does NOT stop the search
// — the next candidate is still tried, since a repo can legitimately have a
// `CLAUDE.md/` directory and an `AGENTS.md` file.
//
// Read permission on the agent's own working directory is not assumed — the
// file tools are gated by the tool policy, but the rules file is read here
// regardless, which is why this stays a plain os.ReadFile with no policy check:
// it returns the user's own instructions to the user's own agent.
//
// The confined-to-workdir question does not arise: every path is inside
// workDir by construction, so this cannot be used to read outside it.
func loadProjectRules(workDir string) string {
	if workDir == "" {
		return ""
	}

	for _, name := range ProjectRulesFiles {
		path := filepath.Join(workDir, name)

		fi, err := os.Stat(path)
		if err != nil || fi.IsDir() {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		truncated := false
		if len(data) > MaxProjectRulesBytes {
			data = data[:MaxProjectRulesBytes]
			truncated = true
		}

		rules := strings.TrimSpace(string(data))
		if rules == "" {
			// An empty file is a deliberate "no rules here", not a reason to
			// fall through to a lower-priority name: the user wrote the file
			// that wins the search and left it blank.
			return ""
		}

		// Cut mid-rune or mid-line, the tail can be garbage; say so rather than
		// letting the model treat a half sentence as a complete rule. The name
		// reported is the one actually read, not the primary constant.
		if truncated {
			rules += fmt.Sprintf("\n\n[The rest of %s was not included: the file exceeds the %d-byte limit "+
				"for injected project rules. Read the file directly for the remainder.]",
				name, MaxProjectRulesBytes)
		}
		return rules
	}

	return ""
}

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
//  3. project-rules — the working directory's AGENTS.md, unwrapped, so it
//     reads as the project's own instructions rather than ours. Placed above
//     the mechanical sections because it is user-authored, and above rules so a
//     project rule that contradicts a generic one wins.
//  4. rules — how to use them, including what is NOT available here.
//  5. environment — where it is and what it is confined to.
//
// projectRules is passed in already loaded (see loadProjectRules) rather than
// read here: this function stays pure so it can be tested without a filesystem,
// and so a missing or unreadable rules file cannot alter what the mechanical
// sections say.
func buildSystemPrompt(agentPrompt, projectRules, workDir string, toolList []tools.Tool, policy model.ToolPolicy) string {
	var b strings.Builder

	if s := strings.TrimSpace(agentPrompt); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}

	// Unwrapped and headed by a line that says what it is: dropped in as a bare
	// block the model has no way to tell project rules from its own
	// instructions, and conflates the two in its replies.
	//
	// The heading deliberately does NOT name the file: the text may have come
	// from any candidate in ProjectRulesFiles, and this function is pure — it
	// cannot know which one that was. Naming the primary constant here would
	// tell the model "AGENTS.md" when the rules actually came from CLAUDE.md,
	// which is exactly the kind of false detail it would then act on.
	if s := strings.TrimSpace(projectRules); s != "" {
		b.WriteString("The project you are working in defines the following rules in its project rules file. Follow them:\n\n")
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
		// Two different worlds, so two different rules. Telling the model its
		// paths are refused when they are not makes it refuse work it could do
		// (and waste rounds re-reading inside the project); telling it the disk
		// is open when it is fenced makes it burn a failed call per attempt.
		if policy.ConfinesToWorkDir() {
			add("Every file path must stay inside the working directory. Paths outside it are refused, so do not try.")
		} else {
			add("Paths outside the working directory are allowed when the task needs them: " +
				"use an absolute path (or ../) to reach one. Relative paths resolve against the working directory.")
		}
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
