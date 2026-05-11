# ADR-0001: Event-driven bus on RabbitMQ topic exchange

- **Status:** accepted
- **Date:** 2026-05
- **Deciders:** AetherFlow founding maintainers

## Context

The agents need to coordinate. We considered three styles:

1. Direct gRPC fan-out from an orchestrator (request/response).
2. A workflow engine (Temporal, Cadence, Argo Workflows).
3. Choreographed events on a message bus.

## Decision

We use **option 3**: choreography on a RabbitMQ topic exchange. Each agent owns a single durable queue bound to one or more routing keys. There is no central orchestrator dispatching calls; the orchestrator service exists only for cross-cutting saga concerns (timeouts, compensations, archival).

Specifically:

- Single topic exchange `aether.events`, durable.
- One DLX `aether.dlx`, durable.
- Routing keys follow `<bounded.context>.<event>.<version>` (`incident.created.v1`, `context.assembled.v1`, …).
- Queues have `x-dead-letter-exchange = aether.dlx` and `x-dead-letter-routing-key = <original>.dead`.
- Manual ack, prefetch tunable per consumer.
- Trace-context propagated via AMQP headers (`traceparent`, `tracestate`).

## Consequences

**Positive**

- New agents subscribe; no changes to existing services.
- Replay is trivial: drain DLX or replay from event log.
- Backpressure is per-consumer, not global.
- Operationally familiar to anyone who's run RabbitMQ in anger.

**Negative**

- Choreography makes the overall workflow implicit. We mitigate by maintaining a single architecture diagram, ADRs, and an "incident timeline" view in the UI.
- We need idempotency keys everywhere because at-least-once delivery is a fact of life.
- Multi-step transactional semantics require explicit saga compensation in the orchestrator.

## Alternatives considered

- **gRPC RPC** — tight coupling; one slow agent blocks the chain; rebuilding the chain for a new agent is invasive.
- **Temporal** — excellent for long-running workflows but a much heavier operational dependency. Worth revisiting once we have a saga that genuinely needs activity timers and signals at the durable-workflow level.
- **NATS JetStream** — viable; chose RabbitMQ for its dead-letter ergonomics and our team's prior production experience.

## Follow-ups

- Implement HMAC event signing (T-08 in the threat model).
- Decide whether to introduce per-tenant exchanges for the multi-tenant story.
