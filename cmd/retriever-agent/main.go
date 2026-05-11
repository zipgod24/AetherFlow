// Retriever agent.
//
// Consumes:   incident.created.v1
// Produces:   context.assembled.v1
//
// Responsibilities:
//
//   - Extract entities (domains, IPs, hashes, CVE IDs) from the description.
//   - Run a deterministic set of DNS tools per entity kind.
//   - Run the hybrid RAG retriever against the corpus.
//   - Bundle everything into a ContextAssembled event.
package main

import (
	"context"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zipgod24/aetherflow/internal/agent"
	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/config"
	aetherdns "github.com/zipgod24/aetherflow/internal/dns"
	"github.com/zipgod24/aetherflow/internal/events"
	"github.com/zipgod24/aetherflow/internal/llm"
	"github.com/zipgod24/aetherflow/internal/rag"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		slog.Error("pg pool", "err", err)
		return
	}
	defer pool.Close()

	store := rag.NewStore(pool, embedDim(cfg))
	if err := store.EnsureSchema(ctx); err != nil {
		slog.Error("ensure schema", "err", err)
		return
	}

	embedder := newEmbedder(cfg)
	retriever := rag.NewRetriever(store, embedder)

	resolver := aetherdns.NewResolver(cfg.DNSResolver)
	ti := aetherdns.NewThreatIntel(resolver, "threats."+cfg.DNSBase)

	handle := func(ctx context.Context, in bus.Delivery, log *slog.Logger) ([]agent.Outbound, error) {
		ev, err := events.Decode[events.IncidentCreated](in.Body)
		if err != nil {
			return nil, err
		}
		log = log.With("retriever_stage", "start")

		entities := extractEntities(ev.Description)
		log.Info("entities extracted",
			"domains", len(entities.Domains),
			"ips", len(entities.IPs),
			"hashes", len(entities.Hashes),
			"cves", len(entities.CVEs),
		)

		var dnsObs []events.DNSObservation
		dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
		defer dcancel()
		for _, d := range entities.Domains {
			if recs, err := resolver.LookupA(dctx, d); err == nil {
				dnsObs = append(dnsObs, events.DNSObservation{Tool: "resolve_a", Target: d, Records: recs})
			} else {
				dnsObs = append(dnsObs, events.DNSObservation{Tool: "resolve_a", Target: d, Error: err.Error()})
			}
			if recs, err := resolver.LookupMX(dctx, d); err == nil && len(recs) > 0 {
				dnsObs = append(dnsObs, events.DNSObservation{Tool: "resolve_mx", Target: d, Records: recs})
			}
			if recs, err := resolver.LookupNS(dctx, d); err == nil && len(recs) > 0 {
				dnsObs = append(dnsObs, events.DNSObservation{Tool: "resolve_ns", Target: d, Records: recs})
			}
			if v, _ := ti.Lookup(dctx, d); v.Found {
				dnsObs = append(dnsObs, events.DNSObservation{
					Tool: "threat_intel_txt", Target: d,
					Verdict: v.Category + " conf=" + ftoa(v.Confidence) + " src=" + v.Source,
				})
			}
		}
		for _, ip := range entities.IPs {
			rev, _ := net.LookupAddr(ip)
			if len(rev) > 0 {
				dnsObs = append(dnsObs, events.DNSObservation{Tool: "reverse_ptr", Target: ip, Records: rev})
			}
			if v, _ := ti.Lookup(dctx, ip); v.Found {
				dnsObs = append(dnsObs, events.DNSObservation{
					Tool: "threat_intel_txt", Target: ip,
					Verdict: v.Category + " conf=" + ftoa(v.Confidence) + " src=" + v.Source,
				})
			}
		}

		// Hybrid RAG over the corpus.
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		defer rcancel()
		hits, herr := retriever.Hybrid(rctx, ev.Description)
		degraded := herr != nil
		if herr != nil {
			log.Warn("RAG hybrid failed; degrading", "err", herr)
		}

		var evidence []events.Evidence
		for _, h := range hits {
			evidence = append(evidence, events.Evidence{
				ChunkID:    h.ChunkID.String(),
				DocumentID: h.DocumentID.String(),
				Source:     h.Source,
				Title:      h.Title,
				URL:        h.URL,
				Snippet:    truncate(h.Content, 600),
				DenseScore: h.DenseScore,
				SparseRank: h.SparseRank,
				FusedScore: h.FusedScore,
			})
		}

		out := events.ContextAssembled{
			Header: events.Header{
				EventID:        uuid.NewString(),
				IncidentID:     ev.IncidentID,
				TraceID:        ev.TraceID,
				IdempotencyKey: ev.IdempotencyKey + ":retriever",
				SchemaVersion:  "v1",
				OccurredAt:     time.Now().UTC(),
				Producer:       "retriever-agent",
			},
			Description:     ev.Description,
			Severity:        ev.Severity,
			Evidence:        evidence,
			DNSObservations: dnsObs,
			Degraded:        degraded,
			LLM:             ev.LLM,
		}

		log.Info("context assembled", "evidence_chunks", len(evidence), "dns_observations", len(dnsObs), "degraded", degraded)
		return []agent.Outbound{{RoutingKey: events.KeyContextAssembled, Payload: out}}, nil
	}

	agent.Run(agent.Spec{
		Name:        "retriever-agent",
		QueueName:   "q.retriever",
		RoutingKeys: []string{events.KeyIncidentCreated},
		Handle:      handle,
	})
}

