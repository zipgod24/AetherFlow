# Architecture

This document explains the **why** behind AetherFlow's structure. The README's diagrams show the **what**.

## 1. Event-driven, not request/response

Every agent is a consumer of one (or more) topics on a RabbitMQ topic exchange called `aether.events`. There is **no agent-to-agent RPC**. The benefits:

- **Decoupling.** Adding a new agent (e.g., a "compliance reviewer" that re-checks executor actions) is one new subscription, zero changes to existing code.
- **Replay.** Every event carries a `trace_id` and `incident_id`. We can re-drive an incident from the event log without rebuilding state.
- **Backpressure.** A slow Reasoner doesn't block the Retriever — they each pull from their own queue at their own rate.

### Topology

| Object | Type | Purpose |
| --- | --- | --- |
| `aether.events` | topic exchange | All normal traffic |
| `aether.dlx` | topic exchange | Dead-letter for parking, replay, alerting |
| `q.retriever` | durable queue, bind `incident.created` | Retriever input |
| `q.reasoner` | durable queue, bind `context.assembled` | Reasoner input |
| `q.validator` | durable queue, bind `analysis.completed` | Validator input |
| `q.executor` | durable queue, bind `analysis.validated` | Executor input |
| `q.gateway` | durable queue, bind `#` | API gateway SSE fan-out |

Each work queue has:

- `x-dead-letter-exchange` = `aether.dlx`
- `x-dead-letter-routing-key` = `<routing_key>.dead`
- Consumer prefetch = `RABBITMQ_PREFETCH` (default 16)

Failures get nack'd without requeue → DLX. A "parking lot" consumer in the gateway records DLX traffic so operators can inspect and selectively replay.

### Idempotency

Every event has an `idempotency_key`. Agents that produce side effects (Executor) check a `processed_events` table before acting; a partial crash plus replay therefore can't double-block an IP.

## 2. Agentic RAG

Naive RAG: vector search → stuff into prompt. AetherFlow is two notches up:

1. **Hybrid retrieval.** Dense (`pgvector` cosine, top-K) + sparse (Postgres `ts_rank_cd` over the chunk's `tsvector`). The two ranked lists are fused with **reciprocal rank fusion** (`1 / (k + rank)`, `k = 60`).
2. **Tool-using retriever.** Beyond the corpus, the Retriever has DNS lookup, threat-intel TXT-record resolution, and a CIDR/ASN heuristic available as tools. It picks them deterministically based on entity extraction over the incident description (no LLM round trip required for the simple cases — this is fast and cheap).
3. **Citation-grounded outputs.** The Reasoner is prompted to cite every claim by `chunk_id`. The Validator verifies every cited ID was actually in the retrieved set.

### Why pgvector

- One container for both vectors and relational metadata. Easier ops, easier joins, easier hybrid search.
- HNSW indexes are excellent up to ~50M vectors; that's well past the scale this MVP is meant to demonstrate.
- ADR-0003 captures the decision in full.

## 3. DNS-native service discovery

Each service registers a DNS SRV record (`_amqp._tcp.aether.local` for RabbitMQ, `_otlp._tcp.aether.local` for the collector, etc.). Clients resolve those records at startup, then refresh on a `DNS_REFRESH_INTERVAL`. There is **no Consul, no Zookeeper, no etcd dependency** — the OS resolver is the discovery layer.

The dev `docker-compose.yml` runs CoreDNS with a static zonefile. In production, you point services at your Kubernetes cluster's DNS (Headless Services give you SRV records for free).

For the demo workflow, DNS is also used as a **lookup tool**: the Retriever resolves the suspicious domain's A, MX, NS, and TXT records, plus optionally a threat-intel zone (e.g., `<domain>.threats.aether.local` returning a TXT verdict).

## 4. LLM provider abstraction

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}
```

Adapters: `ollama`, `openai`, `anthropic`, `openai_compatible` (any base URL).

The Reasoner pulls a provider from a `Registry` at the start of each incident. If the incident's `LLMOverride` header is set (because the UI submitted user keys), the registry materializes a one-shot provider for that incident only.

## 5. Observability

Every binary creates an OpenTelemetry tracer at startup. AMQP messages carry W3C trace-context in their headers, so a trace spans the full bus journey. The Jaeger UI in compose lets you click an incident_id and see every span: gateway HTTP, AMQP publish, retriever DNS lookups, retriever DB query, reasoner LLM call, validator scans, executor action.

Logging is `log/slog` in JSON mode, with `trace_id` and `incident_id` always attached.

## 6. Failure modes

| Failure | What happens |
| --- | --- |
| Retriever crashes mid-incident | Message redelivers to a sibling Retriever; if no siblings, message sits in queue until restart |
| LLM returns malformed JSON | Reasoner retries with stricter prompt up to 3x, then nacks to DLX |
| Validator finds injection markers | Event → DLX with structured rejection reason; gateway streams `validation.rejected` to UI |
| Executor crashes after acting but before publishing `action.executed` | Idempotency table prevents double-execute on replay |
| pgvector down | Retriever serves "no context" with a `degraded: true` flag; Reasoner is instructed to either refuse or answer conservatively |
| Ollama OOM | Reasoner retries with smaller `num_ctx` parameter; falls back to a smaller model if configured |

See ADR-0001 for the failure-handling decision record.
