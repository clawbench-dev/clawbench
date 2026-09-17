package service

import (
	"strings"
	"testing"
	"time"

	"clawbench/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullItem returns an item populated with every field the zero-cost union
// exposes, so a test can assert the whole rendered block at once.
func fullItem() forge.Item {
	merged := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	return forge.Item{
		Type:         forge.ItemTypeChangeRequest,
		Number:       42,
		Title:        "Fix the nil deref",
		Body:         "The handler panics when the body is empty.",
		State:        forge.StateMerged,
		Draft:        true,
		Author:       forge.Author{Login: "alice"},
		Assignees:    []forge.Author{{Login: "bob"}, {Login: "carol"}},
		Labels:       []forge.Label{{Name: "bug"}, {Name: "urgent"}},
		CommentCount: 3,
		URL:          "https://github.com/acme/widgets/pull/42",
		CreatedAt:    time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC),
		MergedAt:     &merged,
		SourceBranch: "fix/nil-deref",
	}
}

// TestRenderEventContext_ZeroCostUnionRendersEveryField pins the whole block
// for a state transition: the point of the extension is that a task now sees
// the description, labels, assignees and branch it previously could not.
func TestRenderEventContext_ZeroCostUnionRendersEveryField(t *testing.T) {
	ec := EventContextFromChange(
		ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"},
		fullItem(),
		forge.Change{Type: forge.EventMerged, Number: 42, PrevState: string(forge.StateOpen)},
	)
	out := RenderEventContext(ec)

	for _, want := range []string{
		"事件类型：merged",
		"仓库：acme/widgets",
		"条目：pr #42",
		"标题：Fix the nil deref",
		"链接：https://github.com/acme/widgets/pull/42",
		"作者：alice",
		"状态：merged",
		"前一状态：open",
		"正文：The handler panics when the body is empty.",
		"标签：bug, urgent",
		"指派人：bob, carol",
		"草稿：是",
		"源分支：fix/nil-deref",
		"合并时间：2026-09-10T08:30:00Z",
		"创建时间：2026-09-01T10:00:00Z",
		"更新时间：2026-09-10T09:00:00Z",
		"评论数：3",
	} {
		assert.Contains(t, out, want)
	}
	// A merged PR event has no comment or pipeline payload.
	assert.NotContains(t, out, "评论内容")
	assert.NotContains(t, out, "评论 ID")
	assert.NotContains(t, out, "流水线")
}

// TestRenderEventContext_PrevStateExcludedFromCommented is the guard for the
// scoping change: `commented` carries a PrevState equal to the current state,
// so rendering it would emit the noise "状态：open / 前一状态：open".
func TestRenderEventContext_PrevStateExcludedFromCommented(t *testing.T) {
	ec := EventContext{
		EventType:   string(forge.EventCommented),
		Repo:        "a/b",
		ItemType:    "issue",
		ItemNumber:  1,
		State:       "open",
		PrevState:   "open",
		CommentBody: "please rebase",
		CommentID:   987,
	}
	out := RenderEventContext(ec)

	assert.NotContains(t, out, "前一状态")
	assert.Contains(t, out, "状态：open")
	assert.Contains(t, out, "评论内容：please rebase")
	assert.Contains(t, out, "评论 ID：987")
}

// TestRenderEventContext_PipelineDetail covers the pipeline fields the synthetic
// item cannot express: a run carries Number 0, so the ref/SHA are the only way
// a repair task can identify the failing revision.
func TestRenderEventContext_PipelineDetail(t *testing.T) {
	ec := EventContext{
		EventType: string(forge.EventPipeline),
		Repo:      "acme/widgets",
		ItemType:  string(forge.ItemTypePipeline),
		Title:     "CI",
		Pipeline: &forge.PipelineRun{
			ID:       555,
			Number:   17,
			Name:     "CI",
			Status:   forge.PipelineFailure,
			Ref:      "main",
			SHA:      "abc123def456",
			Event:    "push",
			Actor:    "octocat",
			URL:      "https://ci.example/run/555",
			Duration: 3*time.Minute + 12*time.Second,
			PullRequests: []forge.PipelinePullRequest{
				{Number: 42, Title: "Fix the nil deref"},
			},
		},
	}
	out := RenderEventContext(ec)

	for _, want := range []string{
		"流水线状态：failure",
		"流水线链接：https://ci.example/run/555",
		"流水线编号：17",
		"流水线分支：main",
		"流水线提交：abc123def456",
		"流水线触发方式：push",
		"流水线耗时：3m12s",
		"关联合并请求：#42 Fix the nil deref",
	} {
		assert.Contains(t, out, want)
	}
	// A run has no item, so none of the item-scoped variables may appear.
	assert.NotContains(t, out, "条目")
	assert.NotContains(t, out, "正文")
	assert.NotContains(t, out, "源分支")
	assert.NotContains(t, out, "#0")
}

