package rag

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Initialize segmenter for Sørensen-Dice tests that need Chinese tokenization
	_ = InitSegmenter()
	os.Exit(m.Run())
}

func TestClusterMessages_ExactDedupOnly(t *testing.T) {
	// nil simFn -> each stat is its own cluster
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "hello", Count: 2},
	}

	clusters := ClusterMessages(stats, nil, 0.65)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	// Each cluster should have exactly one variant
	for _, c := range clusters {
		if len(c.Variants) != 1 {
			t.Errorf("expected 1 variant, got %d for representative %q", len(c.Variants), c.Representative)
		}
	}
}

func TestClusterMessages_ThresholdZero(t *testing.T) {
	// threshold <= 0 -> same as nil simFn, exact dedup only
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "你好啊", Count: 2},
	}

	mockSim := func(a, b string) float64 { return 0.99 }
	clusters := ClusterMessages(stats, mockSim, 0)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters with threshold 0, got %d", len(clusters))
	}
}

func TestClusterMessages_SimilarGrouping(t *testing.T) {
	// mock simFn that returns 0.67 for "你好"/"你好啊" -> cluster at threshold 0.65
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "你好啊", Count: 2},
		{Text: "hello", Count: 1},
	}

	mockSim := func(a, b string) float64 {
		if (a == "你好" && b == "你好啊") || (a == "你好啊" && b == "你好") {
			return 0.67
		}
		return 0
	}

	clusters := ClusterMessages(stats, mockSim, 0.65)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	// Find the cluster containing "你好"
	var chineseCluster *MessageCluster
	for i := range clusters {
		if clusters[i].Representative == "你好" {
			chineseCluster = &clusters[i]
			break
		}
	}

	if chineseCluster == nil {
		t.Fatal("expected cluster with representative '你好'")
	}

	if len(chineseCluster.Variants) != 2 {
		t.Errorf("expected 2 variants, got %d", len(chineseCluster.Variants))
	}

	if chineseCluster.TotalCount != 5 {
		t.Errorf("expected TotalCount=5, got %d", chineseCluster.TotalCount)
	}

	if chineseCluster.RepresentativeCount != 3 {
		t.Errorf("expected RepresentativeCount=3, got %d", chineseCluster.RepresentativeCount)
	}
}

func TestClusterMessages_NoClusterBelowThreshold(t *testing.T) {
	// similarity below threshold -> separate clusters
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "你好啊", Count: 2},
	}

	mockSim := func(a, b string) float64 { return 0.5 }

	clusters := ClusterMessages(stats, mockSim, 0.65)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters (similarity below threshold), got %d", len(clusters))
	}
}

func TestClusterMessages_RepresentativeIsMostFrequent(t *testing.T) {
	// verify representative is the variant with highest count
	stats := []MessageStat{
		{Text: "好的啊", Count: 1},
		{Text: "好的", Count: 10},
		{Text: "好哒", Count: 5},
	}

	// All similar -> one cluster
	mockSim := func(a, b string) float64 { return 0.9 }

	clusters := ClusterMessages(stats, mockSim, 0.8)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}

	if clusters[0].Representative != "好的" {
		t.Errorf("expected representative '好的' (count=10), got %q", clusters[0].Representative)
	}

	if clusters[0].RepresentativeCount != 10 {
		t.Errorf("expected RepresentativeCount=10, got %d", clusters[0].RepresentativeCount)
	}

	if clusters[0].TotalCount != 16 {
		t.Errorf("expected TotalCount=16, got %d", clusters[0].TotalCount)
	}
}

func TestClusterMessages_EmptyInput(t *testing.T) {
	clusters := ClusterMessages(nil, nil, 0.65)
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters for nil input, got %d", len(clusters))
	}

	clusters = ClusterMessages([]MessageStat{}, nil, 0.65)
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters for empty input, got %d", len(clusters))
	}
}

func TestSorensenDiceWithLengthPenalty_SimilarShort(t *testing.T) {
	// "你好" and "你好啊" -> score > 0.65 (should cluster)
	simFn := sorensenDiceWithLengthPenalty(0.4)
	score := simFn("你好", "你好啊")
	if score <= 0.65 {
		t.Errorf("expected score > 0.65 for similar short texts, got %.4f", score)
	}
}

