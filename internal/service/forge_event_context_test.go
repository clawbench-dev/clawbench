package service

import (
	"strings"
	"testing"

	"clawbench/internal/forge"

	"github.com/stretchr/testify/assert"
)

func TestRenderEventContext_OmitsInapplicableVariables(t *testing.T) {
	// A comment event must not render pipeline lines (which would be dangling
	// labels with empty values).
	ec := EventContext{
		EventType:   string(forge.EventCommented),
		Repo:        "acme/widgets",
		ItemNumber:  42,
		ItemType:    "pr",
		Title:       "Fix the thing",
		URL:         "https://github.com/acme/widgets/pull/42",
		Author:      "alice",
		State:       "open",
		CommentBody: "please rebase",
	}
	out := RenderEventContext(ec)

	if strings.Contains(out, "流水线状态") || strings.Contains(out, "PIPELINE") {
		t.Fatalf("comment event must not render pipeline lines, got:\n%s", out)
	}
	if !strings.Contains(out, "评论内容：please rebase") {
		t.Fatalf("comment body missing, got:\n%s", out)
	}
	if !strings.Contains(out, "仓库：acme/widgets") {
		t.Fatalf("repo missing, got:\n%s", out)
	}
	if !strings.Contains(out, "条目：pr #42") {
		t.Fatalf("item line wrong, got:\n%s", out)
	}
}

func TestRenderEventContext_PipelineEvent(t *testing.T) {
	ec := EventContext{
		EventType:      string(forge.EventPipeline),
		Repo:           "acme/widgets",
		ItemType:       string(forge.ItemTypePipeline),
		ItemNumber:     0,
		Title:          "CI",
		PipelineStatus: "success",
		PipelineURL:    "https://ci.example/run/7",
	}
	out := RenderEventContext(ec)

	if !strings.Contains(out, "流水线状态：success") {
		t.Fatalf("pipeline status missing, got:\n%s", out)
	}
	if !strings.Contains(out, "流水线链接：https://ci.example/run/7") {
		t.Fatalf("pipeline url missing, got:\n%s", out)
	}
	// No comment on this event, so the comment line must be absent.
	if strings.Contains(out, "评论内容") {
		t.Fatalf("pipeline event must not render comment line, got:\n%s", out)
	}
	// A run has no item, so the item line must be absent rather than rendering
	// the nonsensical "pipeline #0".
	if strings.Contains(out, "条目") {
		t.Fatalf("pipeline event must not render an item line, got:\n%s", out)
	}
	if strings.Contains(out, "#0") {
		t.Fatalf("pipeline event must not render a zero item number, got:\n%s", out)
	}
}

// TestRenderEventContext_PipelineActorIsSelf guards the prompt's only means of
// telling whether it triggered itself: the code deliberately does not suppress
// self-triggered pipelines, so the flag must reach the prompt.
func TestRenderEventContext_PipelineActorIsSelf(t *testing.T) {
	base := EventContext{
		EventType: string(forge.EventPipeline),
		Repo:      "acme/widgets",
		ItemType:  string(forge.ItemTypePipeline),
	}

	self := base
	self.ActorIsSelf = true
	out := RenderEventContext(self)
	if !strings.Contains(out, "是否自身触发：是") {
		t.Fatalf("self-triggered pipeline must be labeled, got:\n%s", out)
	}

	other := base
	other.ActorIsSelf = false
	out = RenderEventContext(other)
	if !strings.Contains(out, "是否自身触发：否") {
		t.Fatalf("external pipeline must be labeled, got:\n%s", out)
	}

	// The flag is pipeline-scoped: an issue/PR event decides this in code, so
	// the variable must not appear for it.
	issueEC := EventContext{
		EventType: string(forge.EventOpened), Repo: "a/b",
		ItemType: "issue", ItemNumber: 1,
	}
	if strings.Contains(RenderEventContext(issueEC), "是否自身触发") {
		t.Fatalf("issue event must not render the pipeline self flag, got:\n%s", RenderEventContext(issueEC))
	}
}