// TestRenderEventContext_PipelineNilDoesNotPanic: a Change reconstructed without
// its run detail (a persisted row, a test fixture) must render nothing rather
// than crash the task.
func TestRenderEventContext_PipelineNilDoesNotPanic(t *testing.T) {
	ec := EventContext{
		EventType: string(forge.EventPipeline),
		Repo:      "acme/widgets",
		ItemType:  string(forge.ItemTypePipeline),
	}
	require.NotPanics(t, func() { RenderEventContext(ec) })
	out := RenderEventContext(ec)
	assert.NotContains(t, out, "流水线")
	// The self-trigger flag is not derived from the run, so it still renders.
	assert.Contains(t, out, "是否自身触发：否")
}

// TestRenderEventContext_BodyTruncated: an item description can be tens of
// thousands of characters and the task is fired automatically, so an uncapped
// body would push the user's actual instruction out of the context window.
func TestRenderEventContext_BodyTruncated(t *testing.T) {
	long := strings.Repeat("x", eventBodyMaxRunes+500)
	ec := EventContext{
		EventType:  string(forge.EventOpened),
		Repo:       "a/b",
		ItemType:   "issue",
		ItemNumber: 1,
		Body:       long,
	}
	out := RenderEventContext(ec)

	assert.Contains(t, out, "...(truncated)")
	assert.NotContains(t, out, long)
	// The full body must not have survived anywhere in the block.
	assert.Less(t, len(out), len(long))
}

// TestRenderEventContext_CommentBodyTruncated: the same cap applies to a
// comment, which is equally attacker-controlled free text.
func TestRenderEventContext_CommentBodyTruncated(t *testing.T) {
	long := strings.Repeat("y", eventBodyMaxRunes+500)
	ec := EventContext{
		EventType:   string(forge.EventCommented),
		Repo:        "a/b",
		ItemType:    "issue",
		ItemNumber:  1,
		CommentBody: long,
	}
	out := RenderEventContext(ec)
	assert.Contains(t, out, "...(truncated)")
	assert.NotContains(t, out, long)
}

// TestRenderEventContext_DraftOnlyWhenTrue: "草稿：否" on every non-draft item
// would be noise, so the line is present only when the item IS a draft.
func TestRenderEventContext_DraftOnlyWhenTrue(t *testing.T) {
	base := EventContext{
		EventType: string(forge.EventOpened), Repo: "a/b",
		ItemType: "pr", ItemNumber: 1, Title: "t",
	}
	assert.NotContains(t, RenderEventContext(base), "草稿")

	draft := base
	draft.Draft = true
	assert.Contains(t, RenderEventContext(draft), "草稿：是")
}

// TestRenderEventContext_ZeroValuesOmitted guards the invariant that keeps the
// block coherent: a variable with no value is omitted, never rendered blank.
func TestRenderEventContext_ZeroValuesOmitted(t *testing.T) {
	ec := EventContext{
		EventType:  string(forge.EventOpened),
		Repo:       "a/b",
		ItemType:   "issue",
		ItemNumber: 1,
		Title:      "t",
	}
	out := RenderEventContext(ec)

	for _, label := range []string{
		"前一状态", "正文", "标签", "指派人", "草稿", "源分支", "合并时间",
		"创建时间", "更新时间", "评论数", "评论内容", "评论 ID", "流水线",
	} {
		assert.NotContains(t, out, label)
	}
	// A zero timestamp must never render as the Go zero time.
	assert.NotContains(t, out, "0001-01-01")
}

// TestRenderEventContext_DurationZeroOmitted: GitLab's run list reports no
// duration, and "耗时：0s" would read as an instant run rather than "unknown".
func TestRenderEventContext_DurationZeroOmitted(t *testing.T) {
	ec := EventContext{
		EventType: string(forge.EventPipeline),
		Repo:      "a/b",
		ItemType:  string(forge.ItemTypePipeline),
		Pipeline:  &forge.PipelineRun{ID: 1, Status: forge.PipelineSuccess},
	}
	assert.NotContains(t, RenderEventContext(ec), "流水线耗时")
}

// TestEventPromptTemplate_OffersNewVariables checks the template advertises the
// new variables for the subscriptions that can actually receive them, and hides
// them otherwise. The template is the user's only documentation of the payload.
func TestEventPromptTemplate_OffersNewVariables(t *testing.T) {
	out := EventPromptTemplate([]string{"pr.opened"})
	for _, want := range []string{
		"{{BODY}}", "{{LABELS}}", "{{ASSIGNEES}}", "{{SOURCE_BRANCH}}", "{{PREV_STATE}}",
		"{{DRAFT}}", "{{CREATED_AT}}", "{{UPDATED_AT}}", "{{COMMENT_COUNT}}",
	} {
		assert.Contains(t, out, want, "pr.opened subscription should offer %s", want)
	}
	assert.NotContains(t, out, "{{PIPELINE_")
	assert.NotContains(t, out, "{{COMMENT_BODY}}")
}