func TestSorensenDiceWithLengthPenalty_LengthRatioRejects(t *testing.T) {
	// short vs long -> score = 0 (length penalty kicks in)
	simFn := sorensenDiceWithLengthPenalty(0.4)
	score := simFn("好的", "好的，我收到了你的邮件并且已经处理完毕")
	if score != 0 {
		t.Errorf("expected score = 0 when length ratio below threshold, got %.4f", score)
	}
}

func TestSorensenDiceWithLengthPenalty_ExactSame(t *testing.T) {
	// same text -> score = 1.0
	simFn := sorensenDiceWithLengthPenalty(0.4)
	score := simFn("你好世界", "你好世界")
	if math.Abs(score-1.0) > 0.001 {
		t.Errorf("expected score = 1.0 for identical text, got %.4f", score)
	}
}

func TestSorensenDiceWithLengthPenalty_NoOverlap(t *testing.T) {
	// completely different texts -> score ≈ 0
	simFn := sorensenDiceWithLengthPenalty(0.4)
	score := simFn("你好世界", "abcdefg")
	if score > 0.01 {
		t.Errorf("expected score ≈ 0 for no overlap, got %.4f", score)
	}
}

func TestVectorSimilarityMatrix_Basic(t *testing.T) {
	// Test normalization and cosine similarity computation with known embeddings.
	// Use a mock HTTP server that returns pre-set embedding vectors.
	//
	// Vectors:
	//   [1,0] → normalized: [1,0]
	//   [1,1] → normalized: [0.7071, 0.7071]
	//   [0,1] → normalized: [0,1]
	//
	// Cosine similarities:
	//   [1,0] vs [1,1] = 0.7071
	//   [1,0] vs [0,1] = 0
	//   [1,1] vs [0,1] = 0.7071

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return fixed embeddings for any input
		w.Write([]byte(`{
			"data": [
				{"embedding": [1.0, 0.0], "index": 0},
				{"embedding": [1.0, 1.0], "index": 1},
				{"embedding": [0.0, 1.0], "index": 2}
			]
		}`))
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")
	ctx := context.Background()

	lookup, err := VectorSimilarityMatrix(ctx, embedder, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// [1,0] vs [1,1] = dot(normalized) = 0.7071
	sim01 := lookup(0, 1)
	if math.Abs(sim01-0.7071) > 0.01 {
		t.Errorf("sim(0,1) expected ~0.7071, got %.4f", sim01)
	}

	// [1,0] vs [0,1] = 0
	sim02 := lookup(0, 2)
	if math.Abs(sim02) > 0.01 {
		t.Errorf("sim(0,2) expected 0, got %.4f", sim02)
	}

	// [1,1] vs [0,1] = 0.7071
	sim12 := lookup(1, 2)
	if math.Abs(sim12-0.7071) > 0.01 {
		t.Errorf("sim(1,2) expected ~0.7071, got %.4f", sim12)
	}

	// Self-similarity should be 1.0
	sim00 := lookup(0, 0)
	if math.Abs(sim00-1.0) > 0.001 {
		t.Errorf("sim(0,0) expected 1.0, got %.4f", sim00)
	}
}

func TestVectorSimilarityMatrix_NilEmbeddingSkipped(t *testing.T) {
	// When a sub-batch fails, that embedding is nil.
	// The C3 fix ensures inner slices are allocated only for non-nil embeddings.
	// Nil entries should produce similarity 0 (dot product of zero-length vectors).
	//
	// We'll use a server that returns empty data for the second batch (3 texts, batch size 5)
	// Actually, easier: test with a server that fails on second request,
	// but since batch size = 5 and we have 3 texts, they all go in one batch.
	// Instead, let's test with 7 texts (2 batches) where the second batch fails.

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First batch (5 texts) succeeds
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"data": [
					{"embedding": [1.0, 0.0], "index": 0},
					{"embedding": [1.0, 0.0], "index": 1},
					{"embedding": [1.0, 0.0], "index": 2},
					{"embedding": [1.0, 0.0], "index": 3},
					{"embedding": [1.0, 0.0], "index": 4}
				]
			}`))
		} else {
			// Second batch (2 texts) fails → nil embeddings for indices 5,6
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "server error"}`))
		}
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")
	ctx := context.Background()

	texts := make([]string, 7)
	for i := range texts {
		texts[i] = "text"
	}

	lookup, err := VectorSimilarityMatrix(ctx, embedder, texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Indices 0-4 should have similarity 1.0 with each other
	sim00 := lookup(0, 0)
	if math.Abs(sim00-1.0) > 0.001 {
		t.Errorf("sim(0,0) expected 1.0, got %.4f", sim00)
	}

	// Indices 5 and 6 have nil embeddings → similarity should be 0
	sim56 := lookup(5, 6)
	if sim56 != 0 {
		t.Errorf("sim(5,6) expected 0 (nil embeddings), got %.4f", sim56)
	}

	// 0 vs 5 also should be 0 (one side nil)
	sim05 := lookup(0, 5)
	if sim05 != 0 {
		t.Errorf("sim(0,5) expected 0 (one nil embedding), got %.4f", sim05)
	}
}