// embedDim chooses the dimension for the pgvector column.
//
// AetherFlow's MVP standardises on 768 (nomic-embed-text). If you switch the
// embedder, set OPENAI_EMBED_MODEL to one with a 768-dim output (e.g.
// `text-embedding-3-small` configured with `dimensions=768` at call time)
// or change this constant and re-create the schema. ADR-0003 covers the
// trade-off.
func embedDim(cfg config.Config) int { return 768 }

func newEmbedder(cfg config.Config) rag.Embedder {
	if cfg.LLMProvider == "openai" && cfg.OpenAIAPIKey != "" {
		return llm.NewOpenAI(cfg.OpenAIBaseURL, llm.NewKey(cfg.OpenAIAPIKey), cfg.OpenAIChatModel, cfg.OpenAIEmbedModel)
	}
	return llm.NewOllama(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaEmbedModel)
}

// Entities is the result of a quick regex-based extractor. It's deliberately
// not LLM-driven — it's fast, free, and easy to audit. Edge cases that need
// model intervention belong to the Reasoner.
type Entities struct {
	Domains []string
	IPs     []string
	Hashes  []string
	CVEs    []string
}

var (
	rxDomain = regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+[a-z]{2,}\b`)
	rxIPv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	rxHash   = regexp.MustCompile(`(?i)\b[a-f0-9]{32,64}\b`)
	rxCVE    = regexp.MustCompile(`\bCVE-\d{4}-\d{4,7}\b`)
)

func extractEntities(text string) Entities {
	var e Entities
	seen := map[string]struct{}{}
	add := func(slice *[]string, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		*slice = append(*slice, v)
	}
	for _, m := range rxDomain.FindAllString(text, -1) {
		// Skip pure IPs (which match the domain regex too).
		if rxIPv4.MatchString(m) {
			continue
		}
		add(&e.Domains, strings.ToLower(m))
	}
	for _, m := range rxIPv4.FindAllString(text, -1) {
		if net.ParseIP(m) != nil {
			add(&e.IPs, m)
		}
	}
	for _, m := range rxHash.FindAllString(text, -1) {
		add(&e.Hashes, strings.ToLower(m))
	}
	for _, m := range rxCVE.FindAllString(text, -1) {
		add(&e.CVEs, strings.ToUpper(m))
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func ftoa(f float32) string {
	// keep it simple: 2 dp
	if f != f { // NaN
		return "nan"
	}
	whole := int(f * 100)
	if whole < 0 {
		whole = -whole
	}
	hi := whole / 100
	lo := whole % 100
	if lo < 10 {
		return itoa(hi) + ".0" + itoa(lo)
	}
	return itoa(hi) + "." + itoa(lo)
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
