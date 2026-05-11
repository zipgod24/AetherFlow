// Validator agent.
//
// Consumes:   analysis.completed.v1
// Produces:   analysis.validated.v1   (on pass)
//             validation.rejected.v1  (on fail)
//
// Layered checks, none of which require an LLM:
//
//   1. Schema conformance: required fields present, values in allowed sets.
//   2. Citation grounding: every cited chunk_id was in the retrieved set.
//   3. Adversarial scan: prompt-injection markers in summary/IOCs/reasons.
//   4. Tool safety: every recommended action is within configured bounds.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zipgod24/aetherflow/internal/agent"
	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/events"
	"github.com/zipgod24/aetherflow/internal/security"
)

var allowedVerdicts = map[string]struct{}{
	"malicious":  {},
	"suspicious": {},
	"benign":     {},
	"unknown":    {},
}

func main() {
	safety := security.DefaultToolSafety()

	handle := func(ctx context.Context, in bus.Delivery, log *slog.Logger) ([]agent.Outbound, error) {
		ev, err := events.Decode[events.AnalysisCompleted](in.Body)
		if err != nil {
			return nil, err
		}

		// 1. Schema
		var reasons []string
		var markers []string
		if _, ok := allowedVerdicts[ev.Analysis.Verdict]; !ok {
			reasons = append(reasons, "invalid verdict: "+ev.Analysis.Verdict)
		}
		if ev.Analysis.Confidence < 0 || ev.Analysis.Confidence > 1 {
			reasons = append(reasons, "confidence out of [0,1]")
		}
		if strings.TrimSpace(ev.Analysis.Summary) == "" {
			reasons = append(reasons, "empty summary")
		}

		// 2. Citation grounding
		retrievedIDs := make([]string, 0, len(ev.Evidence))
		for _, e := range ev.Evidence {
			retrievedIDs = append(retrievedIDs, e.ChunkID)
		}
		if missing := security.CheckCitations(ev.Analysis.Citations, retrievedIDs); len(missing) > 0 {
			reasons = append(reasons, fmt.Sprintf("ungrounded citations: %v", missing))
		}

		// 3. Adversarial scan over text fields
		scan := security.InjectionScan(ev.Analysis.Summary)
		for _, ioc := range ev.Analysis.IOCs {
			scan.Markers = append(scan.Markers, security.InjectionScan(ioc).Markers...)
		}
		for _, a := range ev.Analysis.Actions {
			scan.Markers = append(scan.Markers, security.InjectionScan(a.Reason).Markers...)
			scan.Markers = append(scan.Markers, security.InjectionScan(a.Target).Markers...)
		}
		for _, m := range scan.Markers {
			markers = append(markers, m.Kind+":"+m.Severity+":"+m.Match)
		}
		if sev := scan.HighestSeverity(); sev == "high" {
			reasons = append(reasons, "high-severity injection markers in output")
		}

		// 4. Tool safety per recommended action
		var notes []string
		for _, a := range ev.Analysis.Actions {
			if why := safety.CheckAction(a.Kind, a.Target, a.Args); why != "" {
				reasons = append(reasons, "action ("+a.Kind+","+a.Target+"): "+why)
			}
			if scan := security.InjectionScan(a.Target); scan.HighestSeverity() != "" {
				notes = append(notes, "action target has scan markers: "+a.Target)
			}
		}

		if len(reasons) > 0 {
			rejected := events.ValidationRejected{
				Header: events.Header{
					EventID:        uuid.NewString(),
					IncidentID:     ev.IncidentID,
					TraceID:        ev.TraceID,
					IdempotencyKey: ev.IdempotencyKey + ":validator-reject",
					SchemaVersion:  "v1",
					OccurredAt:     time.Now().UTC(),
					Producer:       "validator-agent",
				},
				Reason:  strings.Join(reasons, "; "),
				Markers: markers,
			}
			log.Warn("validation rejected", "reasons", reasons)
			return []agent.Outbound{{RoutingKey: events.KeyValidationRejected, Payload: rejected}}, nil
		}

		validated := events.AnalysisValidated{
			Header: events.Header{
				EventID:        uuid.NewString(),
				IncidentID:     ev.IncidentID,
				TraceID:        ev.TraceID,
				IdempotencyKey: ev.IdempotencyKey + ":validator",
				SchemaVersion:  "v1",
				OccurredAt:     time.Now().UTC(),
				Producer:       "validator-agent",
			},
			Analysis: ev.Analysis,
			Notes:    notes,
		}
		log.Info("validation passed", "actions", len(ev.Analysis.Actions), "notes", len(notes))
		return []agent.Outbound{{RoutingKey: events.KeyAnalysisValidated, Payload: validated}}, nil
	}

	agent.Run(agent.Spec{
		Name:        "validator-agent",
		QueueName:   "q.validator",
		RoutingKeys: []string{events.KeyAnalysisCompleted},
		Handle:      handle,
	})
}
