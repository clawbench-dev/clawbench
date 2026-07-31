package rag

import (
	"math"
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
