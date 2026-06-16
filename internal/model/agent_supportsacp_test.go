package model_test

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestSupportsACP_WithAcpCommand(t *testing.T) {
	agent := &model.Agent{
		AcpCommand: "kimi --acp",
	}
	assert.True(t, agent.SupportsACP(), "agent with AcpCommand set should support ACP")
}

func TestSupportsACP_WithoutAcpCommand(t *testing.T) {
	agent := &model.Agent{
		AcpCommand: "",
	}
	assert.False(t, agent.SupportsACP(), "agent without AcpCommand should not support ACP")
}

func TestSupportsACP_DefaultAgent(t *testing.T) {
	agent := &model.Agent{}
	assert.False(t, agent.SupportsACP(), "default zero-value agent should not support ACP")
}
