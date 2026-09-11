package service

import (
	"strings"
	"testing"

	"clawbench/internal/forge"
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
		ItemNumber:     7,
		ItemType:       "pr",
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
