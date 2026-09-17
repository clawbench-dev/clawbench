package service

import (
	"fmt"
	"strings"
	"time"

	"clawbench/internal/forge"
)

// eventBodyMaxRunes bounds a free-text field (an item description or a comment
// body) before it enters the prompt.
//
// Both platforms allow a description of tens of thousands of characters, and an
// event task is fired automatically — so without a cap a single verbose issue
// would consume the task's whole context window and the actual instruction
// would be pushed out. The limit is deliberately generous: it exists to bound
// the pathological case, not to shorten real content.
const eventBodyMaxRunes = 4000

// EventContext carries the forge event payload that an event-triggered task
// renders into its prompt. Fields that do not apply to a given event stay empty
// and their line is omitted (see RenderEventContext), so the prompt never
// contains a dangling label like "流水线状态：" with no value.
type EventContext struct {
	EventType string
	Repo      string // owner/repo
	// ItemNumber is 0 for a repository-targeted event (a pipeline run), which
	// has no item identity.
	ItemNumber int
	ItemType   string // issue | pr | pipeline
	Title      string
	URL        string
	Author     string
	State      string
	// PrevState is the state the item was in before a transition. It is what
	// distinguishes a reopen (closed→open) from an original open, which the
	// final State alone cannot express.
	PrevState string
	// Body is the item's raw Markdown description.
	Body string
	// Labels / Assignees are the item's labels and assignees, already joined for
	// display.
	Labels    string
	Assignees string
	// Draft marks a draft change request.
	Draft bool
	// SourceBranch is a change request's head branch (empty for issues).
	SourceBranch string
	// MergedAt is set for a merged change request.
	MergedAt *time.Time
	// CreatedAt / UpdatedAt are the item timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
	// CommentCount is the item's comment count as reported by the platform.
	CommentCount int
	// CommentBody / CommentID describe the newest comment (comment events).
	CommentBody string
	CommentID   int64
	// Pipeline carries the finished CI run (pipeline_done). Nil otherwise.
	Pipeline *forge.PipelineRun
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
		EventType:    string(change.Type),
		Repo:         repo.Owner + "/" + repo.Repo,
		ItemNumber:   item.Number,
		ItemType:     string(item.Type),
		Title:        item.Title,
		URL:          item.URL,
		Author:       author,
		State:        string(item.State),
		PrevState:    change.PrevState,
		Body:         item.Body,
		Labels:       joinLabelNames(item.Labels),
		Assignees:    joinAuthorLogins(item.Assignees),
		Draft:        item.Draft,
		SourceBranch: item.SourceBranch,
		MergedAt:     item.MergedAt,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
		CommentCount: item.CommentCount,
		CommentBody:  change.CommentBody,
		CommentID:    change.CommentID,
		Pipeline:     change.Pipeline,
	}
}

// joinLabelNames renders labels as a comma-separated list. Empty input yields an
// empty string so the caller's line is omitted rather than rendering a blank.
func joinLabelNames(labels []forge.Label) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		if l.Name != "" {
			names = append(names, l.Name)
		}
	}
	return strings.Join(names, ", ")
}

// joinAuthorLogins renders assignees as a comma-separated list of logins.
func joinAuthorLogins(users []forge.Author) string {
	if len(users) == 0 {
		return ""
	}
	logins := make([]string, 0, len(users))
	for _, u := range users {
		if u.Login != "" {
			logins = append(logins, u.Login)
		}
	}
	return strings.Join(logins, ", ")
}

// formatTime renders a timestamp for the prompt. A zero time renders empty so
// its line is omitted rather than showing "0001-01-01".
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatOptionalTime renders a possibly-absent timestamp.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

// formatDuration renders a run duration. Zero means "not reported" on both
// platforms (GitHub's job list and GitLab's run list omit it), so it is omitted
// rather than rendered as "0s", which would read as an instant run.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.Round(time.Second).String()
}

// formatLinkedPullRequests renders the change requests a CI run is attached to.
// Empty is a normal outcome — a push-to-main run has no PR — so it renders
// nothing rather than a misleading "none".
func formatLinkedPullRequests(prs []forge.PipelinePullRequest) string {
	if len(prs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(prs))
	for _, pr := range prs {
		if pr.Number <= 0 {
			continue
		}
		if pr.Title != "" {
			parts = append(parts, fmt.Sprintf("#%d %s", pr.Number, pr.Title))
			continue
		}
		parts = append(parts, fmt.Sprintf("#%d", pr.Number))
	}
	return strings.Join(parts, ", ")
}

