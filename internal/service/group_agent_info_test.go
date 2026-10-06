package service

import (
	"testing"

	"clawbench/internal/model"
)

func TestGetAgentSpecialty(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"spec-test-with":    {ID: "spec-test-with", Name: "WithSpec", Specialty: "代码编写与推理"},
		"spec-test-without": {ID: "spec-test-without", Name: "NoSpec"},
	}
	defer func() { model.Agents = origAgents }()

	if got := GetAgentSpecialty("spec-test-with"); got != "代码编写与推理" {
		t.Fatalf("expected specialty, got %q", got)
	}
	// No specialty: empty string, NOT the name or a placeholder — callers rely
	// on "" to omit the parenthetical.
	if got := GetAgentSpecialty("spec-test-without"); got != "" {
		t.Fatalf("expected empty for agent without specialty, got %q", got)
	}
	// Unknown id and empty id must both be empty rather than panic.
	if got := GetAgentSpecialty("does-not-exist"); got != "" {
		t.Fatalf("expected empty for unknown agent, got %q", got)
	}
	if got := GetAgentSpecialty(""); got != "" {
		t.Fatalf("expected empty for empty id, got %q", got)
	}
}
