package service

import (
	"strings"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateScriptInput_CronOnlyCommonVars(t *testing.T) {
	task := &model.ScheduledTask{ID: 5, Name: "Nightly", ProjectPath: "/repo"}
	in := gateScriptInput(task, nil, nil)

	assert.Equal(t, "cron", in.Env["CLAWBENCH_TRIGGER"])
	assert.Equal(t, "5", in.Env["CLAWBENCH_TASK_ID"])
	assert.Equal(t, "Nightly", in.Env["CLAWBENCH_TASK_NAME"])
	assert.Equal(t, "/repo", in.Env["CLAWBENCH_PROJECT_PATH"])
	assert.Empty(t, in.Stdin)

	// No mail/event variable may leak into a cron run.
	for name := range in.Env {
		assert.False(t, strings.HasPrefix(name, "CLAWBENCH_MAIL_"), "unexpected mail var %s", name)
		assert.NotEqual(t, "CLAWBENCH_EVENT_TYPE", name)
	}
}

func TestGateScriptInput_MailVarsAndStdin(t *testing.T) {
	task := &model.ScheduledTask{ID: 9, Name: "Mail", TriggerMode: "email"}
	mc := &MailEventContext{
		AccountName: "工作邮箱", Folder: "收件箱", From: "boss@corp.com", To: "me@corp.com",
		Subject: "周报", Date: "2026-11-14 09:00", Body: "正文内容", MessageID: 42,
	}
	in := gateScriptInput(task, mc, nil)

	assert.Equal(t, "email", in.Env["CLAWBENCH_TRIGGER"])
	assert.Equal(t, "工作邮箱", in.Env["CLAWBENCH_MAIL_ACCOUNT"])
	assert.Equal(t, "收件箱", in.Env["CLAWBENCH_MAIL_FOLDER"])
	assert.Equal(t, "boss@corp.com", in.Env["CLAWBENCH_MAIL_FROM"])
	assert.Equal(t, "周报", in.Env["CLAWBENCH_MAIL_SUBJECT"])
	assert.Equal(t, "42", in.Env["CLAWBENCH_MAIL_MESSAGE_ID"])
	assert.Equal(t, "正文内容", in.Stdin, "the body must arrive on stdin")

	// Booleans are always present so a script can test them without a default.
	assert.Equal(t, "0", in.Env["CLAWBENCH_MAIL_HAS_ATTACHMENT"])

	// Empty fields are omitted rather than exported as empty strings.
	sparse := gateScriptInput(task, &MailEventContext{Subject: "只有主题"}, nil)
	_, hasFrom := sparse.Env["CLAWBENCH_MAIL_FROM"]
	assert.False(t, hasFrom)
	_, hasMessageID := sparse.Env["CLAWBENCH_MAIL_MESSAGE_ID"]
	assert.False(t, hasMessageID)
}

func TestGateScriptInput_EventStdinPrefersComment(t *testing.T) {
	task := &model.ScheduledTask{ID: 1, Name: "Event", TriggerMode: "event"}

	comment := &EventContext{
		EventType: "commented", Repo: "acme/widgets", ItemType: "pr", ItemNumber: 12,
		Title: "Fix", Body: "item description", CommentBody: "please rebase",
	}
	in := gateScriptInput(task, nil, comment)
	assert.Equal(t, "please rebase", in.Stdin, "a comment event is about the new comment")
	assert.Equal(t, "event", in.Env["CLAWBENCH_TRIGGER"])
	assert.Equal(t, "acme/widgets", in.Env["CLAWBENCH_REPO"])
	assert.Equal(t, "12", in.Env["CLAWBENCH_ITEM_NUMBER"])
	assert.Equal(t, "0", in.Env["CLAWBENCH_DRAFT"])
	assert.Equal(t, "0", in.Env["CLAWBENCH_ACTOR_IS_SELF"])

	opened := &EventContext{EventType: "opened", ItemType: "issue", ItemNumber: 3, Body: "item description"}
	assert.Equal(t, "item description", gateScriptInput(task, nil, opened).Stdin)

	// Pipeline fields only exist when the event carries a run.
	noRun := gateScriptInput(task, nil, opened)
	_, hasPipeline := noRun.Env["CLAWBENCH_PIPELINE_STATUS"]
	assert.False(t, hasPipeline)
}

func TestGateScriptInput_EventPipelineVars(t *testing.T) {
	task := &model.ScheduledTask{ID: 1, Name: "CI", TriggerMode: "event"}
	ec := &EventContext{
		EventType: "pipeline_done", Repo: "acme/widgets", ItemType: "pipeline",
		Pipeline: &forge.PipelineRun{
			Number: 9, Status: forge.PipelineFailure,
			URL: "https://ci.example/run/9", Ref: "main", SHA: "abc123",
		},
	}

	in := gateScriptInput(task, nil, ec)
	assert.Equal(t, "failure", in.Env["CLAWBENCH_PIPELINE_STATUS"])
	assert.Equal(t, "https://ci.example/run/9", in.Env["CLAWBENCH_PIPELINE_URL"])
	assert.Equal(t, "9", in.Env["CLAWBENCH_PIPELINE_NUMBER"])
	assert.Equal(t, "main", in.Env["CLAWBENCH_PIPELINE_REF"])
	assert.Equal(t, "abc123", in.Env["CLAWBENCH_PIPELINE_SHA"])
}

func TestMergeScriptEnv_OverridesInherited(t *testing.T) {
	t.Setenv("CLAWBENCH_TEST_OVERRIDE", "from-parent")
	t.Setenv("CLAWBENCH_TEST_KEEP", "kept")

	env := mergeScriptEnv(map[string]string{
		"CLAWBENCH_TEST_OVERRIDE": "from-trigger",
		"CLAWBENCH_TEST_NEW":      "added",
	})

	var overrides, kept int
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "CLAWBENCH_TEST_OVERRIDE="):
			overrides++
			assert.Equal(t, "CLAWBENCH_TEST_OVERRIDE=from-trigger", kv)
		case strings.HasPrefix(kv, "CLAWBENCH_TEST_KEEP="):
			kept++
			assert.Equal(t, "CLAWBENCH_TEST_KEEP=kept", kv)
		}
	}
	assert.Equal(t, 1, overrides, "the inherited value must be replaced, not duplicated")
	assert.Equal(t, 1, kept, "unrelated inherited variables must survive")
	assert.Contains(t, env, "CLAWBENCH_TEST_NEW=added")
}

func TestSanitizeScriptEnvValue(t *testing.T) {
	// A NUL byte makes exec fail outright, which would read as a closed gate.
	assert.Equal(t, "ab", sanitizeScriptEnvValue("a\x00b"))

	long := strings.Repeat("中", scriptEnvValueMax) // 3 bytes each
	got := sanitizeScriptEnvValue(long)
	require.LessOrEqual(t, len(got), scriptEnvValueMax)
	assert.True(t, strings.HasPrefix(long, got), "the cap must cut a prefix, not garble it")
	assert.Equal(t, strings.ToValidUTF8(got, ""), got, "a partially cut rune must be dropped")
}
