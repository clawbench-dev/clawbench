package kb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/rag"
)

// Search modes returned in SearchResult.Mode and accepted in SearchParams.Mode.
const (
	SearchModeHybrid = "hybrid"
	SearchModeVector = "vector"
	SearchModeFTS    = "fts"
)

// SearchParams describes a knowledge base query.
type SearchParams struct {
	Query string `json:"q"`
	// CollectionIDs narrows the search. Empty means every collection visible
	// from ProjectID.
	CollectionIDs []int64 `json:"collection_ids,omitempty"`
	// ProjectID scopes the implicit collection list. 0 means global only.
	ProjectID int64 `json:"-"`
	Limit     int   `json:"limit,omitempty"`
	PoolSize  int   `json:"-"`
	// Mode is "hybrid" (default), "fts" or "vector".
	Mode string `json:"mode,omitempty"`
	// AutoInjectOnly restricts the implicit collection list to collections
	// that opted into automatic injection.
	AutoInjectOnly bool `json:"-"`
}

// Search runs a keyword, semantic or hybrid query.
func Search(ctx context.Context, params SearchParams) (*SearchResult, error) { //nolint:gocognit,gocyclo // multi-mode search with fallback
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return &SearchResult{Mode: SearchModeFTS}, nil
	}

	ids := params.CollectionIDs
	if len(ids) == 0 {
		var err error
		if params.AutoInjectOnly {
			ids, err = st.ListInjectCollectionIDs(params.ProjectID)
		} else {
			ids, err = st.ListCollectionIDs(params.ProjectID)
		}
		if err != nil {
			return nil, err
		}
	} else {
		allowed, err := st.ListCollectionIDs(params.ProjectID)
		if err != nil {
			return nil, err
		}
		allowedSet := make(map[int64]struct{}, len(allowed))
		for _, id := range allowed {
			allowedSet[id] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := allowedSet[id]; !ok {
				return nil, ErrAccessDenied
			}
		}
	}
	if len(ids) == 0 {
		return &SearchResult{Mode: SearchModeFTS}, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	pool := params.PoolSize
	if pool <= 0 {
		pool = 20
	}

	mode := strings.ToLower(strings.TrimSpace(params.Mode))
	emb := currentEmbedder()
	vectorReady := emb != nil && st.HasVecData()

	var hits []SearchHit
	var err error
	switch mode {
	case SearchModeFTS:
		hits, err = st.SearchFTS(query, limit, ids)
		mode = SearchModeFTS
	case SearchModeVector:
		if !vectorReady {
			hits, err = st.SearchFTS(query, limit, ids)
			mode = SearchModeFTS
			break
		}
		var vec []float64
		vec, err = emb.Embed(ctx, query)
		if err != nil {
			hits, err = st.SearchFTS(query, limit, ids)
			mode = SearchModeFTS
			break
		}
		hits, err = st.SearchVector(vec, limit, ids)
		mode = SearchModeVector
	default:
		if !vectorReady {
			hits, err = st.SearchFTS(query, limit, ids)
			mode = SearchModeFTS
			break
		}
		var vec []float64
		vec, err = emb.Embed(ctx, query)
		if err != nil {
			// Embedding is optional: a dead endpoint must not break search.
			hits, err = st.SearchFTS(query, limit, ids)
			mode = SearchModeFTS
			break
		}
		var vecHits, ftsHits []SearchHit
		vecHits, _ = st.SearchVector(vec, pool, ids)
		ftsHits, _ = st.SearchFTS(query, pool, ids)
		hits = fuseRRF(vecHits, ftsHits, limit)
		mode = SearchModeHybrid
	}
	if err != nil {
		return nil, err
	}

	for i := range hits {
		positions := rag.TextMatchPositions(query, hits[i].ChunkText)
		if len(positions) > 0 {
			hits[i].MatchPositions = make([]MatchRange, len(positions))
			for j, p := range positions {
				hits[i].MatchPositions[j] = MatchRange{Start: p.Start, End: p.End}
			}
		}
	}
	return &SearchResult{Results: hits, Total: len(hits), Mode: mode}, nil
}

