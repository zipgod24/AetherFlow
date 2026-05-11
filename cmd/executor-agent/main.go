// Executor agent.
//
// Consumes:   analysis.validated.v1
// Produces:   action.executed.v1   (one per recommended action)
//
// Each action is dispatched to a mock adapter (firewall, pager, ticketer).
// Real adapters would implement the same Actioner interface.
//
// Every action is keyed by (incident_id, kind, target) and recorded in the
// processed_events table before execution. A replay finds the key already
// present and emits an "outcome: skipped" event with the original receipt.
package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zipgod24/aetherflow/internal/agent"
	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/config"
	"github.com/zipgod24/aetherflow/internal/events"
	"github.com/zipgod24/aetherflow/internal/rag"
)

// Actioner abstracts a side-effect adapter.
type Actioner interface {
	Kind() string
	Execute(ctx context.Context, a events.RecommendedAction) (string, error) // returns receipt
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		slog.Error("pg pool", "err", err)
		return
	}
	defer pool.Close()
	store := rag.NewStore(pool, 768)
	if err := store.EnsureSchema(ctx); err != nil {
		slog.Error("ensure schema", "err", err)
		return
	}

	actioners := map[string]Actioner{
		"block_ip":      mockFirewall{kind: "block_ip"},
		"block_domain":  mockFirewall{kind: "block_domain"},
		"page_oncall":   mockPager{},
		"create_ticket": mockTicketer{},
	}

	handle := func(ctx context.Context, in bus.Delivery, log *slog.Logger) ([]agent.Outbound, error) {
		ev, err := events.Decode[events.AnalysisValidated](in.Body)
		if err != nil {
			return nil, err
		}
		var out []agent.Outbound
		for _, a := range ev.Analysis.Actions {
			adapter, ok := actioners[a.Kind]
			if !ok {
				log.Warn("no adapter for action kind", "kind", a.Kind)
				continue
			}
			key := idempotencyKey(ev.IncidentID, a.Kind, a.Target)
			fresh, err := store.MarkProcessed(ctx, key, "executor-agent", "")
			if err != nil {
				log.Error("mark processed failed", "err", err)
				return nil, err
			}
			if !fresh {
				log.Info("action already executed; skipping", "kind", a.Kind, "target", a.Target)
				out = append(out, makeExecuted(ev, a, "skipped", "duplicate idempotency key", key))
				continue
			}
			receipt, err := adapter.Execute(ctx, a)
			outcome := "executed"
			detail := ""
			if err != nil {
				outcome = "failed"
				detail = err.Error()
			}
			out = append(out, makeExecuted(ev, a, outcome, detail, receipt))
		}
		log.Info("actions dispatched", "count", len(out))
		return out, nil
	}

	agent.Run(agent.Spec{
		Name:        "executor-agent",
		QueueName:   "q.executor",
		RoutingKeys: []string{events.KeyAnalysisValidated},
		Handle:      handle,
	})
}

func makeExecuted(ev events.AnalysisValidated, a events.RecommendedAction, outcome, detail, receipt string) agent.Outbound {
	payload := events.ActionExecuted{
		Header: events.Header{
			EventID:        uuid.NewString(),
			IncidentID:     ev.IncidentID,
			TraceID:        ev.TraceID,
			IdempotencyKey: ev.IdempotencyKey + ":exec:" + a.Kind + ":" + a.Target,
			SchemaVersion:  "v1",
			OccurredAt:     time.Now().UTC(),
			Producer:       "executor-agent",
		},
		Action:     a,
		Outcome:    outcome,
		Detail:     detail,
		Receipt:    receipt,
		ExecutedAt: time.Now().UTC(),
	}
	return agent.Outbound{RoutingKey: events.KeyActionExecuted, Payload: payload}
}

func idempotencyKey(incidentID, kind, target string) string {
	h := sha1.Sum([]byte(incidentID + "|" + kind + "|" + target))
	return "act:" + hex.EncodeToString(h[:])
}

// --- Mock adapters --------------------------------------------------------

type mockFirewall struct{ kind string }

func (m mockFirewall) Kind() string { return m.kind }

func (m mockFirewall) Execute(ctx context.Context, a events.RecommendedAction) (string, error) {
	// Pretend to call out to a NGFW REST API.
	time.Sleep(10 * time.Millisecond)
	return fmt.Sprintf("ngfw://rule/%s/%s", a.Kind, a.Target), nil
}

type mockPager struct{}

func (mockPager) Kind() string { return "page_oncall" }
func (mockPager) Execute(ctx context.Context, a events.RecommendedAction) (string, error) {
	time.Sleep(5 * time.Millisecond)
	prio := a.Args["priority"]
	if prio == "" {
		prio = "3"
	}
	return fmt.Sprintf("pager://incident/%s?priority=%s", uuid.NewString(), prio), nil
}

type mockTicketer struct{}

func (mockTicketer) Kind() string { return "create_ticket" }
func (mockTicketer) Execute(ctx context.Context, a events.RecommendedAction) (string, error) {
	time.Sleep(5 * time.Millisecond)
	return fmt.Sprintf("ticket://INC-%s", uuid.NewString()[:8]), nil
}
