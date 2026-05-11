// API gateway.
//
// HTTP endpoints:
//
//   POST  /v1/incidents       - submit a new incident; returns { incident_id }
//   GET   /v1/events          - SSE stream of incident timeline (?incident=<id|*>)
//   POST  /v1/config/llm      - save UI-supplied BYO-LLM config (encrypted at rest)
//   GET   /v1/config/llm      - return masked summary of the current config
//   POST  /v1/corpus          - ingest a single document (text)
//   GET   /healthz            - liveness
//   /                          - serves the static UI from $GATEWAY_UI_PATH
//
// Also subscribes to all bus traffic via routing key "#" and republishes
// each event to the SSE hub, fanning out to connected browsers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/config"
	"github.com/zipgod24/aetherflow/internal/events"
	"github.com/zipgod24/aetherflow/internal/llm"
	otelsetup "github.com/zipgod24/aetherflow/internal/otel"
	"github.com/zipgod24/aetherflow/internal/rag"
	"github.com/zipgod24/aetherflow/internal/security"
	"github.com/zipgod24/aetherflow/internal/sse"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "api-gateway")
	slog.SetDefault(log)

	cfg := config.Load()
	shutdown, _ := otelsetup.Setup(ctx, "api-gateway", cfg.ServiceNamespace, cfg.OTLPEndpoint)
	defer shutdown(context.Background()) //nolint:errcheck

	b, err := bus.Dial(ctx, bus.Config{
		URL: cfg.RabbitMQURL, Exchange: cfg.RabbitMQExchange,
		DLX: cfg.RabbitMQDLX, Prefetch: cfg.RabbitMQPrefetch,
	}, log)
	if err != nil {
		log.Error("bus dial", "err", err)
		return
	}
	defer b.Close()

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Error("pg pool", "err", err)
		return
	}
	defer pool.Close()
	store := rag.NewStore(pool, 768)
	if err := store.EnsureSchema(ctx); err != nil {
		log.Error("schema", "err", err)
		return
	}

	var sealer *security.Sealer
	if cfg.MasterKey != "" {
		s, err := security.NewSealer(cfg.MasterKey)
		if err != nil {
			log.Warn("master key invalid; BYO key feature disabled", "err", err)
		} else {
			sealer = s
		}
	}

	hub := sse.NewHub()
	gw := &gateway{
		cfg:    cfg,
		bus:    b,
		hub:    hub,
		store:  store,
		log:    log,
		sealer: sealer,
		emb:    llm.NewOllama(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaEmbedModel),
	}

	// Bus → SSE fan-out
	go func() {
		err := b.Subscribe(ctx, "q.gateway", []string{"#"}, gw.relay)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Error("gateway bus subscribe", "err", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/incidents", gw.submitIncident)
	mux.HandleFunc("GET /v1/events", hub.ServeHTTP)
	mux.HandleFunc("POST /v1/config/llm", gw.saveLLMConfig)
	mux.HandleFunc("GET /v1/config/llm", gw.getLLMConfig)
	mux.HandleFunc("POST /v1/corpus", gw.ingestCorpus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", http.FileServer(http.Dir(cfg.GatewayUIPath)))

	srv := &http.Server{
		Addr:              cfg.GatewayHTTPAddr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("gateway listening", "addr", cfg.GatewayHTTPAddr, "ui", cfg.GatewayUIPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_ = srv.Shutdown(sctx)
}

type gateway struct {
	cfg    config.Config
	bus    *bus.Bus
	hub    *sse.Hub
	store  *rag.Store
	log    *slog.Logger
	sealer *security.Sealer
	emb    rag.Embedder

	mu        sync.RWMutex
	llmConfig llmConfigRecord // user-supplied config (encrypted)
}

type llmConfigRecord struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	BaseURL      string `json:"base_url,omitempty"`
	APIKeyCipher string `json:"api_key_cipher,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

type submitReq struct {
	Description string            `json:"description"`
	Source      string            `json:"source,omitempty"`
	Severity    string            `json:"severity,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

func (g *gateway) submitIncident(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		http.Error(w, "description is required", http.StatusBadRequest)
		return
	}
	scan := security.InjectionScan(req.Description)
	if scan.HighestSeverity() == "high" {
		http.Error(w, "description rejected by input policy", http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		req.Source = "ui"
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	id := uuid.NewString()
	ev := events.IncidentCreated{
		Header: events.Header{
			EventID:        uuid.NewString(),
			IncidentID:     id,
			IdempotencyKey: id,
			SchemaVersion:  "v1",
			OccurredAt:     time.Now().UTC(),
			Producer:       "api-gateway",
		},
		Description: req.Description,
		Source:      req.Source,
		Severity:    req.Severity,
		Tags:        req.Tags,
		LLM:         g.currentOverride(),
	}
	headers := map[string]any{
		"incident_id": id,
	}
	if err := g.bus.Publish(r.Context(), events.KeyIncidentCreated, ev, headers); err != nil {
		http.Error(w, "publish failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	g.log.Info("incident submitted", "incident_id", id, "severity", req.Severity)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"incident_id": id})
}

type llmConfigReq struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

func (g *gateway) saveLLMConfig(w http.ResponseWriter, r *http.Request) {
	if g.sealer == nil {
		http.Error(w, "BYO LLM disabled — set AETHER_MASTER_KEY to enable", http.StatusServiceUnavailable)
		return
	}
	var req llmConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Provider {
	case "ollama":
		// Ollama doesn't take an API key; clearing is fine.
	case "openai", "anthropic", "openai_compatible":
		if req.APIKey == "" {
			http.Error(w, "api_key required for "+req.Provider, http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "unknown provider: "+req.Provider, http.StatusBadRequest)
		return
	}
	if req.BaseURL != "" && !g.cfg.AllowPrivateLLM {
		if u, err := url.Parse(req.BaseURL); err == nil && isPrivateHost(u.Hostname()) {
			http.Error(w, "private base_url not allowed; set AETHER_ALLOW_PRIVATE_LLM=true to override", http.StatusBadRequest)
			return
		}
	}

	rec := llmConfigRecord{
		Provider:  req.Provider,
		Model:     req.Model,
		BaseURL:   req.BaseURL,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if req.APIKey != "" {
		ct, err := g.sealer.Seal(req.APIKey)
		if err != nil {
			http.Error(w, "seal failed", http.StatusInternalServerError)
			return
		}
		rec.APIKeyCipher = ct
	}
	g.mu.Lock()
	g.llmConfig = rec
	g.mu.Unlock()
	g.log.Info("llm config saved", "provider", rec.Provider, "model", rec.Model, "has_key", rec.APIKeyCipher != "")
	w.WriteHeader(http.StatusNoContent)
}

func (g *gateway) getLLMConfig(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	rec := g.llmConfig
	g.mu.RUnlock()
	out := map[string]any{
		"provider":   rec.Provider,
		"model":      rec.Model,
		"base_url":   rec.BaseURL,
		"has_key":    rec.APIKeyCipher != "",
		"updated_at": rec.UpdatedAt,
	}
	if rec.Provider == "" {
		out["provider"] = g.cfg.LLMProvider // default cluster setting
		out["model"] = g.cfg.OllamaChatModel
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// currentOverride materializes an LLMOverride from the UI-supplied config, if any.
func (g *gateway) currentOverride() *events.LLMOverride {
	g.mu.RLock()
	rec := g.llmConfig
	g.mu.RUnlock()
	if rec.Provider == "" || g.sealer == nil {
		return nil
	}
	if rec.Provider == "ollama" {
		return &events.LLMOverride{Provider: "ollama", Model: rec.Model, BaseURL: rec.BaseURL}
	}
	if rec.APIKeyCipher == "" {
		return nil
	}
	key, err := g.sealer.Open(rec.APIKeyCipher)
	if err != nil {
		g.log.Warn("override open failed", "err", err)
		return nil
	}
	return &events.LLMOverride{
		Provider: rec.Provider,
		Model:    rec.Model,
		BaseURL:  rec.BaseURL,
		APIKey:   key,
	}
}

type ingestReq struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	URL    string `json:"url,omitempty"`
	Text   string `json:"text"`
}

func (g *gateway) ingestCorpus(w http.ResponseWriter, r *http.Request) {
	var req ingestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Text == "" || req.Title == "" {
		http.Error(w, "title and text are required", http.StatusBadRequest)
		return
	}
	chunks := rag.Chunk(req.Text, rag.DefaultChunkOptions())
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	emb, err := g.emb.Embed(r.Context(), texts)
	if err != nil {
		http.Error(w, "embed failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	doc := rag.Document{
		ID:     uuid.New(),
		Source: req.Source,
		Title:  req.Title,
		URL:    req.URL,
	}
	if err := g.store.IngestDocument(r.Context(), doc, chunks, emb); err != nil {
		http.Error(w, "ingest failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"document_id": doc.ID.String(),
		"chunks":      len(chunks),
	})
}

func (g *gateway) relay(ctx context.Context, d bus.Delivery) error {
	var hdr events.Header
	_ = json.Unmarshal(d.Body, &hdr)
	var payload any
	_ = json.Unmarshal(d.Body, &payload)
	// Strip user-supplied API keys before fanning out to the browser. The
	// keys live inside IncidentCreated.llm.api_key and ContextAssembled.llm.api_key.
	payload = redactForSSE(payload)
	g.hub.Publish(sse.Event{
		IncidentID: hdr.IncidentID,
		Type:       d.RoutingKey,
		Payload:    payload,
	})
	return nil
}

// redactForSSE walks the decoded JSON tree and replaces any "api_key" value
// with "<redacted>". This is a belt-and-braces complement to the
// KeyMaterial.MarshalJSON redactor — the LLMOverride here is decoded into a
// generic map[string]any, so we can't rely on the typed redactor.
func redactForSSE(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			if k == "api_key" {
				t[k] = "<redacted>"
				continue
			}
			t[k] = redactForSSE(vv)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactForSSE(t[i])
		}
		return t
	default:
		return v
	}
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func isPrivateHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	// RFC1918 quick checks
	if strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "169.254.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		// 172.16.0.0/12 — parse the second octet.
		parts := strings.Split(host, ".")
		if len(parts) > 1 {
			n := 0
			for _, r := range parts[1] {
				if r < '0' || r > '9' {
					n = -1
					break
				}
				n = n*10 + int(r-'0')
			}
			if n >= 16 && n <= 31 {
				return true
			}
		}
	}
	return false
}
