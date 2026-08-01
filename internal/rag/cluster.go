package rag

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
)

// ClusterProgressCallback is called periodically during cluster computation
// to report fine-grained progress. done is the number of items completed,
// total is the total number of items to process.
type ClusterProgressCallback func(done, total int)

// MessageStat is a unique user message with its occurrence count.
// Defined in rag package to avoid rag→service data-type coupling.
type MessageStat struct {
	Text  string
	Count int
}

// MessageCluster represents a group of similar user messages.
type MessageCluster struct {
	Representative      string   `json:"representative"`       // most frequent variant
	Variants            []string `json:"variants"`             // all exact variants in cluster
	TotalCount          int      `json:"total_count"`          // sum of counts across variants
	RepresentativeCount int      `json:"representative_count"` // count of representative variant
}

// SimilarityFunc returns similarity (0-1) between two texts.
type SimilarityFunc func(textA, textB string) float64

// ClusterMessages groups similar messages using Union-Find.
// If simFn is nil or threshold <= 0, each stat is its own cluster (exact dedup only).
// progressCb is called periodically during O(n²) comparison; nil means no progress reporting.
func ClusterMessages(stats []MessageStat, simFn SimilarityFunc, threshold float64, progressCb ClusterProgressCallback) []MessageCluster {
	if len(stats) == 0 {
		return nil
	}

	// Exact dedup only when simFn is nil or threshold <= 0
	if simFn == nil || threshold <= 0 {
		clusters := make([]MessageCluster, 0, len(stats))
		for _, s := range stats {
			clusters = append(clusters, MessageCluster{
				Representative:      s.Text,
				Variants:            []string{s.Text},
				TotalCount:          s.Count,
				RepresentativeCount: s.Count,
			})
		}
		sortByTotalCount(clusters)
		return clusters
	}

	n := len(stats)
	parent := make([]int, n)
	rank := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	// Union-Find with path compression + union-by-rank
	find := func(x int) int {
		root := x
		for parent[root] != root {
			root = parent[root]
		}
		// Path compression
		for parent[x] != root {
			next := parent[x]
			parent[x] = root
			x = next
		}
		return root
	}

	union := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx == ry {
			return
		}
		if rank[rx] < rank[ry] {
			parent[rx] = ry
		} else if rank[rx] > rank[ry] {
			parent[ry] = rx
		} else {
			parent[ry] = rx
			rank[rx]++
		}
	}

	// Compare all pairs O(n²) — report progress every ~1% completion
	totalPairs := n * (n - 1) / 2
	pairIdx := 0
	nextReport := 0
	reportInterval := 0
	if totalPairs > 0 && progressCb != nil {
		reportInterval = totalPairs / 100 // ~1% granularity
		if reportInterval < 1 {
			reportInterval = 1
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if simFn(stats[i].Text, stats[j].Text) >= threshold {
				union(i, j)
			}
			pairIdx++
			if progressCb != nil && reportInterval > 0 && pairIdx >= nextReport+reportInterval {
				nextReport = pairIdx
				progressCb(pairIdx, totalPairs)
			}
		}
	}
	if progressCb != nil && pairIdx > 0 {
		progressCb(pairIdx, totalPairs) // final 100%
	}

	// Group by root
	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	clusters := make([]MessageCluster, 0, len(groups))
	for _, indices := range groups {
		// Pick most frequent text as representative
		bestIdx := indices[0]
		for _, idx := range indices {
			if stats[idx].Count > stats[bestIdx].Count {
				bestIdx = idx
			}
		}

		variants := make([]string, 0, len(indices))
		totalCount := 0
		for _, idx := range indices {
			variants = append(variants, stats[idx].Text)
			totalCount += stats[idx].Count
		}

		clusters = append(clusters, MessageCluster{
			Representative:      stats[bestIdx].Text,
			Variants:            variants,
			TotalCount:          totalCount,
			RepresentativeCount: stats[bestIdx].Count,
		})
	}

	sortByTotalCount(clusters)
	return clusters
}

func sortByTotalCount(clusters []MessageCluster) {
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].TotalCount > clusters[j].TotalCount
	})
}

// sorensenDiceWithLengthPenalty returns a SimilarityFunc that applies
// a length-ratio penalty before computing Sørensen-Dice coefficient.
// If min(lenA,lenB)/max(lenA,lenB) < minLengthRatio, similarity is 0.
func sorensenDiceWithLengthPenalty(minLengthRatio float64) SimilarityFunc {
	return func(textA, textB string) float64 {
		lenA := float64(len(textA))
		lenB := float64(len(textB))

		if lenA == 0 || lenB == 0 {
			return 0
		}

		// Length penalty: reject if ratio too small
		ratio := math.Min(lenA, lenB) / math.Max(lenA, lenB)
		if ratio < minLengthRatio {
			return 0
		}

		// Tokenize using SegmentTokens (raw token slice for set-based similarity)
		tokensA := SegmentTokens(textA)
		tokensB := SegmentTokens(textB)

		if len(tokensA) == 0 || len(tokensB) == 0 {
			return 0
		}

		// Build sets
		setA := make(map[string]struct{}, len(tokensA))
		for _, t := range tokensA {
			setA[t] = struct{}{}
		}
		setB := make(map[string]struct{}, len(tokensB))
		for _, t := range tokensB {
			setB[t] = struct{}{}
		}

		// Count intersection
		intersection := 0
		for t := range setA {
			if _, ok := setB[t]; ok {
				intersection++
			}
		}

		// Sørensen-Dice: 2|intersection| / (|A| + |B|)
		return float64(2*intersection) / float64(len(setA)+len(setB))
	}
}

