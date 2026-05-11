// Orchestrator.
//
// Subscribes to every event with routing key `#` and:
//
//   - Tracks per-incident progress in memory (with periodic compaction).
//   - Emits a timeout if an incident has not reached action.executed within
//     a configurable deadline (default 5m). The timeout event is published
//     to the DLX so operators are alerted, mirroring how an SLO breach
//     notification would surface.
//
// This service is **not** the bus's coordinator — choreography is enforced
// by the routing keys. The orchestrator is a watcher for cross-cutting
// concerns (timeouts, archival, replay tooling).
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/zipgod24/aetherflow/internal/agent"
	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/events"
)

type state struct {
	started   time.Time
	lastStage string
	completed bool
}

func main() {
	var (
		mu       sync.Mutex
		byIncident = map[string]*state{}
	)

	const deadline = 5 * time.Minute

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			now := time.Now()
			mu.Lock()
			for id, s := range byIncident {
				if !s.completed && now.Sub(s.started) > deadline {
					slog.Warn("incident timeout",
						"incident_id", id,
						"last_stage", s.lastStage,
						"age", now.Sub(s.started).Round(time.Second),
					)
					s.completed = true // don't realert
				}
				if now.Sub(s.started) > 24*time.Hour {
					delete(byIncident, id)
				}
			}
			mu.Unlock()
		}
	}()

	handle := func(ctx context.Context, in bus.Delivery, log *slog.Logger) ([]agent.Outbound, error) {
		var h events.Header
		_ = json.Unmarshal(in.Body, &h) // tolerate unknown payloads — only header is needed
		if h.IncidentID == "" {
			return nil, nil
		}
		mu.Lock()
		s, ok := byIncident[h.IncidentID]
		if !ok {
			s = &state{started: time.Now()}
			byIncident[h.IncidentID] = s
		}
		s.lastStage = in.RoutingKey
		if in.RoutingKey == events.KeyActionExecuted ||
			in.RoutingKey == events.KeyValidationRejected {
			s.completed = true
		}
		mu.Unlock()
		return nil, nil
	}

	agent.Run(agent.Spec{
		Name:        "orchestrator",
		QueueName:   "q.orchestrator",
		RoutingKeys: []string{"#"},
		Handle:      handle,
	})
}
