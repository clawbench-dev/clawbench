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
	ItemType       string // issue | pr | pipeline
	Title          string
	URL            string
	Author         string
	State          string
	CommentBody    string
	PipelineStatus string
	PipelineURL    string
	// ActorIsSelf reports whether the acting user is the credential's own
	// account. It is set by the trigger (which is what can resolve the
	// credential) and rendered so a prompt can decide for itself whether to act
	// — the code does not suppress such events on its own.
	ActorIsSelf bool
}

// EventContextFromChange builds the context for a derived event. item supplies
// the human-readable fields; change supplies the transition and the acting user.
//
// ActorIsSelf is left false here; the trigger fills it in, because only the
// trigger can resolve the credential's login.
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
	// requiresItem marks a variable that only makes sense for an event attached
	// to an issue or PR. A pipeline run has no item, so the variable renders
	// nothing for it (otherwise the prompt would read "pipeline #0") and is
	// hidden from the template when the subscription is exclusively
	// repository-targeted events.
	requiresItem bool
}

// eventContextVars is the ordered variable set. Order is stable so the rendered
// prompt and the read-only UI template stay in lockstep.
var eventContextVars = []eventContextVar{
	{label: "事件类型", placeholder: "EVENT_TYPE", value: func(e EventContext) string { return e.EventType }},
	{label: "仓库", placeholder: "REPO", value: func(e EventContext) string { return e.Repo }},
	{
		label:       "条目",
		placeholder: "ITEM_TYPE #ITEM_NUMBER",
		value: func(e EventContext) string {
			if e.ItemType == string(forge.ItemTypePipeline) {
				return ""
			}
			return fmt.Sprintf("%s #%d", e.ItemType, e.ItemNumber)
		},
		requiresItem: true,
	},
	{label: "标题", placeholder: "TITLE", value: func(e EventContext) string { return e.Title }},
	{label: "链接", placeholder: "URL", value: func(e EventContext) string { return e.URL }},
	{label: "作者", placeholder: "AUTHOR", value: func(e EventContext) string { return e.Author }},
	{label: "状态", placeholder: "STATE", value: func(e EventContext) string { return e.State }},
	{
		label: "评论内容", placeholder: "COMMENT_BODY",
		value:       func(e EventContext) string { return e.CommentBody },
		eventScoped: string(forge.EventCommented),
	},
	{
		label: "流水线状态", placeholder: "PIPELINE_STATUS",
		value:       func(e EventContext) string { return e.PipelineStatus },
		eventScoped: string(forge.EventPipeline),
	},
	{
		label: "流水线链接", placeholder: "PIPELINE_URL",
		value:       func(e EventContext) string { return e.PipelineURL },
		eventScoped: string(forge.EventPipeline),
	},
	{
		// Whether the run was triggered by this installation's own credential.
		// The code deliberately does not suppress such events, so this is the
		// prompt's only way to tell — the AI cannot know the credential's login
		// on its own. Only meaningful for pipeline events: for issue/PR events
		// the equivalent decision is already made in code.
		label:       "是否自身触发",
		placeholder: "ACTOR_IS_SELF",
		value: func(e EventContext) string {
			if e.ActorIsSelf {
				return "是"
			}
			return "否"
		},
		eventScoped: string(forge.EventPipeline),
	},
}

// RenderEventContext renders the fixed, read-only context block prepended to an
// event task's user prompt. Variables with no value are omitted entirely rather
// than rendered empty, so the block stays coherent for every event type.
func RenderEventContext(ec EventContext) string {
	var b strings.Builder
	b.WriteString("## Forge 事件\n")
	for _, v := range eventContextVars {
		// A variable scoped to another event type must not appear. Without this
		// check a scoped variable whose value is never empty (the yes/no
		// self-authorship flag) would leak into every other event's block.
		if v.eventScoped != "" && v.eventScoped != ec.EventType {
			continue
		}
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

// isRepoTargetedTransition reports whether a subscription key names an event
// that belongs to the repository rather than to an issue or PR.
func isRepoTargetedTransition(key string) bool {
	for _, tr := range forgeRepoTargetedTransitions {
		if key == tr {
			return true
		}
	}
	return false
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

	// A subscription made up ONLY of repository-targeted events (a pipeline)
	// never carries an item, so the item variable is not shown — listing a
	// {{ITEM_TYPE}} the task can never receive would be misleading.
	repoTargetedOnly := len(subscribed) > 0
	for t := range subscribed {
		if !isRepoTargetedTransition(t) {
			repoTargetedOnly = false
			break
		}
	}

	var b strings.Builder
	b.WriteString("## Forge 事件\n")
	for _, v := range eventContextVars {
		if !showAll && v.eventScoped != "" && !subscribed[v.eventScoped] {
			continue
		}
		if repoTargetedOnly && v.requiresItem {
			continue
		}
		fmt.Fprintf(&b, "- %s：{{%s}}\n", v.label, v.placeholder)
	}
	return strings.TrimRight(b.String(), "\n")
}