// eventContextVar describes one variable in the fixed event-context block. The
// placeholder is the template token; the value extracts it from a context.
type eventContextVar struct {
	label       string
	placeholder string
	value       func(EventContext) string
	// eventScoped, when non-empty, restricts the variable to events of those
	// types (used by the read-only UI template). Empty means "applies to all
	// events".
	eventScoped []string
	// requiresItem marks a variable that only makes sense for an event attached
	// to an issue or PR. A pipeline run has no item, so the variable renders
	// nothing for it (otherwise the prompt would read "pipeline #0") and is
	// hidden from the template when the subscription is exclusively
	// repository-targeted events.
	requiresItem bool
}

// appliesTo reports whether the variable is in scope for an event of the given
// type. An unscoped variable applies to every event.
func (v eventContextVar) appliesTo(eventType string) bool {
	if len(v.eventScoped) == 0 {
		return true
	}
	for _, t := range v.eventScoped {
		if t == eventType {
			return true
		}
	}
	return false
}

// stateTransitionEvents are the events that report a lifecycle transition, i.e.
// the ones for which a previous state exists and is meaningful.
//
// `commented` is deliberately excluded: its PrevState is the item's unchanged
// state, so rendering it would produce the noise "状态：open / 前一状态：open".
// Pipeline events are excluded because a run has no lifecycle state to precede.
var stateTransitionEvents = []string{
	string(forge.EventOpened),
	string(forge.EventClosed),
	string(forge.EventMerged),
	string(forge.EventReopened),
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
		// Only a genuine transition has a previous state; see
		// stateTransitionEvents for why `commented` is excluded.
		label:       "前一状态",
		placeholder: "PREV_STATE",
		value:       func(e EventContext) string { return e.PrevState },
		eventScoped: stateTransitionEvents,
	},
	{
		label:        "正文",
		placeholder:  "BODY",
		value:        func(e EventContext) string { return truncateRunes(e.Body, eventBodyMaxRunes) },
		requiresItem: true,
	},
	{
		label:        "标签",
		placeholder:  "LABELS",
		value:        func(e EventContext) string { return e.Labels },
		requiresItem: true,
	},
	{
		label:        "指派人",
		placeholder:  "ASSIGNEES",
		value:        func(e EventContext) string { return e.Assignees },
		requiresItem: true,
	},
	{
		label:       "草稿",
		placeholder: "DRAFT",
		value: func(e EventContext) string {
			if e.Draft {
				return "是"
			}
			return ""
		},
		requiresItem: true,
	},
	{
		label:        "源分支",
		placeholder:  "SOURCE_BRANCH",
		value:        func(e EventContext) string { return e.SourceBranch },
		requiresItem: true,
	},
	{
		label:        "合并时间",
		placeholder:  "MERGED_AT",
		value:        func(e EventContext) string { return formatOptionalTime(e.MergedAt) },
		requiresItem: true,
	},
	{label: "创建时间", placeholder: "CREATED_AT", value: func(e EventContext) string { return formatTime(e.CreatedAt) }},
	{label: "更新时间", placeholder: "UPDATED_AT", value: func(e EventContext) string { return formatTime(e.UpdatedAt) }},
	{
		label:        "评论数",
		placeholder:  "COMMENT_COUNT",
		value:        func(e EventContext) string { return formatCommentCount(e.CommentCount) },
		requiresItem: true,
	},
	{
		label: "评论内容", placeholder: "COMMENT_BODY",
		value:       func(e EventContext) string { return truncateRunes(e.CommentBody, eventBodyMaxRunes) },
		eventScoped: []string{string(forge.EventCommented)},
	},
	{
		label: "评论 ID", placeholder: "COMMENT_ID",
		value:       func(e EventContext) string { return formatCommentID(e.CommentID) },
		eventScoped: []string{string(forge.EventCommented)},
	},
	{
		label: "流水线状态", placeholder: "PIPELINE_STATUS",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return string(p.Status) })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		label: "流水线链接", placeholder: "PIPELINE_URL",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return p.URL })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		// The run's own identity, which the synthetic item cannot express (it
		// carries Number 0 for every run).
		label: "流水线编号", placeholder: "PIPELINE_NUMBER",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string {
				if p.Number <= 0 {
					return ""
				}
				return fmt.Sprintf("%d", p.Number)
			})
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		// The branch or tag the run executed on. This is the field a repair task
		// needs in order to check out the failing revision.
		label:       "流水线分支",
		placeholder: "PIPELINE_REF",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return p.Ref })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		// The commit the run executed against — the other half of "which
		// revision failed".
		label:       "流水线提交",
		placeholder: "PIPELINE_SHA",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return p.SHA })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		// What started the run, in the platform's own vocabulary ("push",
		// "pull_request", "schedule", "web", ...). Passed through rather than
		// normalized: it is informational and the two vocabularies differ.
		label:       "流水线触发方式",
		placeholder: "PIPELINE_TRIGGER",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return p.Event })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		label:       "流水线耗时",
		placeholder: "PIPELINE_DURATION",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return formatDuration(p.Duration) })
		},
		eventScoped: []string{string(forge.EventPipeline)},
	},
	{
		label:       "关联合并请求",
		placeholder: "PIPELINE_LINKED_PRS",
		value: func(e EventContext) string {
			return pipelineField(e, func(p *forge.PipelineRun) string { return formatLinkedPullRequests(p.PullRequests) })
		},
		eventScoped: []string{string(forge.EventPipeline)},
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
		eventScoped: []string{string(forge.EventPipeline)},
	},
}