// TestEventPromptTemplate_KindScopedKeysMatchScopedVariables is the regression
// for a latent bug: subscriptions are stored kind-scoped ("pr.commented") but
// scoped variables name a bare transition, so a direct comparison never matched
// and the comment/pipeline variables were hidden from the form for every
// kind-scoped subscription.
func TestEventPromptTemplate_KindScopedKeysMatchScopedVariables(t *testing.T) {
	commented := EventPromptTemplate([]string{"pr.commented"})
	assert.Contains(t, commented, "{{COMMENT_BODY}}")
	assert.Contains(t, commented, "{{COMMENT_ID}}")
	assert.NotContains(t, commented, "{{PIPELINE_STATUS}}")

	// The legacy bare key must behave identically.
	bare := EventPromptTemplate([]string{"commented"})
	assert.Contains(t, bare, "{{COMMENT_BODY}}")

	pipeline := EventPromptTemplate([]string{"pipeline_done"})
	assert.Contains(t, pipeline, "{{PIPELINE_REF}}")
	assert.Contains(t, pipeline, "{{PIPELINE_SHA}}")
	assert.NotContains(t, pipeline, "{{COMMENT_BODY}}")
}

// TestEventPromptTemplate_PipelineOmitsItemScopedVariables: a pipeline run has
// no item, so every item-scoped variable must be hidden from a pipeline-only
// subscription rather than promised and never delivered.
func TestEventPromptTemplate_PipelineOmitsItemScopedVariables(t *testing.T) {
	out := EventPromptTemplate([]string{"pipeline_done"})
	for _, hidden := range []string{"{{ITEM_TYPE}}", "{{BODY}}", "{{LABELS}}", "{{SOURCE_BRANCH}}", "{{MERGED_AT}}"} {
		assert.NotContains(t, out, hidden)
	}
	// Repository-level fields still apply to a run.
	assert.Contains(t, out, "{{PIPELINE_SHA}}")
}

// TestEventContextFromChange_JoinsLabelsAndAssignees guards the display joining:
// a raw Go slice rendered with %v would produce "[bug urgent]", which reads as
// one label named "bug urgent".
func TestEventContextFromChange_JoinsLabelsAndAssignees(t *testing.T) {
	ec := EventContextFromChange(
		ForgeRepoRef{Owner: "acme", Repo: "widgets"},
		forge.Item{
			Type:   forge.ItemTypeIssue,
			Number: 1,
			Labels: []forge.Label{{Name: "bug"}, {Name: "p1"}},
			Assignees: []forge.Author{
				{Login: "bob"}, {Login: "carol"},
			},
		},
		forge.Change{Type: forge.EventOpened},
	)
	assert.Equal(t, "bug, p1", ec.Labels)
	assert.Equal(t, "bob, carol", ec.Assignees)
}

// TestEventContextFromChange_EmptyLabelsStayEmpty keeps a label-less item from
// rendering an empty label line.
func TestEventContextFromChange_EmptyLabelsStayEmpty(t *testing.T) {
	ec := EventContextFromChange(
		ForgeRepoRef{Owner: "acme", Repo: "widgets"},
		forge.Item{Type: forge.ItemTypeIssue, Number: 1},
		forge.Change{Type: forge.EventOpened},
	)
	assert.Empty(t, ec.Labels)
	assert.Empty(t, ec.Assignees)
}

// TestEventContextFromChange_CarriesPipelineDetail verifies the run reaches the
// context through the change, which is what makes the pipeline fields renderable
// at all.
func TestEventContextFromChange_CarriesPipelineDetail(t *testing.T) {
	run := &forge.PipelineRun{ID: 9, Status: forge.PipelineFailure, Ref: "main", SHA: "deadbeef"}
	ec := EventContextFromChange(
		ForgeRepoRef{Owner: "acme", Repo: "widgets"},
		forge.Item{Type: forge.ItemTypePipeline, Number: 0},
		forge.Change{Type: forge.EventPipeline, PipelineRunID: 9, Pipeline: run},
	)
	require.NotNil(t, ec.Pipeline)
	assert.Equal(t, "deadbeef", ec.Pipeline.SHA)

	out := RenderEventContext(ec)
	assert.Contains(t, out, "流水线提交：deadbeef")
	assert.Contains(t, out, "流水线分支：main")
}

// TestEventContextFromChange_CommentCarriesIDAndCount covers the comment event's
// new identity field.
func TestEventContextFromChange_CommentCarriesIDAndCount(t *testing.T) {
	ec := EventContextFromChange(
		ForgeRepoRef{Owner: "acme", Repo: "widgets"},
		forge.Item{Type: forge.ItemTypeIssue, Number: 7, CommentCount: 4},
		forge.Change{Type: forge.EventCommented, CommentID: 321, CommentBody: "please fix"},
	)
	assert.Equal(t, int64(321), ec.CommentID)
	assert.Equal(t, 4, ec.CommentCount)

	out := RenderEventContext(ec)
	assert.Contains(t, out, "评论 ID：321")
	assert.Contains(t, out, "评论数：4")
}

// TestTransitionOf covers the key normalization the template's scoping depends
// on: a kind-scoped key must reduce to its bare transition, and a bare key must
// pass through unchanged.
func TestTransitionOf(t *testing.T) {
	assert.Equal(t, "commented", transitionOf("pr.commented"))
	assert.Equal(t, "opened", transitionOf("issue.opened"))
	assert.Equal(t, "commented", transitionOf("commented"))
	assert.Equal(t, "pipeline_done", transitionOf("pipeline_done"))
}