func TestVectorSimilarityMatrix_AllEmbeddingsFailed(t *testing.T) {
	// When ALL embeddings fail, VectorSimilarityMatrix should return an error
	// so the caller falls back to FTS mode instead of misleadingly reporting "vector".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")
	ctx := context.Background()

	_, err := VectorSimilarityMatrix(ctx, embedder, []string{"a", "b", "c"})
	if err == nil {
		t.Error("expected error when all embeddings fail, got nil")
	}
}

func TestClusterMessagesWithEmbeddings_FTSFallbackNoEmbedder(t *testing.T) {
	// nil embedder → mode="fts" (FTS is always available, no embedding needed)
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "hello", Count: 2},
	}

	clusters, mode := ClusterMessagesWithEmbeddings(context.Background(), stats, nil, 0.65)

	if mode != "fts" {
		t.Errorf("expected mode 'fts', got %q", mode)
	}
	// "你好" and "hello" have no shared tokens → each in own cluster
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
}

func TestClusterMessagesWithEmbeddings_FTSFallback(t *testing.T) {
	// embedder != nil but EmbedderHealthy() = false → falls back to FTS
	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "你好啊", Count: 2},
	}

	// Make embedder unhealthy
	SetEmbedderHealthy(false)

	// Use a real embedder struct but it won't be called since healthy=false
	embedder := &EmbeddingClient{}

	clusters, mode := ClusterMessagesWithEmbeddings(context.Background(), stats, embedder, 0.65)

	if mode != "fts" {
		t.Errorf("expected mode 'fts', got %q", mode)
	}
	// With sorensenDiceWithLengthPenalty(0.5), "你好" and "你好啊" should cluster
	// (they are similar enough and length ratio passes)
	if len(clusters) < 1 {
		t.Fatalf("expected at least 1 cluster, got %d", len(clusters))
	}
}

func TestClusterMessagesWithEmbeddings_VectorMode(t *testing.T) {
	// embedder != nil and EmbedderHealthy() = true → uses vector mode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// All texts get the same embedding → all cluster together
		w.Write([]byte(`{
			"data": [
				{"embedding": [1.0, 1.0], "index": 0},
				{"embedding": [1.0, 1.0], "index": 1},
				{"embedding": [1.0, 1.0], "index": 2}
			]
		}`))
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")
	SetEmbedderHealthy(true)

	stats := []MessageStat{
		{Text: "你好", Count: 3},
		{Text: "你好啊", Count: 2},
		{Text: "hello", Count: 1},
	}

	clusters, mode := ClusterMessagesWithEmbeddings(context.Background(), stats, embedder, 0.65)

	if mode != "vector" {
		t.Errorf("expected mode 'vector', got %q", mode)
	}
	// All have identical normalized vectors → cosine similarity = 1.0 → all in one cluster
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (all identical embeddings), got %d", len(clusters))
	}
	if clusters[0].TotalCount != 6 {
		t.Errorf("expected TotalCount=6, got %d", clusters[0].TotalCount)
	}
}
