package agentcore

import (
	"fmt"
	"strings"

	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// buildSystemPrompt assembles the system prompt for a run.
//
// The order matters: the agent's own instructions come first (that is what the
// user wrote and what defines the agent), then the mechanical facts the model
// cannot infer — where it is, what it may do, and how to behave when it cannot
// do something.
func buildSystemPrompt(agentPrompt, workDir string, toolList []tools.Tool, policy model.ToolPolicy) string {
	var b strings.Builder

	if s := strings.TrimSpace(agentPrompt); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}

	b.WriteString("## Environment\n")
	if workDir != "" {
		fmt.Fprintf(&b, "Working directory: %s\n", workDir)
		b.WriteString("All file paths are relative to it, and you cannot read or write anything outside it.\n")
	} else {
		b.WriteString("No working directory is set, so file tools are unavailable.\n")
	}

	b.WriteString("\n## Tools\n")
	if len(toolList) == 0 {
		b.WriteString("You have no tools in this session. Answer from what you already know and say so " +
			"when the answer would need a file or a command.\n")
	} else {
		names := make([]string, 0, len(toolList))
		for _, t := range toolList {
			names = append(names, t.Name())
		}
		fmt.Fprintf(&b, "Available: %s.\n", strings.Join(names, ", "))
		b.WriteString("Do not claim to have read or run something unless you actually called the tool.\n")
	}

	// The capability summary is stated explicitly because a missing tool reads
	// to the model as "I forgot", not "I am not allowed". Saying it plainly is
	// what stops it from spending rounds hunting for a way to write a file.
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
		fmt.Fprintf(&b, "\nYou cannot %s in this session. If the task needs it, explain what you would "+
			"do instead of pretending it happened.\n", strings.Join(denied, ", "))
	}

	return strings.TrimRight(b.String(), "\n")
}
