package api

import (
	"fmt"
	"strings"
)

// Command identifies a built-in slash command whose prompt fragment is derived
// from the spec.
type Command string

const (
	// CommandChatSearch backs /cb-chatsearch — searching past conversations.
	CommandChatSearch Command = "chatsearch"
	// CommandTask backs /cb-task — managing tasks.
	CommandTask Command = "task"
	// CommandUsage backs /cb-usage — token / cost usage statistics.
	CommandUsage Command = "usage"
)

// Spec tags referenced by commandOperations. They are named constants so the
// selection binding cannot drift by a typo.
const (
	tagRAG    = "RAG"
	tagTasks  = "Tasks"
	tagAgents = "Agents"
)

// commandOperations lists the exact operations each command exposes to the AI,
// paired with the spec tag they are expected to carry.
//
// Selection is by operationId rather than by tag on purpose: a tag is too
// coarse (the RAG tag also covers index-rebuild and summarize operations, and
// the Agents tag covers create/delete — none of which a slash command should
// ever trigger). The tag is kept alongside as a binding check so that a
// retagged or renamed operation fails the drift test instead of silently
// vanishing from the prompt.
var commandOperations = map[Command][]struct {
	ID  string
	Tag string
}{
	CommandChatSearch: {
		{"ragSearch", tagRAG},
		{"ragMessage", tagRAG},
		{"ragSession", tagRAG},
		{"ragSessionSearch", tagRAG},
	},
	CommandTask: {
		{"tasksList", tagTasks},
		{"tasksCreate", tagTasks},
		{"taskGet", tagTasks},
		{"taskUpdate", tagTasks},
		{"taskDelete", tagTasks},
		{"taskExecutions", tagTasks},
		{"agentsList", tagAgents},
		// An event-triggered task only fires for repositories its project is
		// bound to, so the AI must be able to check the binding before creating
		// one — otherwise it would happily configure a task that never runs.
		// Only the read operation is exposed; binding mutation stays out of the
		// task command's reach.
		{"forgeBindingGet", "Forge"},
	},
	CommandUsage: {
		{"usageStats", "System"},
	},
}

// RenderCommand renders the endpoint reference for a built-in slash command.
//
// The output is deliberately terse: the AI needs to know which endpoint to call
// and which fields to send, not the full response schemas. Responses and
// component schemas are omitted because they dominate the document size while
// adding nothing the model must act on.
//
// Output is deterministic (paths sorted, fields in document order) so prompt
// content is stable across requests and cacheable.
func RenderCommand(cmd Command) (string, error) {
	specs, ok := commandOperations[cmd]
	if !ok {
		return "", fmt.Errorf("api: unknown command %q", cmd)
	}
	ids := make([]string, 0, len(specs))
	required := make(map[string]string, len(specs))
	for _, s := range specs {
		ids = append(ids, s.ID)
		required[s.ID] = s.Tag
	}

	eps, err := endpointsForOperations(ids, required)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for i, ep := range eps {
		if i > 0 {
			b.WriteString("\n")
		}
		writeEndpoint(&b, ep)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// OperationIDs returns the operationIds a command renders, for drift tests.
func OperationIDs(cmd Command) []string {
	specs := commandOperations[cmd]
	ids := make([]string, 0, len(specs))
	for _, s := range specs {
		ids = append(ids, s.ID)
	}
	return ids
}

func writeEndpoint(b *strings.Builder, ep endpoint) {
	b.WriteString(ep.Method)
	b.WriteString(" ")
	b.WriteString(ep.Path)
	if ep.Summary != "" {
		b.WriteString(" — ")
		b.WriteString(ep.Summary)
	}
	b.WriteString("\n")

	// Path parameters are positional; render them as the concrete substitution
	// the AI must make.
	if len(ep.PathParams) > 0 {
		names := make([]string, 0, len(ep.PathParams))
		for _, p := range ep.PathParams {
			names = append(names, p.Name)
		}
		b.WriteString("  path: ")
		b.WriteString(strings.Join(names, ", "))
		b.WriteString("\n")
	}

	if len(ep.QueryParams) > 0 {
		parts := make([]string, 0, len(ep.QueryParams))
		repeatable := false
		for _, p := range ep.QueryParams {
			parts = append(parts, paramLabel(p.Name, p.Required, p.Schema))
			if p.Schema != nil && p.Schema.Type == "array" {
				repeatable = true
			}
		}
		b.WriteString("  query: ")
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n")
		// An array-typed query parameter is OpenAPI's encoding for "repeat this
		// parameter", not "pass a list in one value". Saying so is essential:
		// the enum values shown inline otherwise read as a menu to pick from,
		// and sending them comma-separated is rejected by the server.
		if repeatable {
			b.WriteString("  repeat: array-valued params must be repeated once per value (e.g. dims=model&dims=backend); comma-separated values are rejected\n")
		}
	}

	if len(ep.BodyFields) > 0 {
		parts := make([]string, 0, len(ep.BodyFields))
		for _, f := range ep.BodyFields {
			parts = append(parts, paramLabel(f.Name, f.Required, f.Schema))
		}
		body := "  body"
		if !ep.BodyRequired {
			body += " (optional)"
		}
		b.WriteString(body)
		b.WriteString(": ")
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n")

		// A body field's constraints (an enum's permitted values, "required
		// when X") live in its description, not in its name. Rendering them on
		// their own line keeps the compact field list readable while still
		// telling the AI what each field accepts.
		for _, f := range ep.BodyFields {
			if f.Description == "" {
				continue
			}
			b.WriteString("  field ")
			b.WriteString(f.Name)
			b.WriteString(": ")
			b.WriteString(collapseWhitespace(f.Description))
			b.WriteString("\n")
		}
	}

	// Descriptions carry usage notes the AI must honor (e.g. the taskUpdate
	// action enumeration), so they are preserved but collapsed to one line.
	if ep.Description != "" {
		b.WriteString("  note: ")
		b.WriteString(collapseWhitespace(ep.Description))
		b.WriteString("\n")
	}
}

// paramLabel renders "name" or "name (required)", annotating the type or the
// permitted values when known. An enum replaces the type entirely — listing the
// legal values is strictly more useful than knowing it is an array, and keeps
// the AI from guessing (dims must be model|backend|agent, not "project").
func paramLabel(name string, required bool, sc *schema) string {
	label := name
	if vals := sc.enumValues(); len(vals) > 0 {
		label += ":" + strings.Join(vals, "|")
	} else if t := sc.displayType(); t != "" && t != "string" {
		label += ":" + t
	}
	if required {
		label += " (required)"
	}
	return label
}

// collapseWhitespace flattens a YAML block scalar into a single line so the
// rendered fragment stays compact and line-oriented.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
