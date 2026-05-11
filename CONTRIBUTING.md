# Contributing to AetherFlow

Thanks for considering a contribution. AetherFlow is an opinionated reference architecture, so we are deliberate about what lands in `main` — but discussion is always welcome.

## Getting started

```bash
git clone https://github.com/zipgod24/aetherflow.git
cd aetherflow
cp .env.example .env
make up    # boots everything
make test  # unit + race
```

Everything is plain Go modules. There is no Python/Node build step (the UI is intentionally vanilla JS so PRs that add a frontend toolchain will be politely declined).

## How we work

- **One concern per PR.** "Add Helm support for Vault-backed secrets" — yes. "Refactor + add feature + fix typos" — please split.
- **Architecture changes need an ADR.** Drop a new `docs/adr/NNNN-title.md` following the existing template before writing code.
- **No new dependencies without justification.** AetherFlow's dep list is short on purpose. If you add one, the PR description must explain why a stdlib or existing-dep solution doesn't work.
- **Tests.** All new packages need unit tests. Anything touching the bus or the retriever needs at least one integration test (see `tests/integration`).
- **Run `make lint` and `make test` before pushing.**

## Code style

- `gofmt` + `goimports` (CI will fail otherwise).
- Errors are returned, not panicked on. The only `panic` allowed is in startup wiring.
- Every exported symbol has a doc comment that starts with the symbol name.
- Context is always the first arg.
- Logs are structured (`log/slog`). No fmt.Println in production paths.

## Reporting security issues

Please do not file public GitHub issues for security problems. See [SECURITY.md](SECURITY.md).

## License

By contributing, you agree your contributions are licensed under the Apache License, Version 2.0.
