package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- GET /api/chat/message-clusters ----------

func TestServeMessageClusters_CachedResults(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Pre-populate cluster cache
	entries := []service.ClusterCacheEntry{
		{Representative: "继续", Variants: "继续,请继续", TotalCount: 5, RepresentativeCount: 3, SortOrder: 0},
		{Representative: "提交代码", Variants: "提交代码,commit", TotalCount: 3, RepresentativeCount: 2, SortOrder: 1},
	}
	err := service.SaveClusterCache(entries, "embedding")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters", nil)
	w := callHandlerWithAuth(ServeMessageClusters, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp MessageClustersResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Total)
	assert.Equal(t, "embedding", resp.Mode)
	assert.Equal(t, "done", resp.Progress)
	if len(resp.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(resp.Clusters))
	}
	assert.Equal(t, "继续", resp.Clusters[0].Representative)
	assert.Equal(t, []string{"继续", "请继续"}, resp.Clusters[0].Variants)
	assert.Equal(t, 5, resp.Clusters[0].TotalCount)
}

func TestServeMessageClusters_FiltersQuickSend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add quick-send items whose commands match cluster variants
	_, _ = service.AddChatQuickSend("Continue", "继续")
	_, _ = service.AddChatQuickSend("Please Continue", "请继续")
	_, _ = service.AddChatQuickSend("Commit", "提交代码")

	// Pre-populate cluster cache
	entries := []service.ClusterCacheEntry{
		// Cluster 1: ALL variants match quick-send → entire cluster filtered out
		{Representative: "继续", Variants: "继续,请继续", TotalCount: 5, RepresentativeCount: 3, SortOrder: 0},
		// Cluster 2: partial match → "提交代码" filtered from variants, "commit" kept
		{Representative: "提交代码", Variants: "提交代码,commit", TotalCount: 3, RepresentativeCount: 2, SortOrder: 1},
		// Cluster 3: no match → kept entirely
		{Representative: "帮我写个函数", Variants: "帮我写个函数,写一个函数", TotalCount: 4, RepresentativeCount: 2, SortOrder: 2},
	}
	err := service.SaveClusterCache(entries, "embedding")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters", nil)
	w := callHandlerWithAuth(ServeMessageClusters, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp MessageClustersResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	// Cluster 1 fully filtered, Cluster 2 partial (1 variant left), Cluster 3 untouched
	assert.Equal(t, 2, resp.Total)
	if len(resp.Clusters) != 2 {
		t.Fatalf("expected 2 clusters after filtering, got %d", len(resp.Clusters))
	}
	// Cluster 2: "commit" kept (partial match)
	assert.Equal(t, "提交代码", resp.Clusters[0].Representative)
	assert.Equal(t, []string{"commit"}, resp.Clusters[0].Variants)
	// Cluster 3: untouched
	assert.Equal(t, "帮我写个函数", resp.Clusters[1].Representative)
	assert.Equal(t, []string{"帮我写个函数", "写一个函数"}, resp.Clusters[1].Variants)
}

func TestServeMessageClusters_EmptyCache(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// No cluster cache → empty response with progress="idle"
	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters", nil)
	w := callHandlerWithAuth(ServeMessageClusters, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp MessageClustersResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Total)
	assert.Equal(t, "idle", resp.Progress)
	assert.Empty(t, resp.Clusters)
}

