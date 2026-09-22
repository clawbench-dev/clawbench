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
	// TriggerMode selects how the task runs: "cron" (default) or "event".
	TriggerMode string `json:"triggerMode,omitempty"`
	// EventTypes lists the forge event types that trigger an event task
	// (comma-separated: opened,closed,merged,reopened,commented,pipeline_done).
	//
	// An event task always watches its own project's bound repository, so there
	// is deliberately no repository field: the binding is the single source of
	// truth and cannot drift from the task configuration.
	EventTypes string `json:"eventTypes,omitempty"`
	// Script is an optional shell script run BEFORE the AI call. When it exits
	// 0 with no output the run is skipped entirely (no session, no
	// notification); otherwise its output is injected into the prompt.
	// Only meaningful for cron tasks.
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
