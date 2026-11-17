package service

import (
	"strconv"

	"clawbench/internal/forge"
	"clawbench/internal/model"
)

// A gating script runs before the AI turn for every trigger kind. This file is
// the single place that decides what the script can see: short structured
// fields become environment variables, the long free-text body goes on stdin.
//
// The registry (gateScriptVars) is ordered and labeled so the task form can
// render exactly the variables the backend will set — the frontend mirrors it
// in web/src/utils/gateScriptVars.ts, and a parity test keeps the two in step.
// Adding a variable means touching both sides.

// gateScriptScope says which trigger kinds a variable belongs to.
type gateScriptScope int

const (
	// gateScriptCommon applies to cron, email and event tasks alike.
	gateScriptCommon gateScriptScope = iota
	// gateScriptMail only exists for an email-triggered run.
	gateScriptMail
	// gateScriptEvent only exists for a forge (GitHub/GitLab) event run.
	gateScriptEvent
)

// gateScriptVar describes one environment variable.
type gateScriptVar struct {
	// Name is the environment variable name, e.g. CLAWBENCH_MAIL_SUBJECT.
	Name string
	// LabelKey is the i18n message ID shown next to the name in the task form.
	LabelKey string
	Scope    gateScriptScope
	// Always keeps a variable even when its value is empty. Used for booleans
	// ("0" is meaningful) — every other empty field is omitted so a script can
	// test with `[ -n "$VAR" ]` and not see a misleading blank line.
	Always bool
	value  func(task *model.ScheduledTask, mc *MailEventContext, ec *EventContext) string
}

// gateScriptVars is the ordered registry. Order is stable so the form's guide
// panel reads in the same order as the variables are built.
var gateScriptVars = []gateScriptVar{
	{
		Name: "CLAWBENCH_TRIGGER", LabelKey: "task.form.scriptVarTrigger", Scope: gateScriptCommon,
		value: func(t *model.ScheduledTask, _ *MailEventContext, _ *EventContext) string {
			return gateScriptTriggerName(t)
		},
	},
	{
		Name: "CLAWBENCH_TASK_ID", LabelKey: "task.form.scriptVarTaskId", Scope: gateScriptCommon,
		value: func(t *model.ScheduledTask, _ *MailEventContext, _ *EventContext) string {
			return strconv.FormatInt(t.ID, 10)
		},
	},
	{
		Name: "CLAWBENCH_TASK_NAME", LabelKey: "task.form.scriptVarTaskName", Scope: gateScriptCommon,
		value: func(t *model.ScheduledTask, _ *MailEventContext, _ *EventContext) string { return t.Name },
	},
	{
		Name: "CLAWBENCH_PROJECT_PATH", LabelKey: "task.form.scriptVarProjectPath", Scope: gateScriptCommon,
		value: func(t *model.ScheduledTask, _ *MailEventContext, _ *EventContext) string { return t.ProjectPath },
	},

	// ── Email ────────────────────────────────────────────────────────────
	{
		Name: "CLAWBENCH_MAIL_ACCOUNT", LabelKey: "task.form.scriptVarMailAccount", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.AccountName },
	},
	{
		Name: "CLAWBENCH_MAIL_FOLDER", LabelKey: "task.form.scriptVarMailFolder", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.Folder },
	},
	{
		Name: "CLAWBENCH_MAIL_FROM", LabelKey: "task.form.scriptVarMailFrom", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.From },
	},
	{
		Name: "CLAWBENCH_MAIL_TO", LabelKey: "task.form.scriptVarMailTo", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.To },
	},
	{
		Name: "CLAWBENCH_MAIL_SUBJECT", LabelKey: "task.form.scriptVarMailSubject", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.Subject },
	},
	{
		Name: "CLAWBENCH_MAIL_DATE", LabelKey: "task.form.scriptVarMailDate", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string { return mc.Date },
	},
	{
		Name: "CLAWBENCH_MAIL_HAS_ATTACHMENT", LabelKey: "task.form.scriptVarMailHasAttachment", Scope: gateScriptMail, Always: true,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string {
			return scriptBoolFlag(mc.HasAttachments)
		},
	},
	{
		Name: "CLAWBENCH_MAIL_MESSAGE_ID", LabelKey: "task.form.scriptVarMailMessageId", Scope: gateScriptMail,
		value: func(_ *model.ScheduledTask, mc *MailEventContext, _ *EventContext) string {
			if mc.MessageID <= 0 {
				return ""
			}
			return strconv.FormatInt(mc.MessageID, 10)
		},
	},

	// ── Forge event (GitHub / GitLab) ────────────────────────────────────
	{
		Name: "CLAWBENCH_EVENT_TYPE", LabelKey: "task.form.scriptVarEventType", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.EventType },
	},
	{
		Name: "CLAWBENCH_REPO", LabelKey: "task.form.scriptVarRepo", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.Repo },
	},
	{
		Name: "CLAWBENCH_ITEM_TYPE", LabelKey: "task.form.scriptVarItemType", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.ItemType },
	},
	{
		Name: "CLAWBENCH_ITEM_NUMBER", LabelKey: "task.form.scriptVarItemNumber", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			if ec.ItemNumber <= 0 {
				return ""
			}
			return strconv.Itoa(ec.ItemNumber)
		},
	},
	{
		Name: "CLAWBENCH_TITLE", LabelKey: "task.form.scriptVarTitle", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.Title },
	},
	{
		Name: "CLAWBENCH_URL", LabelKey: "task.form.scriptVarUrl", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.URL },
	},
	{
		Name: "CLAWBENCH_AUTHOR", LabelKey: "task.form.scriptVarAuthor", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.Author },
	},
	{
		Name: "CLAWBENCH_STATE", LabelKey: "task.form.scriptVarState", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.State },
	},
	{
		Name: "CLAWBENCH_PREV_STATE", LabelKey: "task.form.scriptVarPrevState", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.PrevState },
	},
	{
		Name: "CLAWBENCH_LABELS", LabelKey: "task.form.scriptVarLabels", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.Labels },
	},
	{
		Name: "CLAWBENCH_ASSIGNEES", LabelKey: "task.form.scriptVarAssignees", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.Assignees },
	},
	{
		Name: "CLAWBENCH_SOURCE_BRANCH", LabelKey: "task.form.scriptVarSourceBranch", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string { return ec.SourceBranch },
	},
	{
		Name: "CLAWBENCH_DRAFT", LabelKey: "task.form.scriptVarDraft", Scope: gateScriptEvent, Always: true,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return scriptBoolFlag(ec.Draft)
		},
	},
	{
		Name: "CLAWBENCH_ACTOR_IS_SELF", LabelKey: "task.form.scriptVarActorIsSelf", Scope: gateScriptEvent, Always: true,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return scriptBoolFlag(ec.ActorIsSelf)
		},
	},
	{
		Name: "CLAWBENCH_PIPELINE_STATUS", LabelKey: "task.form.scriptVarPipelineStatus", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return pipelineField(*ec, func(p *forge.PipelineRun) string { return string(p.Status) })
		},
	},
	{
		Name: "CLAWBENCH_PIPELINE_URL", LabelKey: "task.form.scriptVarPipelineUrl", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return pipelineField(*ec, func(p *forge.PipelineRun) string { return p.URL })
		},
	},
	{
		Name: "CLAWBENCH_PIPELINE_NUMBER", LabelKey: "task.form.scriptVarPipelineNumber", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return pipelineField(*ec, func(p *forge.PipelineRun) string {
				if p.Number <= 0 {
					return ""
				}
				return strconv.Itoa(p.Number)
			})
		},
	},
	{
		Name: "CLAWBENCH_PIPELINE_REF", LabelKey: "task.form.scriptVarPipelineRef", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return pipelineField(*ec, func(p *forge.PipelineRun) string { return p.Ref })
		},
	},
	{
		Name: "CLAWBENCH_PIPELINE_SHA", LabelKey: "task.form.scriptVarPipelineSha", Scope: gateScriptEvent,
		value: func(_ *model.ScheduledTask, _ *MailEventContext, ec *EventContext) string {
			return pipelineField(*ec, func(p *forge.PipelineRun) string { return p.SHA })
		},
	},
}

