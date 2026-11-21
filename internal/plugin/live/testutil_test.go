package live

import (
	"net/http"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

func newRequest(t *testing.T, method, path string, body any) *http.Request {
	return handlertest.NewRequest(t, method, path, body)
}

// saveFeatureSwitchState snapshots the feature-switch globals and restores them
// after the test (moved from handler/feature_switch_test.go).
func saveFeatureSwitchState(t *testing.T) {
	t.Helper()
	origCfg := model.ConfigInstance
	origTasks := model.TasksEnabled
	origShare := model.ShareEnabled
	origPolicy := model.FeaturePolicy
	origDataDir := model.DataDir
	origScheduler := service.GlobalScheduler

	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.TasksEnabled = origTasks
		model.ShareEnabled = origShare
		model.FeaturePolicy = origPolicy
		model.DataDir = origDataDir
		service.GlobalScheduler = origScheduler
	})
}
