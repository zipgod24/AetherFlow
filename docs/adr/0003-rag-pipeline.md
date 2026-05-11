# ADR-0003: Hybrid RAG on Postgres + pgvector

- **Status:** accepted
- **Date:** 2026-05

## Context

We need retrieval that handles both semantic queries ("lateral movement via SMB") and exact identifiers ("CVE-2024-3094", "185.220.101.42"). Dense-only retrieval loses on the exact-match case; sparse-only loses on the semantic case.

We also need relational joins (chunk → document → source → ingestion timestamp) and a small dependency footprint.

## Decision

Single Postgres 16 with **pgvector 0.7** plus the built-in `tsvector` full-text index.

Schema:

```sql
CREATE TABLE documents (
  id           uuid PRIMARY KEY,
  source       text NOT NULL,
  title        text NOT NULL,
  url          text,
  ingested_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE chunks (
  id           uuid PRIMARY KEY,
  document_id  uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  ordinal      int  NOT NULL,
  content      text NOT NULL,
  tsv          tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
  embedding    vector(768) NOT NULL,
  metadata     jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX chunks_tsv_idx       ON chunks USING gin(tsv);
CREATE INDEX chunks_embedding_idx ON chunks USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);
```

Retrieval:

1. Run dense ANN top-K (K=20).
2. Run sparse `ts_rank_cd` top-K (K=20).
3. Fuse with reciprocal rank fusion: `score(d) = Σ 1 / (60 + rank_i(d))`.
4. Truncate to top-N (N=8 by default).
5. Optional cross-encoder rerank slot (left as a `Reranker` interface for production).

## Consequences

**Positive**

- One database to operate.
- Hybrid is the production answer; we ship it on day one.
- Joins, transactions, JSONB metadata — all the Postgres goodies.

**Negative**

- pgvector tops out around tens of millions of vectors. For larger scale, swap in Weaviate / Qdrant behind the `rag.Store` interface.
