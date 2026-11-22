package ai

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// modelOpt builds the `category: "model"` config option an ACP agent reports at
// session/new, with current as its currentValue.
func modelOpt(current string) acp.SessionConfigOption {
	category := acp.SessionConfigOptionCategoryModel
	return acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Id:           acp.SessionConfigId("model"),
			Name:         "Model",
			Type:         "select",
			Category:     &category,
			CurrentValue: acp.SessionConfigValueId(current),
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
					{Name: "Pro", Value: acp.SessionConfigValueId("pro")},
					{Name: "Fast", Value: acp.SessionConfigValueId("fast")},
				},
			},
		},
	}
}

// The self-test has to name the model it is about to exercise, and it runs
// BEFORE the turn extracts the NewSessionResponse — so the connection's own
// current model is still empty at that point. This reads the pending response
// instead.
func TestReportedModelFromNewSession(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "a"}, "reported-model")
	conn.lastNewSessionResp = &acp.NewSessionResponse{
		SessionId:     acp.SessionId("sess-1"),
		ConfigOptions: []acp.SessionConfigOption{modelOpt("pro")},
	}

	if got := conn.ReportedModelFromNewSession(); got != "pro" {
		t.Fatalf("reported model = %q, want pro", got)
	}
	// Reading must NOT consume the response: the turn's own extraction is what
	// teaches the registry the model list, so a caller peeking early cannot be
	// allowed to take it away.
	if conn.lastNewSessionResp == nil {
		t.Fatal("ReportedModelFromNewSession consumed the NewSessionResponse")
	}
	if got := conn.ReportedModelFromNewSession(); got != "pro" {
		t.Fatalf("second read = %q, want pro (idempotent)", got)
	}
}

// No pending response, or a response without a model option, must report "" so
// the caller falls back to leaving the choice to the agent.
func TestReportedModelFromNewSession_NoModel(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "a"}, "reported-model-none")
	if got := conn.ReportedModelFromNewSession(); got != "" {
		t.Fatalf("no pending response: got %q, want empty", got)
	}

	conn.lastNewSessionResp = &acp.NewSessionResponse{SessionId: acp.SessionId("sess-2")}
	if got := conn.ReportedModelFromNewSession(); got != "" {
		t.Fatalf("response without options: got %q, want empty", got)
	}
}
