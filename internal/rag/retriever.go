package rag

import (
	"context"
	"sort"

	"github.com/google/uuid"
)

// Retriever performs hybrid search with reciprocal rank fusion.
type Retriever struct {
	Store    *Store
	Embedder Embedder
	// DenseK and SparseK are the per-side retrieval depths.
	DenseK, SparseK int
	// TopN is how many results the retriever returns after fusion.
	TopN int
	// RRFK is the RRF constant (Cormack et al. 2009 recommend k=60).
	RRFK float32
}

// Embedder is anything that can embed strings.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// NewRetriever returns a retriever with sensible defaults.
func NewRetriever(store *Store, emb Embedder) *Retriever {
	return &Retriever{
		Store:    store,
		Embedder: emb,
		DenseK:   20,
		SparseK:  20,
		TopN:     8,
		RRFK:     60,
	}
}

// Hybrid runs the dense + sparse query and returns the fused top-N.
//
// Reciprocal Rank Fusion: for each ranked list L and document d,
//
//	score_L(d) = 1 / (RRFK + rank_L(d))
//
// The fused score is the sum across lists. We tag the returned Hit with both
// component scores for explainability.
func (r *Retriever) Hybrid(ctx context.Context, query string) ([]Hit, error) {
	embeddings, err := r.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	dense, derr := r.Store.DenseSearch(ctx, embeddings[0], r.DenseK)
	sparse, serr := r.Store.SparseSearch(ctx, query, r.SparseK)

	// If either side failed, we still degrade gracefully on the other.
	if derr != nil && serr != nil {
		return nil, derr
	}

	type acc struct {
		hit   Hit
		score float32
	}
	byID := map[uuid.UUID]*acc{}

	if derr == nil {
		for i, h := range dense {
			h.SparseRank = 0
			cur, ok := byID[h.ChunkID]
			if !ok {
				cur = &acc{hit: h}
				byID[h.ChunkID] = cur
			} else {
				cur.hit.DenseScore = h.DenseScore
			}
			cur.score += 1.0 / (r.RRFK + float32(i+1))
		}
	}
	if serr == nil {
		for i, h := range sparse {
			cur, ok := byID[h.ChunkID]
			if !ok {
				cur = &acc{hit: h}
				byID[h.ChunkID] = cur
			} else {
				cur.hit.SparseRank = h.SparseRank
			}
			cur.score += 1.0 / (r.RRFK + float32(i+1))
		}
	}

	out := make([]Hit, 0, len(byID))
	for _, a := range byID {
		a.hit.FusedScore = a.score
		out = append(out, a.hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FusedScore > out[j].FusedScore })

	if len(out) > r.TopN {
		out = out[:r.TopN]
	}
	return out, nil
}
