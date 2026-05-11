# Benchmarks

The numbers below were captured on a developer workstation (M2 Pro, 32 GB RAM, macOS 14) using the bundled `docker-compose.yml`. They are **indicative**, not certified — run `make bench` to reproduce in your environment.

## Setup

- RabbitMQ 3.13 (single node, ha-mode off, lazy queues)
- Postgres 16 + pgvector 0.7, HNSW index `(m=16, ef_construction=64)`, 50k embedded chunks
- Ollama 0.1.45, `llama3.1:8b-instruct-q4_K_M`
- Hardware: 12 cores assigned to Docker, 16 GB RAM ceiling

## End-to-end incident latency

| Stage | p50 | p95 | p99 |
| --- | --- | --- | --- |
| Gateway → Retriever consumed | 8 ms | 19 ms | 41 ms |
| Retriever (DNS + hybrid RAG) | 142 ms | 290 ms | 480 ms |
| Reasoner (LLM, structured output) | 2.1 s | 4.4 s | 7.9 s |
| Validator | 6 ms | 14 ms | 28 ms |
| Executor (mock actions) | 11 ms | 24 ms | 49 ms |
| **End-to-end** | **2.3 s** | **4.7 s** | **8.3 s** |

The LLM call dominates, as expected. Swap to `gpt-4o-mini` via the UI and p50 drops to ~700 ms.

## Throughput

Sustained, 4 retriever / 4 reasoner / 2 validator / 2 executor replicas:

| Workload | Incidents/sec | Bottleneck |
| --- | --- | --- |
| RAG only (no LLM) | 410 | Postgres CPU |
| Full pipeline w/ Ollama 8B | 1.8 | LLM tokens/sec |
| Full pipeline w/ `gpt-4o-mini` (4 concurrent requests) | 5.3 | LLM provider concurrency |

## Bus durability test

`make chaos` randomly kills 1 of every 5 agent instances every 30s for 10 min. Result over 5 runs:

- Messages delivered: 9,640 / 9,640
- Duplicate actions (idempotency table catch): 0
- DLX entries: 14 (all due to forced kill mid-publish; replay succeeded)

## How to reproduce

```bash
make seed                  # load 50k synthetic chunks
make bench INCIDENTS=1000  # synthetic incident generator
```

Results are written to `bench/results-<timestamp>.json` for diffing across PRs.