// VectorSimilarityMatrix batch-embeds texts and returns a pairwise
// cosine similarity lookup function. Returns (lookupFn, error).
func VectorSimilarityMatrix(ctx context.Context, embedder *EmbeddingClient, texts []string, progressCb ClusterProgressCallback) (func(i, j int) float64, error) {
	embeddings := embedInSubBatches(ctx, embedder, texts, progressCb)

	// Count how many embeddings actually succeeded
	successCount := 0
	for _, emb := range embeddings {
		if emb != nil {
			successCount++
		}
	}
	if successCount == 0 {
		return nil, fmt.Errorf("all %d embedding sub-batches failed", len(texts))
	}

	// Normalize all vectors — allocate inner slices (C3 fix: nil-slice bug)
	normalized := make([][]float64, len(embeddings))
	for i, emb := range embeddings {
		if emb == nil {
			continue // skip failed embeddings
		}
		// C3 FIX: allocate inner slice!
		normalized[i] = make([]float64, len(emb))

		// Compute L2 norm
		var norm float64
		for _, v := range emb {
			norm += v * v
		}
		norm = math.Sqrt(norm)
		if norm == 0 {
			continue // zero vector → skip
		}
		for j, v := range emb {
			normalized[i][j] = v / norm
		}
	}

	// Lookup function: dot product of normalized vectors = cosine similarity
	lookup := func(i, j int) float64 {
		a := normalized[i]
		b := normalized[j]
		if a == nil || b == nil {
			return 0 // one or both embeddings failed
		}
		var dot float64
		for k := 0; k < len(a); k++ {
			dot += a[k] * b[k]
		}
		return dot
	}

	return lookup, nil
}

// embedInSubBatches calls EmbedBatch in smaller sub-batches to avoid timeouts.
// Uses embedSubBatchSize (= 5, same as RAG indexer).
// Failed sub-batches leave nil entries in results (graceful degradation).
// Returns all embeddings in the same order as input texts.
func embedInSubBatches(ctx context.Context, embedder *EmbeddingClient, texts []string, progressCb ClusterProgressCallback) [][]float64 {
	results := make([][]float64, len(texts))
	for i := 0; i < len(texts); i += embedSubBatchSize {
		end := i + embedSubBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		subBatch := texts[i:end]
		embeddings, err := embedder.EmbedBatch(ctx, subBatch)
		if err != nil {
			slog.Warn("rag: cluster sub-batch embedding failed",
				slog.Int("from", i), slog.Int("to", end),
				slog.String("err", err.Error()))
			// nil entries left in results — caller treats as no embedding
			continue
		}
		for j, emb := range embeddings {
			results[i+j] = emb
		}
		// Report embedding progress
		if progressCb != nil {
			done := min(end, len(texts))
			progressCb(done, len(texts))
		}
		// Check context cancellation between sub-batches
		if ctx.Err() != nil {
			break
		}
	}
	return results
}

// ClusterMessagesWithEmbeddings clusters messages using best available method.
// Returns (clusters, mode) where mode is "vector" | "fts" | "exact".
// vectorThreshold is used when embeddings are available (semantic similarity).
// ftsThreshold is used for FTS fallback (token-based, needs higher threshold for short texts).
// progressCb reports fine-grained progress during embedding and comparison phases.
func ClusterMessagesWithEmbeddings(ctx context.Context, stats []MessageStat, embedder *EmbeddingClient, vectorThreshold, ftsThreshold float64, progressCb ClusterProgressCallback) ([]MessageCluster, string) {
	// Try vector mode: if embedder != nil and EmbedderHealthy()
	// Embedding phase: pct 0-30, Comparison phase: pct 30-100 (of overall clustering)
	if embedder != nil && EmbedderHealthy() {
		// Embedding sub-progress: 0→30% of overall clustering
		embedCb := func(done, total int) {
			if progressCb != nil {
				subPct := 0
				if total > 0 {
					subPct = done * 30 / total
				}
				progressCb(subPct, 100)
			}
		}
		lookup, err := VectorSimilarityMatrix(ctx, embedder, extractTexts(stats), embedCb)
		if err == nil {
			// Comparison sub-progress: 30→100% of overall clustering
			compCb := func(done, total int) {
				if progressCb != nil {
					subPct := 30
					if total > 0 {
						subPct = 30 + done*70/total
					}
					progressCb(subPct, 100)
				}
			}
			clusters := ClusterMessages(stats, simFnFromLookup(lookup, stats), vectorThreshold, compCb)
			return clusters, "vector"
		}
		// Vector failed → fall back to FTS
		slog.Warn("rag: vector similarity failed, falling back to FTS",
			slog.String("err", err.Error()))
	}

	// FTS fallback — always available (token-based Sørensen-Dice, no embedding needed)
	// Used when: no embedder, embedder unhealthy, or vector failed
	simFn := sorensenDiceWithLengthPenalty(0.5)
	clusters := ClusterMessages(stats, simFn, ftsThreshold, progressCb)
	return clusters, "fts"
}

// simFnFromLookup creates a SimilarityFunc from a pairwise similarity lookup and stats.
func simFnFromLookup(lookup func(i, j int) float64, stats []MessageStat) SimilarityFunc {
	textToIdx := make(map[string]int, len(stats))
	for i, s := range stats {
		textToIdx[s.Text] = i
	}
	return func(textA, textB string) float64 {
		iA, okA := textToIdx[textA]
		iB, okB := textToIdx[textB]
		if !okA || !okB {
			return 0
		}
		return lookup(iA, iB)
	}
}

// extractTexts returns just the Text field from each MessageStat.
func extractTexts(stats []MessageStat) []string {
	texts := make([]string, len(stats))
	for i, s := range stats {
		texts[i] = s.Text
	}
	return texts
}
