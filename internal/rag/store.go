package rag

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Document is the top-level ingest unit.
type Document struct {
	ID     uuid.UUID
	Source string
	Title  string
	URL    string
}

// Hit is a retrieved chunk with retrieval scores.
type Hit struct {
	ChunkID    uuid.UUID
	DocumentID uuid.UUID
	Source     string
	Title      string
	URL        string
	Content    string
	DenseScore float32
	SparseRank int
	FusedScore float32
}

// Store is the pgvector-backed corpus store.
type Store struct {
	pool *pgxpool.Pool
	dim  int
}

// NewStore returns a store bound to the connection pool. dim is the embedding
// vector size (e.g. 768 for nomic-embed-text, 1536 for text-embedding-3-small).
func NewStore(pool *pgxpool.Pool, dim int) *Store {
	return &Store{pool: pool, dim: dim}
}

// EnsureSchema creates the tables and indexes if missing. Safe to call on every boot.
func (s *Store) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector;`,
		`CREATE TABLE IF NOT EXISTS documents (
			id          uuid PRIMARY KEY,
			source      text NOT NULL,
			title       text NOT NULL,
			url         text,
			ingested_at timestamptz NOT NULL DEFAULT now()
		);`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chunks (
			id          uuid PRIMARY KEY,
			document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			ordinal     int  NOT NULL,
			content     text NOT NULL,
			tsv         tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
			embedding   vector(%d) NOT NULL,
			metadata    jsonb NOT NULL DEFAULT '{}'::jsonb
		);`, s.dim),
		`CREATE INDEX IF NOT EXISTS chunks_tsv_idx       ON chunks USING gin(tsv);`,
		`CREATE INDEX IF NOT EXISTS chunks_doc_idx       ON chunks(document_id);`,
		// HNSW on cosine distance. We CREATE INDEX CONCURRENTLY in real life,
		// but inside a function it's fine on the small dev set.
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS chunks_embedding_idx ON chunks USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);`),
		// Idempotency table used by the executor.
		`CREATE TABLE IF NOT EXISTS processed_events (
			idempotency_key text PRIMARY KEY,
			processed_at    timestamptz NOT NULL DEFAULT now(),
			producer        text NOT NULL,
			receipt         text
		);`,
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("schema: %s: %w", firstLine(q), err)
		}
	}
	return nil
}

// IngestDocument inserts a document and its chunks. Existing chunks for the
// same document_id are deleted first to make ingestion idempotent.
func (s *Store) IngestDocument(ctx context.Context, doc Document, chunks []Chunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("chunks (%d) and embeddings (%d) length mismatch", len(chunks), len(embeddings))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`INSERT INTO documents (id, source, title, url)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET source = EXCLUDED.source, title = EXCLUDED.title, url = EXCLUDED.url`,
		doc.ID, doc.Source, doc.Title, doc.URL,
	); err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, doc.ID); err != nil {
		return fmt.Errorf("clear chunks: %w", err)
	}
	for i, c := range chunks {
		_, err := tx.Exec(ctx,
			`INSERT INTO chunks (id, document_id, ordinal, content, embedding)
			 VALUES ($1, $2, $3, $4, $5::vector)`,
			uuid.New(), doc.ID, c.Ordinal, c.Content, vectorText(embeddings[i]),
		)
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", i, err)
		}
	}
	return tx.Commit(ctx)
}

// DenseSearch returns the top-K nearest chunks by cosine distance.
func (s *Store) DenseSearch(ctx context.Context, query []float32, k int) ([]Hit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.document_id, d.source, d.title, COALESCE(d.url, ''), c.content,
		       1 - (c.embedding <=> $1::vector) AS score
		FROM chunks c JOIN documents d ON d.id = c.document_id
		ORDER BY c.embedding <=> $1::vector
		LIMIT $2`,
		vectorText(query), k,
	)
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ChunkID, &h.DocumentID, &h.Source, &h.Title, &h.URL, &h.Content, &h.DenseScore); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SparseSearch returns the top-K chunks by BM25-style ts_rank_cd.
func (s *Store) SparseSearch(ctx context.Context, query string, k int) ([]Hit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.document_id, d.source, d.title, COALESCE(d.url, ''), c.content,
		       ts_rank_cd(c.tsv, plainto_tsquery('english', $1)) AS rnk
		FROM chunks c JOIN documents d ON d.id = c.document_id
		WHERE c.tsv @@ plainto_tsquery('english', $1)
		ORDER BY rnk DESC
		LIMIT $2`,
		query, k,
	)
	if err != nil {
		return nil, fmt.Errorf("sparse search: %w", err)
	}
	defer rows.Close()
	var out []Hit
	rank := 0
	for rows.Next() {
		var h Hit
		var rnk float32
		if err := rows.Scan(&h.ChunkID, &h.DocumentID, &h.Source, &h.Title, &h.URL, &h.Content, &rnk); err != nil {
			return nil, err
		}
		rank++
		h.SparseRank = rank
		out = append(out, h)
	}
	return out, rows.Err()
}

// MarkProcessed records an idempotency key for an executor action. Returns
// true if the key was inserted (i.e., the action should proceed); false if a
// previous run already recorded it.
func (s *Store) MarkProcessed(ctx context.Context, key, producer, receipt string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO processed_events (idempotency_key, producer, receipt)
		 VALUES ($1, $2, $3) ON CONFLICT (idempotency_key) DO NOTHING`,
		key, producer, receipt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	if len(s) > 60 {
		return s[:60]
	}
	return s
}
