package tasks

import "clawbench/internal/plugins"

// MountTasksRoutes 挂载任务功能的 HTTP 接口。
func MountTasksRoutes(m plugins.Mount) {
	m.Auth("/api/tasks", ServeTasks)
	m.Auth("/api/tasks/", ServeTaskByID)
}
