package ai

import (
	"errors"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestListSessionsFromDisk_NoBackend_ReturnsError(t *testing.T) {
	backend := "acp-list-no-backend"
	defer ListSessionsFromDiskRegister(backend, nil)

	assert.False(t, HasListSessionsFromDisk(backend))

	_, err := ListSessionsFromDisk(&model.Agent{Backend: backend}, "/tmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no on-disk ListSessions implementation registered for backend "+backend)
}

func TestListSessionsFromDisk_RegisteredBackend_InvokesScanner(t *testing.T) {
	backend := "acp-list-registered"
	var gotAgent *model.Agent
	var gotCwd string
	ListSessionsFromDiskRegister(backend, func(agent *model.Agent, cwd string) ([]acp.SessionInfo, error) {
		gotAgent = agent
		gotCwd = cwd
		return []acp.SessionInfo{{SessionId: "s1", Cwd: cwd}}, nil
	})
	defer ListSessionsFromDiskRegister(backend, nil)

	assert.True(t, HasListSessionsFromDisk(backend))

	agent := &model.Agent{Backend: backend}
	sessions, err := ListSessionsFromDisk(agent, "/proj")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, acp.SessionId("s1"), sessions[0].SessionId)
	assert.Equal(t, "/proj", sessions[0].Cwd)
	assert.Same(t, agent, gotAgent)
	assert.Equal(t, "/proj", gotCwd)
}

func TestListSessionsFromDisk_ScannerReturnsError(t *testing.T) {
	backend := "acp-list-error"
	scanErr := "scanner exploded"
	ListSessionsFromDiskRegister(backend, func(*model.Agent, string) ([]acp.SessionInfo, error) {
		return nil, errors.New(scanErr)
	})
	defer ListSessionsFromDiskRegister(backend, nil)

	_, err := ListSessionsFromDisk(&model.Agent{Backend: backend}, "/proj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), scanErr)
}

func TestListSessionsFromDisk_Deregistered_RemovesScanner(t *testing.T) {
	backend := "acp-list-dereg"
	ListSessionsFromDiskRegister(backend, func(*model.Agent, string) ([]acp.SessionInfo, error) {
		return nil, nil
	})
	assert.True(t, HasListSessionsFromDisk(backend))

	ListSessionsFromDiskRegister(backend, nil)
	assert.False(t, HasListSessionsFromDisk(backend))

	_, err := ListSessionsFromDisk(&model.Agent{Backend: backend}, "/proj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no on-disk ListSessions implementation")
}
