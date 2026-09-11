package service_test

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingProvider counts how many times comments were fetched, so the
// comment-降频 behavior can be asserted.
type recordingProvider struct {
	commentCalls int
	items        []forge.Item
}

func (p *recordingProvider) CurrentUser(context.Context) (forge.Author, error) {
	return forge.Author{Login: "me"}, nil
}

func (p *recordingProvider) ListItems(_ context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	if opts.Page > 1 {
		return forge.ListResult{}, nil
	}
	return forge.ListResult{Items: p.items}, nil
}

func (p *recordingProvider) GetItem(context.Context, forge.ItemType, int) (forge.Item, error) {
	return forge.Item{}, nil
}

func (p *recordingProvider) ListComments(context.Context, forge.ItemType, int, int, int) ([]forge.Comment, error) {
	p.commentCalls++
	return nil, nil
}

// TestForgeSyncer_StateOnlyPassSkipsComments verifies the comment-降频 flag is
// honored: a state-only pass must not fetch comments at all.
func TestForgeSyncer_StateOnlyPassSkipsComments(t *testing.T) {
	setupTestDBForForgeSync(t)
	provider := &recordingProvider{items: []forge.Item{{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 1,
		State: forge.StateOpen, UpdatedAt: time.Now(),
	}}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{IncludeComments: false}))
	assert.Zero(t, provider.commentCalls, "a state-only pass must not fetch comments")

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{IncludeComments: true}))
	assert.Greater(t, provider.commentCalls, 0, "a full pass must fetch comments")
}

// TestForgePoller_SyncNowDeduplicatesRepos verifies the poller fetches a repo
// once even when two projects are bound to it.
func TestForgePoller_SyncNowDeduplicatesRepos(t *testing.T) {
	setupTestDBForForgeSync(t)

	// Two projects, same repository.
	for range 2 {
		require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
			ProjectPath: t.TempDir(), Platform: "github", Host: "github.com",
			Owner: "acme", Repo: "widgets", Source: "manual",
		}))
	}

	provider := &recordingProvider{}
	var factoryCalls int
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		factoryCalls++
		return provider, nil
	}, nil)

	limiter := forge.NewLimiter(1000, 1000, 4)
	poller := service.NewForgePoller(syncer, limiter, func() model.Config { return model.Config{} })
	poller.SyncNow()

	assert.Equal(t, 1, factoryCalls, "the same repo bound to two projects must be polled once")
}

// TestForgePoller_SyncNowSkipsWhenNoBindings verifies an empty binding set is a
// cheap no-op.
func TestForgePoller_SyncNowSkipsWhenNoBindings(t *testing.T) {
	setupTestDBForForgeSync(t)
	called := false
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		called = true
		return &recordingProvider{}, nil
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	poller.SyncNow()
	assert.False(t, called, "no bindings means no provider is built")
}

// TestForgePoller_StartStop verifies the lifecycle is clean and idempotent.
func TestForgePoller_StartStop(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	poller.Start()
	poller.Start() // idempotent
	poller.Stop()
	poller.Stop() // idempotent
}

// TestForgePoller_GlobalLifecycle verifies the global start/stop helpers.
func TestForgePoller_GlobalLifecycle(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil
	}, nil)

	service.StartForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })
	service.StartForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} }) // no-op
	assert.True(t, service.ForgePollerRunning())

	service.StopForgePoller()
	assert.False(t, service.ForgePollerRunning())
}