// gateScriptTriggerName renders the trigger kind the script is running for.
// It follows the task's configured mode, not the raw triggerType: a manual run
// of a cron task is still a cron task as far as the gate is concerned.
func gateScriptTriggerName(task *model.ScheduledTask) string {
	switch {
	case task.IsEmailTriggered():
		return "email"
	case task.IsEventTriggered():
		return "event"
	default:
		return "cron"
	}
}

// gateScriptInput builds the environment and stdin for one run.
func gateScriptInput(task *model.ScheduledTask, mc *MailEventContext, ec *EventContext) ScriptInput {
	env := make(map[string]string, len(gateScriptVars))
	for _, v := range gateScriptVars {
		switch v.Scope {
		case gateScriptMail:
			if mc == nil {
				continue
			}
		case gateScriptEvent:
			if ec == nil {
				continue
			}
		}
		value := v.value(task, mc, ec)
		if value == "" && !v.Always {
			continue
		}
		env[v.Name] = value
	}

	in := ScriptInput{Env: env}
	switch {
	case mc != nil:
		in.Stdin = mc.Body
	case ec != nil:
		in.Stdin = eventScriptStdin(ec)
	}
	return in
}

// eventScriptStdin picks the text this event is "about": the new comment for a
// comment event, otherwise the item description. Both are capped like the
// prompt's own body variables so a huge description cannot flood the script.
func eventScriptStdin(ec *EventContext) string {
	if ec.EventType == "commented" && ec.CommentBody != "" {
		return truncateRunes(ec.CommentBody, eventBodyMaxRunes)
	}
	return truncateRunes(ec.Body, eventBodyMaxRunes)
}

// boolFlag renders a boolean as 1/0 for shell-friendly tests.
func scriptBoolFlag(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
