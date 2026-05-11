# ADR-0002: Specialized agents (Retriever / Reasoner / Validator / Executor)

- **Status:** accepted
- **Date:** 2026-05

## Context

The "do everything in one LLM call" pattern doesn't compose, doesn't scale, and offers nowhere to insert defensive checks. Production agentic systems split responsibilities and let each specialty be tuned, swapped, or replicated independently.

## Decision

We split work into four roles:

1. **Retriever** — entity extraction, deterministic tool selection (DNS, threat-intel), hybrid RAG against the corpus, evidence assembly.
2. **Reasoner** — single structured LLM call that takes the evidence and produces an `IncidentAnalysis` JSON.
3. **Validator** — non-LLM checks for schema conformance, citation grounding, injection markers, tool-safety bounds.
4. **Executor** — idempotent side effects (firewall block, page, ticket).

The Retriever's tool selection is **deterministic, not LLM-driven**, for the common cases (IP, domain, hash). This is faster, cheaper, and easier to audit. LLM-driven tool use is reserved for the Reasoner.

## Consequences

**Positive**

- Each role has a clean SLA and trace.
- Validator is the choke point for adversarial defense.
- The Reasoner can be swapped without touching Retriever or Validator.

**Negative**

- More moving parts. Mitigated by the choreography pattern (ADR-0001) — adding/removing/replacing a role is one queue subscription.

## Why not let the LLM orchestrate

We don't want a model to decide whether to skip the Validator.
