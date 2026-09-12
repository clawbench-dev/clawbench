package service

import (
	"fmt"
	"strings"

	"clawbench/internal/forge"
)

// EventContext carries the forge event payload that an event-triggered task
// renders into its prompt. Fields that do not apply to a given event stay empty
// and their line is omitted (see RenderEventContext), so the prompt never
// contains a dangling label like "流水线状态：" with no value.
type EventContext struct {
	EventType      string
	Repo           string // owner/repo
	ItemNumber     int
	ItemType       string // issue | pr
	Title          string
	URL            string
	Author         string
	State          string
	CommentBody    string
	PipelineStatus string
	PipelineURL    string
}

// EventContextFromChange builds the context for a derived event. item supplies
// the human-readable fields; change supplies the transition and the acting user.
func EventContextFromChange(repo ForgeRepoRef, item forge.Item, change forge.Change) EventContext {
	// The event author is the acting user when known (the commenter, or the
	// opener), falling back to the item creator for state transitions where the
	// platform does not report who performed them.
	author := change.Actor
	if author == "" {
		author = item.Author.Login
	}
	return EventContext{
		EventType:      string(change.Type),
		Repo:           repo.Owner + "/" + repo.Repo,
		ItemNumber:     item.Number,
		ItemType:       string(item.Type),
		Title:          item.Title,
		URL:            item.URL,
		Author:         author,
		State:          string(item.State),
		CommentBody:    change.CommentBody,
		PipelineStatus: change.PipelineStatus,
		PipelineURL:    change.PipelineURL,
	}
}

// eventContextVar describes one variable in the fixed event-context block. The
// placeholder is the template token; the value extracts it from a context.
type eventContextVar struct {
	label       string
	placeholder string
	value       func(EventContext) string
	// eventScoped, when non-empty, restricts the variable to events of that type
	// (used by the read-only UI template). Empty means "applies to all events".
	eventScoped string
}

// eventContextVars is the ordered variable set. Order is stable so the rendered
// prompt and the read-only UI template stay in lockstep.
var eventContextVars = []eventContextVar{
	{"事件类型", "EVENT_TYPE", func(e EventContext) string { return e.EventType }, ""},
	{"仓库", "REPO", func(e EventContext) string { return e.Repo }, ""},
	{"条目", "ITEM_TYPE #ITEM_NUMBER", func(e EventContext) string {
		return fmt.Sprintf("%s #%d", e.ItemType, e.ItemNumber)
	}, ""},
	{"标题", "TITLE", func(e EventContext) string { return e.Title }, ""},
	{"链接", "URL", func(e EventContext) string { return e.URL }, ""},
	{"作者", "AUTHOR", func(e EventContext) string { return e.Author }, ""},
	{"状态", "STATE", func(e EventContext) string { return e.State }, ""},
	{"评论内容", "COMMENT_BODY", func(e EventContext) string { return e.CommentBody }, string(forge.EventCommented)},
	{"流水线状态", "PIPELINE_STATUS", func(e EventContext) string { return e.PipelineStatus }, string(forge.EventPipeline)},
	{"流水线链接", "PIPELINE_URL", func(e EventContext) string { return e.PipelineURL }, string(forge.EventPipeline)},
}

// RenderEventContext renders the fixed, read-only context block prepended to an
// event task's user prompt. Variables with no value are omitted entirely rather
// than rendered empty, so the block stays coherent for every event type.
func RenderEventContext(ec EventContext) string {
	var b strings.Builder
	b.WriteString("## Forge 事件\n")
	for _, v := range eventContextVars {
		value := v.value(ec)
		if value == "" {
			continue
		}
		// Collapse multi-line values (comment bodies) so one variable stays one
		// bullet in the block.
		value = strings.ReplaceAll(value, "\r\n", "\n")
		value = strings.ReplaceAll(value, "\n", "\n  ")
		fmt.Fprintf(&b, "- %s：%s\n", v.label, value)
	}
	return strings.TrimRight(b.String(), "\n")
}

// EventPromptTemplate returns the read-only placeholder block shown in the task
// form. Only variables relevant to the subscribed event types are listed, so
// the user sees exactly what will be injected. An empty subscription lists all
// variables.
func EventPromptTemplate(eventTypes []string) string {
	subscribed := make(map[string]bool, len(eventTypes))
	for _, t := range eventTypes {
		subscribed[t] = true
	}
	showAll := len(subscribed) == 0

	var b strings.Builder
	b.WriteString("## Forge 事件\n")
	for _, v := range eventContextVars {
		if !showAll && v.eventScoped != "" && !subscribed[v.eventScoped] {
			continue
		}
		fmt.Fprintf(&b, "- %s：{{%s}}\n", v.label, v.placeholder)
	}
	return strings.TrimRight(b.String(), "\n")
}
