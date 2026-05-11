# Security policy

## Reporting a vulnerability

Email **security@aetherflow.dev** (placeholder — replace with the maintainers' contact in your fork). Please include:

- A description of the issue
- Steps to reproduce, ideally with a minimal proof of concept
- The version / commit SHA you tested
- The impact you expect

We respond within 72 hours. Please don't open a public GitHub issue.

## Scope

In scope:

- The AetherFlow source in this repository
- The bundled `docker-compose.yml` and Helm chart **as written** (host-level misconfiguration of your own deployment is out of scope)

Out of scope:

- Vulnerabilities in third-party dependencies (please report upstream and let us know if a patch is urgent)
- Vulnerabilities in default credentials of the local dev compose file (these are documented as dev-only)
- Social engineering of maintainers

## Security model summary

Full model lives in [docs/threat-model.md](docs/threat-model.md). Highlights:

- **Untrusted LLM output.** Reasoner output is treated as adversarial input by the Validator and Executor.
- **Prompt-injection defense in depth.** Lexicon, regex, structural schema enforcement, citation grounding.
- **Idempotent side effects.** Every Executor action has a stable idempotency key.
- **mTLS-ready AMQP.** Cert-based auth supported via env vars.
- **Secret hygiene.** UI-supplied API keys are encrypted at rest with AES-GCM; key material is never logged or echoed to traces.