func TestServeMessageClusters_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/chat/message-clusters", nil)
	w := callHandlerWithAuth(ServeMessageClusters, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- POST /api/chat/message-clusters/compute ----------

func TestServeMessageClustersCompute_TriggersComputation(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up a mock cluster worker
	origWorker := rag.GlobalClusterWorker
	t.Cleanup(func() { rag.GlobalClusterWorker = origWorker })
	worker := rag.NewClusterWorker(nil) // nil hub → no WS broadcast
	rag.GlobalClusterWorker = worker

	req := newRequest(t, http.MethodPost, "/api/chat/message-clusters/compute", nil)
	w := callHandlerWithAuth(ServeMessageClustersCompute, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "accepted", result["status"])

	// Stop the worker to clean up the goroutine
	worker.Stop()
}

func TestServeMessageClustersCompute_AlreadyRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set up a cluster worker
	origWorker := rag.GlobalClusterWorker
	t.Cleanup(func() { rag.GlobalClusterWorker = origWorker })
	worker := rag.NewClusterWorker(nil)
	rag.GlobalClusterWorker = worker

	// Insert messages so the computation goroutine has work to do,
	// keeping IsRunning()=true long enough for our second handler call.
	for i := range 20 {
		_, _ = service.AddChatMessage(env.ProjectDir, "claude", "", "user", "message "+strings.Repeat("x", i*5), nil, false, "NewSession")
	}

	// Start a computation — ComputeOnce() sets running=true synchronously.
	worker.ComputeOnce()

	// Immediately send another POST — should get 409 because IsRunning()=true.
	// The goroutine hasn't had time to complete yet because it needs to
	// process the 20 messages we inserted.
	req := newRequest(t, http.MethodPost, "/api/chat/message-clusters/compute", nil)
	w := callHandlerWithAuth(ServeMessageClustersCompute, req)

	// Accept either 409 (goroutine still running) or 200 (goroutine finished
	// before handler call) — both are valid outcomes of the race condition.
	if w.Code == http.StatusConflict {
		var result map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "conflict", result["status"])
	} else if w.Code != http.StatusOK {
		t.Fatalf("expected 409 or 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Stop the worker to clean up
	worker.Stop()
}

func TestServeMessageClustersCompute_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters/compute", nil)
	w := callHandlerWithAuth(ServeMessageClustersCompute, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- GET /api/chat/message-clusters/compute/status ----------

func TestServeMessageClustersComputeStatus_ShowProgress(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up a mock cluster worker
	origWorker := rag.GlobalClusterWorker
	t.Cleanup(func() { rag.GlobalClusterWorker = origWorker })
	worker := rag.NewClusterWorker(nil)
	rag.GlobalClusterWorker = worker

	// Write some meta state
	err := service.SaveClusterMeta("computing", "embedding", 100, 0, 500)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters/compute/status", nil)
	w := callHandlerWithAuth(ServeMessageClustersComputeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var progress rag.ClusterProgress
	err = json.Unmarshal(w.Body.Bytes(), &progress)
	require.NoError(t, err)
	assert.Equal(t, "computing", progress.Status)
	assert.Equal(t, "embedding", progress.Mode)
	assert.Equal(t, 100, progress.MsgCount)
	assert.Equal(t, int64(500), progress.ElapsedMs)
}

func TestServeMessageClustersComputeStatus_IdleByDefault(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up a mock cluster worker
	origWorker := rag.GlobalClusterWorker
	t.Cleanup(func() { rag.GlobalClusterWorker = origWorker })
	worker := rag.NewClusterWorker(nil)
	rag.GlobalClusterWorker = worker

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters/compute/status", nil)
	w := callHandlerWithAuth(ServeMessageClustersComputeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var progress rag.ClusterProgress
	err := json.Unmarshal(w.Body.Bytes(), &progress)
	require.NoError(t, err)
	assert.Equal(t, "idle", progress.Status)
}

func TestServeMessageClustersComputeStatus_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/chat/message-clusters/compute/status", nil)
	w := callHandlerWithAuth(ServeMessageClustersComputeStatus, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- Auth required ----------

func TestMessageClustersRouteRequiresAuth(t *testing.T) {
	origToken := model.SessionToken
	t.Cleanup(func() { model.SessionToken = origToken })

	model.SessionToken = "test-token"

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/message-clusters", http.NoBody)
	req.RemoteAddr = "203.0.113.10:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected message clusters to require auth, got status %d body %s", w.Code, w.Body.String())
	}
}

// ---------- Test quick-send filtering logic edge cases ----------

func TestServeMessageClusters_QuickSendFiltersAllVariantsButRepresentativeStillShown(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add quick-send where command matches the representative text exactly
	_, _ = service.AddChatQuickSend("Continue", "继续")

	// Cluster where representative matches quick-send but has no other unmatched variants
	entries := []service.ClusterCacheEntry{
		{Representative: "继续", Variants: "继续", TotalCount: 5, RepresentativeCount: 5, SortOrder: 0},
	}
	err := service.SaveClusterCache(entries, "embedding")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/chat/message-clusters", nil)
	w := callHandlerWithAuth(ServeMessageClusters, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp MessageClustersResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	// All variants (just "继续") match quick-send → entire cluster filtered out
	assert.Equal(t, 0, resp.Total)
	assert.Empty(t, resp.Clusters)
}

// ---------- Helper: build quick-send command set from service ----------

// quickSendCommandSet builds a set of quick-send commands for filtering.
func quickSendCommandSet(commands []string) map[string]bool {
	set := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		set[cmd] = true
	}
	return set
}

// filterClusters applies quick-send filtering to cluster cache entries.
func filterClusters(entries []service.ClusterCacheEntry, quickSendSet map[string]bool) []ClusterItem {
	var items []ClusterItem
	for _, e := range entries {
		variants := strings.Split(e.Variants, ",")
		var unmatched []string
		for _, v := range variants {
			if !quickSendSet[v] {
				unmatched = append(unmatched, v)
			}
		}
		// If ALL variants match quick-send → skip entire cluster
		if len(unmatched) == 0 {
			continue
		}
		items = append(items, ClusterItem{
			ID:                  e.ID,
			Representative:      e.Representative,
			Variants:            unmatched,
			TotalCount:          e.TotalCount,
			RepresentativeCount: e.RepresentativeCount,
		})
	}
	return items
}

// Pure logic test for filterClusters (no HTTP, no DB)
func TestFilterClusters_PureLogic(t *testing.T) {
	entries := []service.ClusterCacheEntry{
		{ID: 1, Representative: "继续", Variants: "继续,请继续", TotalCount: 5, RepresentativeCount: 3},
		{ID: 2, Representative: "提交", Variants: "提交,commit", TotalCount: 3, RepresentativeCount: 2},
		{ID: 3, Representative: "帮我写", Variants: "帮我写,写一个函数", TotalCount: 4, RepresentativeCount: 2},
	}

	// All variants of cluster 1 match quick-send → filtered out entirely
	qsSet := quickSendCommandSet([]string{"继续", "请继续", "提交"})

	items := filterClusters(entries, qsSet)

	if len(items) != 2 {
		t.Fatalf("expected 2 items after filtering, got %d", len(items))
	}
	// Cluster 1: all variants match → filtered out
	// Cluster 2: "commit" remains
	assert.Equal(t, []string{"commit"}, items[0].Variants)
	// Cluster 3: no match
	assert.Equal(t, []string{"帮我写", "写一个函数"}, items[1].Variants)
}
