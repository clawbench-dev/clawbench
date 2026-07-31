package rag

import (
	"math"
	"sort"
	"strings"
)

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
func ClusterMessages(stats []MessageStat, simFn SimilarityFunc, threshold float64) []MessageCluster {
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

	// Compare all pairs O(n²)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if simFn(stats[i].Text, stats[j].Text) >= threshold {
				union(i, j)
			}
		}
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

		// Tokenize using SegmentText (will be replaced with SegmentTokens in Task 4)
		tokensA := strings.Fields(SegmentText(textA))
		tokensB := strings.Fields(SegmentText(textB))

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
