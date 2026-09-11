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
	EventTypes string `json:"eventTypes,omitempty"`
	// EventRepo scopes an event task to a repository (platform|host|owner/repo).
	// Empty means "any bound repository in the task's project".
	EventRepo         string                 `json:"eventRepo,omitempty"`
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
	if t.EventTypes == "" {
		return nil
	}
	parts := strings.Split(t.EventTypes, ",")
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
}