// pipelineField reads a field off the event's CI run, tolerating a nil run.
//
// The pointer is nil for every non-pipeline event, and a Change reconstructed
// without its detail (a persisted row, a test) also has none — neither is an
// error, so both render nothing rather than panicking.
func pipelineField(e EventContext, f func(*forge.PipelineRun) string) string {
	if e.Pipeline == nil {
		return ""
	}
	return f(e.Pipeline)
}

// formatCommentCount renders a comment count. Zero is omitted: "0 comments" and
// "the platform did not report a count" are indistinguishable here, and the
// event is usually about a comment that has just been added.
func formatCommentCount(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

// formatCommentID renders a comment id. Zero means "no comment" (a non-comment
// event), so it is omitted.
func formatCommentID(id int64) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", id)
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
		if !v.appliesTo(ec.EventType) {
			continue
		}
		value := v.value(ec)
		if value == "" {
			continue
		}
		// Collapse multi-line values (descriptions, comment bodies) so one
		// variable stays one bullet in the block.
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

// transitionOf strips the "<kind>." prefix from a kind-scoped subscription key
// ("pr.commented" → "commented"). A bare key is returned unchanged.
//
// Subscriptions are stored kind-scoped, but an eventScoped variable names a bare
// transition, so comparing the two directly would never match and every scoped
// variable would be hidden from the template.
func transitionOf(key string) string {
	if _, rest, ok := strings.Cut(key, "."); ok {
		return rest
	}
	return key
}

// EventPromptTemplate returns the read-only placeholder block shown in the task
// form. Only variables relevant to the subscribed event types are listed, so
// the user sees exactly what will be injected. An empty subscription lists all
// variables.
func EventPromptTemplate(eventTypes []string) string {
	subscribed := make(map[string]bool, len(eventTypes))
	for _, t := range eventTypes {
		subscribed[transitionOf(t)] = true
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
		if !showAll && !appliesToSubscription(v, subscribed) {
			continue
		}
		if repoTargetedOnly && v.requiresItem {
			continue
		}
		fmt.Fprintf(&b, "- %s：{{%s}}\n", v.label, v.placeholder)
	}
	return strings.TrimRight(b.String(), "\n")
}

// appliesToSubscription reports whether a variable is in scope for a
// subscription set expressed as bare transitions. It is the subscription-side
// twin of eventContextVar.appliesTo, so the template and the rendered prompt
// cannot disagree about which variables a given subscription yields.
func appliesToSubscription(v eventContextVar, subscribed map[string]bool) bool {
	if len(v.eventScoped) == 0 {
		return true
	}
	for _, t := range v.eventScoped {
		if subscribed[t] {
			return true
		}
	}
	return false
}
