package model

import (
	"strings"
	"time"
)

// ScheduledTask represents a cron-scheduled AI task.
type ScheduledTask struct {
	ID          int64  `json:"id"`
	ProjectPath string `json:"projectPath"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CronExpr    string `json:"cronExpr"`
	AgentID     string `json:"agentId"`
	Prompt      string `json:"prompt"`
	// TriggerMode selects how the task runs: "cron" (default), "event" (forge)
	// or "email" (new mail matching the gate below).
	TriggerMode string `json:"triggerMode,omitempty"`
	// EventTypes lists the forge event types that trigger an event task
	// (comma-separated: opened,closed,merged,reopened,commented,pipeline_done).
	//
	// An event task always watches its own project's bound repository, so there
	// is deliberately no repository field: the binding is the single source of
	// truth and cannot drift from the task configuration.
	EventTypes string `json:"eventTypes,omitempty"`
	// MailAccountID / MailFolder select the mailbox an email task watches.
	// MailFolder is the IMAP path ("" = the inbox). The account is required:
	// a mailbox belongs to a person, and firing on "whatever account happens to
	// exist" would make the same task behave differently after a second account
	// is added.
	MailAccountID int64  `json:"mailAccountId,omitempty"`
	MailFolder    string `json:"mailFolder,omitempty"`
	// MailFrom / MailSubject / MailBody are substring gates (case-insensitive,
	// ALL must match). Empty means "do not constrain on this field", so an
	// email task with no conditions fires for every new message in the folder.
	MailFrom    string `json:"mailFrom,omitempty"`
	MailSubject string `json:"mailSubject,omitempty"`
	MailBody    string `json:"mailBody,omitempty"`
	// MailHasAttachment additionally requires at least one non-inline
	// attachment.
	MailHasAttachment bool `json:"mailHasAttachment,omitempty"`
	// Script is an optional gating shell script run BEFORE the AI call. Exit
	// code 0 opens the gate and the AI turn runs; any other outcome (non-zero,
	// timeout) closes it and the run is skipped without a session. Its result
	// is recorded on the execution and its output is available to the prompt
	// through the {{code}} / {{stdout}} / {{stderr}} / {{output}} template
	// variables. Only meaningful for cron tasks.
	Script string `json:"script,omitempty"`
	// ScriptTimeout bounds Script in seconds; 0 means DefaultScriptTimeout.
	ScriptTimeout     int                    `json:"scriptTimeout,omitempty"`
	SessionID         string                 `json:"sessionId,omitempty"`
	Status            string                 `json:"status"`     // active / paused / completed
	RepeatMode        string                 `json:"repeatMode"` // once / limited / unlimited
	MaxRuns           int                    `json:"maxRuns"`
	LastRunAt         *time.Time             `json:"lastRunAt,omitempty"`
	NextRunAt         *time.Time             `json:"nextRunAt,omitempty"`
	RunCount          int                    `json:"runCount"`
	LastReadAt        *time.Time             `json:"lastReadAt,omitempty"`
	UnreadCount       int                    `json:"unreadCount,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	RunningExecutions []RunningExecutionView `json:"runningExecutions,omitempty"`
	RunningCount      int                    `json:"runningCount,omitempty"`
}

// IsEventTriggered reports whether the task runs on forge events rather than a
// cron schedule.
func (t *ScheduledTask) IsEventTriggered() bool {
	return t.TriggerMode == "event"
}

// IsEmailTriggered reports whether the task runs when new mail arrives.
func (t *ScheduledTask) IsEmailTriggered() bool {
	return t.TriggerMode == "email"
}

// HasEventSource reports whether the task runs on an external event rather than
// a cron schedule. Both kinds carry no NextRunAt.
func (t *ScheduledTask) HasEventSource() bool {
	return t.IsEventTriggered() || t.IsEmailTriggered()
}

// EventTypeList splits the stored comma-separated event types.
func (t *ScheduledTask) EventTypeList() []string {
	return SplitEventTypes(t.EventTypes)
}

// SplitEventTypes parses a comma-separated event subscription into a clean list,
// dropping empty entries so "a,, b" yields ["a", "b"].
func SplitEventTypes(eventTypes string) []string {
	if eventTypes == "" {
		return nil
	}
	parts := strings.Split(eventTypes, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RunningExecutionView is the frontend-facing representation of a running task execution.
type RunningExecutionView struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"startedAt"`
	TriggerType string    `json:"triggerType"` // "auto" | "manual"
	// Phase distinguishes the pre-AI script stage ("script") from the AI turn
	// ("ai"), so the UI can label the row and runningCount can stay AI-only.
	Phase string `json:"phase"`
}
