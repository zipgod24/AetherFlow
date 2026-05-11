.SHELL := /bin/bash
.PHONY: help up down logs seed demo build test lint fmt vet bench clean keygen

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

keygen: ## generate a 32-byte master key for AETHER_MASTER_KEY
	@openssl rand -base64 32

up: ## boot the full stack (compose + ollama model pull)
	@if [ ! -f .env ]; then cp .env.example .env; echo "wrote .env from .env.example"; fi
	docker compose up -d --build
	@echo
	@echo "RabbitMQ UI: http://localhost:15672 (aether / aether)"
	@echo "Jaeger UI:   http://localhost:16686"
	@echo "AetherFlow:  http://localhost:8080"
	@echo
	@echo "Pulling Ollama models in the background…"
	@docker exec aether-ollama ollama pull llama3.1:8b-instruct || true
	@docker exec aether-ollama ollama pull nomic-embed-text || true

down: ## stop everything
	docker compose down

logs: ## tail all logs
	docker compose logs -f --tail=100

seed: ## load the sample threat-intel corpus
	@bash scripts/seed-rag.sh

demo: ## submit the demo incident
	@bash scripts/demo-incident.sh

build: ## build all Go binaries locally to ./bin
	@mkdir -p bin
	@for d in api-gateway orchestrator retriever-agent reasoner-agent validator-agent executor-agent aetherctl; do \
	  echo "building $$d"; \
	  go build -trimpath -o bin/$$d ./cmd/$$d; \
	done

test: ## run unit tests with the race detector
	go test -race -count=1 ./...

bench: ## run benchmarks
	go test -run=^$$ -bench=. -benchmem ./...

lint: vet fmt ## run vet + gofmt

vet: ## go vet
	go vet ./...

fmt: ## check formatting (CI uses this)
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
	  echo "the following files are not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

clean: ## remove build artifacts
	rm -rf bin/ coverage.* *.out