// TestEventPromptTemplate_PipelineHidesItemVariable: a pipeline-only
// subscription can never receive an item, so promising {{ITEM_TYPE}} would
// mislead the user writing the prompt.
func TestEventPromptTemplate_PipelineHidesItemVariable(t *testing.T) {
	out := EventPromptTemplate([]string{string(forge.EventPipeline)})
	if strings.Contains(out, "ITEM_TYPE") {
		t.Fatalf("pipeline-only subscription must not advertise an item variable, got:\n%s", out)
	}
	if !strings.Contains(out, "{{PIPELINE_STATUS}}") {
		t.Fatalf("pipeline vars missing, got:\n%s", out)
	}
	if !strings.Contains(out, "{{ACTOR_IS_SELF}}") {
		t.Fatalf("the self-authorship variable must be offered, got:\n%s", out)
	}

	// A mixed subscription still involves items, so the variable stays.
	mixed := EventPromptTemplate([]string{string(forge.EventPipeline), "pr.opened"})
	if !strings.Contains(mixed, "ITEM_TYPE") {
		t.Fatalf("a mixed subscription must keep the item variable, got:\n%s", mixed)
	}
}

func TestRenderEventContext_MultilineCommentIndented(t *testing.T) {
	ec := EventContext{
		EventType:   string(forge.EventCommented),
		Repo:        "a/b",
		ItemType:    "issue",
		ItemNumber:  1,
		CommentBody: "line one\nline two",
	}
	out := RenderEventContext(ec)
	if !strings.Contains(out, "评论内容：line one\n  line two") {
		t.Fatalf("multiline comment not indented into one bullet, got:\n%s", out)
	}
}

func TestEventPromptTemplate_ScopedToSubscribedTypes(t *testing.T) {
	// Subscribing only to comments hides pipeline variables from the read-only
	// template shown in the task form.
	out := EventPromptTemplate([]string{string(forge.EventCommented)})
	if strings.Contains(out, "PIPELINE") {
		t.Fatalf("comment-only subscription must not list pipeline vars, got:\n%s", out)
	}
	if !strings.Contains(out, "{{COMMENT_BODY}}") {
		t.Fatalf("comment var missing, got:\n%s", out)
	}

	// Pipeline subscription lists pipeline vars but not the comment body.
	out = EventPromptTemplate([]string{string(forge.EventPipeline)})
	if !strings.Contains(out, "{{PIPELINE_STATUS}}") {
		t.Fatalf("pipeline var missing, got:\n%s", out)
	}
	if strings.Contains(out, "COMMENT_BODY") {
		t.Fatalf("pipeline-only subscription must not list comment var, got:\n%s", out)
	}

	// No subscription (empty) lists everything.
	out = EventPromptTemplate(nil)
	if !strings.Contains(out, "{{COMMENT_BODY}}") || !strings.Contains(out, "{{PIPELINE_STATUS}}") {
		t.Fatalf("empty subscription should list all vars, got:\n%s", out)
	}
}

func TestEventContextFromChange_MapsFields(t *testing.T) {
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
	item := forge.Item{
		Type:   forge.ItemTypeChangeRequest,
		Number: 42,
		Title:  "Fix the thing",
		URL:    "https://github.com/acme/widgets/pull/42",
		State:  forge.StateOpen,
		Author: forge.Author{Login: "alice"},
	}
	change := forge.Change{Type: forge.EventOpened, Number: 42}

	ec := EventContextFromChange(repo, item, change)
	if ec.Repo != "acme/widgets" {
		t.Fatalf("repo = %q, want acme/widgets", ec.Repo)
	}
	if ec.ItemNumber != 42 || ec.ItemType != "pr" {
		t.Fatalf("item = %s #%d, want pr #42", ec.ItemType, ec.ItemNumber)
	}
	if ec.Author != "alice" {
		t.Fatalf("author = %q, want alice", ec.Author)
	}
}

// TestEventContextFromChange_PopulatesCommentBody guards that a commented event
// carries the comment text into the prompt. Without it the AI runs blind on the
// one thing the event is about.
func TestEventContextFromChange_PopulatesCommentBody(t *testing.T) {
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
	item := forge.Item{Type: forge.ItemTypeIssue, Number: 7, Title: "Bug", Author: forge.Author{Login: "alice"}}
	change := forge.Change{
		Type: forge.EventCommented, Number: 7,
		Actor: "bob", CommentBody: "please fix the nil deref",
	}

	ec := EventContextFromChange(repo, item, change)
	assert.Equal(t, "please fix the nil deref", ec.CommentBody)
	// The author is the commenter, not the issue creator.
	assert.Equal(t, "bob", ec.Author)

	rendered := RenderEventContext(ec)
	assert.Contains(t, rendered, "please fix the nil deref")
	assert.Contains(t, rendered, "bob")
	// A comment event must not render pipeline lines.
	assert.NotContains(t, rendered, "流水线")
}