// fuseRRF merges two ranked lists with Reciprocal Rank Fusion.
func fuseRRF(vecHits, ftsHits []SearchHit, limit int) []SearchHit {
	const k = 60
	type entry struct {
		hit   SearchHit
		score float64
	}
	byID := map[int64]*entry{}
	add := func(hits []SearchHit, isVector bool) {
		for rank, h := range hits {
			e, ok := byID[h.ChunkID]
			if !ok {
				e = &entry{hit: h}
				byID[h.ChunkID] = e
			}
			if isVector {
				e.hit.VectorScore = h.VectorScore
			} else {
				e.hit.FTSScore = h.FTSScore
			}
			e.score += 1.0 / float64(k+rank+1)
		}
	}
	add(vecHits, true)
	add(ftsHits, false)

	entries := make([]*entry, 0, len(byID))
	for _, e := range byID {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })
	if limit > len(entries) {
		limit = len(entries)
	}
	out := make([]SearchHit, limit)
	for i, e := range entries[:limit] {
		e.hit.Score = e.score
		out[i] = e.hit
	}
	return out
}

// BuildContext retrieves snippets for automatic injection into one user turn.
// It is deliberately bounded: a 1.5s budget, a small top-N, and a per-document
// cap so one long file cannot crowd out the rest. Failures return empty, never
// an error — a knowledge base outage must not block chat.
func BuildContext(ctx context.Context, query string, projectID int64, cfg model.KBConfig) (string, []Ref) { //nolint:gocyclo // bounded retrieval with per-document and per-character caps
	if !cfg.AutoInject {
		return "", nil
	}
	q := strings.TrimSpace(query)
	if utf8.RuneCountInString(q) < 2 || strings.HasPrefix(q, "/") {
		return "", nil
	}

	topN := cfg.InjectTopN
	if topN <= 0 {
		topN = 4
	}
	maxChars := cfg.InjectMaxChars
	if maxChars <= 0 {
		maxChars = 2000
	}

	searchCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	res, err := Search(searchCtx, SearchParams{
		Query:          q,
		ProjectID:      projectID,
		Limit:          topN * 2,
		PoolSize:       20,
		AutoInjectOnly: true,
	})
	if err != nil || res == nil || len(res.Results) == 0 {
		return "", nil
	}

	var refs []Ref
	perDoc := map[int64]int{}
	total := 0
	for _, h := range res.Results {
		if len(refs) >= topN {
			break
		}
		// A vector-only hit below the similarity floor is likely unrelated.
		if h.VectorScore > 0 && h.FTSScore == 0 && h.VectorScore < cfg.InjectMinSimilarity {
			continue
		}
		if perDoc[h.DocumentID] >= 2 {
			continue
		}
		text := strings.TrimSpace(h.ChunkText)
		if text == "" {
			continue
		}
		if total+len(text) > maxChars {
			remain := maxChars - total
			if remain < 200 {
				break
			}
			text = truncateUTF8(text, remain)
		}
		perDoc[h.DocumentID]++
		total += len(text)
		refs = append(refs, Ref{
			DocumentID:     h.DocumentID,
			DocumentTitle:  h.DocumentTitle,
			CollectionID:   h.CollectionID,
			CollectionName: h.CollectionName,
			Location:       h.Location,
			Text:           text,
		})
	}
	if len(refs) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("[知识库参考]\n")
	b.WriteString("以下是系统从用户的知识库中检索到的片段，可能不完整或过时。请优先依据这些内容回答；如果与当前问题无关，请忽略。引用时请注明来源文件名。\n\n")
	for i, r := range refs {
		fmt.Fprintf(&b, "%d. 《%s》", i+1, r.DocumentTitle)
		if r.Location != "" {
			fmt.Fprintf(&b, " · %s", r.Location)
		}
		fmt.Fprintf(&b, "（知识库：%s）\n%s\n\n", r.CollectionName, r.Text)
	}
	b.WriteString("[/知识库参考]")
	return b.String(), refs
}
