package compaction

import (
	"fmt"
	"strings"
)

// DigestSummary builds a deterministic summary from rendered conversation text.
//
// It is the fallback for a caller with no summarizer configured. It is NOT
// meant to read well: its job is to preserve the FACTS a later turn needs
// (what was asked, which files were touched, which tools ran) when no model is
// available to write prose.
//
// The alternative — refusing to compact — is worse in every case: the turn then
// fails outright on the next model call, and the user loses the work entirely
// instead of losing the wording.
func DigestSummary(rendered string) string {
	userTurns, assistant, tools := collectDigestFacts(rendered)
	if len(userTurns) == 0 && len(tools) == 0 && len(assistant) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Earlier in this conversation:\n")
	writeDigestList(&b, "\nThe user asked, in order:", userTurns, 8, 200)
	writeDigestTools(&b, tools)
	writeDigestList(&b, "\nThe assistant had said:", assistant, 4, 200)
	b.WriteString("\n(The original messages are no longer in the context window.)")
	return b.String()
}

// collectDigestFacts sorts the rendered lines into the three fact lists the
// digest keeps.
func collectDigestFacts(rendered string) (userTurns, assistant, tools []string) {
	for _, line := range strings.Split(strings.TrimSpace(rendered), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[tool call]"):
			// Tool calls are the most useful fact in a digest: they say which
			// files and commands were involved, which is what a later turn
			// needs to avoid redoing work.
			tools = append(tools, strings.TrimSpace(strings.TrimPrefix(line, "[tool call]")))
		case strings.HasPrefix(line, "[tool result]"):
			// Deliberately dropped: the result is usually file content, which
			// is exactly what makes a summary as large as the conversation.
		case strings.HasPrefix(line, "User:"):
			userTurns = append(userTurns, strings.TrimSpace(strings.TrimPrefix(line, "User:")))
		case strings.HasPrefix(line, "Assistant:"):
			assistant = append(assistant, strings.TrimSpace(strings.TrimPrefix(line, "Assistant:")))
		}
	}
	return userTurns, assistant, tools
}

// writeDigestList renders the last n entries of a fact list under a heading.
func writeDigestList(b *strings.Builder, heading string, list []string, n, width int) {
	if len(list) == 0 {
		return
	}
	b.WriteString(heading)
	b.WriteString("\n")
	for _, entry := range tail(list, n) {
		fmt.Fprintf(b, "- %s\n", truncate(entry, width))
	}
}

// writeDigestTools renders the tool list with duplicates removed: a loop that
// ran the same command five times is one fact, not five.
func writeDigestTools(b *strings.Builder, tools []string) {
	if len(tools) == 0 {
		return
	}
	b.WriteString("\nTools used:\n")
	seen := make(map[string]bool, len(tools))
	for _, t := range tail(tools, 20) {
		if seen[t] {
			continue
		}
		seen[t] = true
		fmt.Fprintf(b, "- %s\n", truncate(t, 160))
	}
}

// tail returns the last n entries, or all of them when shorter.
func tail(list []string, n int) []string {
	if n <= 0 || len(list) <= n {
		return list
	}
	return list[len(list)-n:]
}
