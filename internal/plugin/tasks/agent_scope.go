package tasks

import (
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// agentVisibleInProject reports whether agent may be used as a NEW entry in the
// given project. A global agent (ProjectID == nil) is visible everywhere; a
// project-scoped agent is visible only in the project whose id it carries.
//
// 与 handler 的同名函数一致：任务面板的"可用智能体"下拉要和聊天侧同一个口径。
func agentVisibleInProject(agent *model.Agent, projectPath string) bool {
	if agent == nil {
		return false
	}
	if agent.ProjectID == nil {
		return true // global
	}
	projectID, ok, err := store.ProjectIDByPath(projectPath)
	if err != nil || !ok {
		return false
	}
	return *agent.ProjectID == projectID
}
