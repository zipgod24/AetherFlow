# ADR-0004: DNS as the service-discovery substrate

- **Status:** accepted
- **Date:** 2026-05

## Context

We need to find peers (RabbitMQ, Postgres, Ollama, the OTLP collector) without baking hostnames into config. The usual options are Consul, etcd, or "just hardcode k8s service names". We wanted something that:

- Works identically in `docker-compose` dev and Kubernetes prod.
- Doesn't add a fourth datastore.
- Is auditable with `dig`.

## Decision

Services discover peers via DNS SRV records.

- Dev: CoreDNS container with a static `aether.local` zonefile.
- Prod: Kubernetes Headless Services give you SRV records (`_amqp._tcp.rabbitmq.aetherflow.svc.cluster.local`) for free.

Clients call `dns.Resolve("_amqp._tcp.aether.local")`, sort by SRV priority/weight, retry on failure, and refresh on an interval. The resolver is configurable (`AETHER_DNS_RESOLVER`) so we can pin to a known-good resolver in production (DoT/DoH planned).

DNS also doubles as a **tool** for the Retriever — see `internal/dns/threatintel.go`.

## Consequences

**Positive**

- Zero new operational dependencies.
- Identical code paths in dev and prod.
- Threat-intel TXT records are a natural data source.

**Negative**

- DNS caching gotchas — we set a short TTL in the zonefile (10s) for dev to make iteration quick.
- DNS poisoning is in the threat model (T-07).

## Follow-ups

- Add DoT/DoH support to the resolver.
- Add a watchdog that alerts when SRV results contain only stale endpoints.
