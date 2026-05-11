// Package agent provides the runtime base that every AetherFlow agent
// binary embeds.
//
// Each concrete agent (retriever, reasoner, validator, executor) supplies:
//
//   - a Name (used for slog, OTel service.name, queue naming)
//   - the routing keys it consumes
//   - a Handle function that turns one inbound event into 0..N outbound events
//
// The runtime wires up structured logging, OpenTelemetry, signal handling,
// graceful shutdown, and AMQP plumbing identically across every binary.
package agent

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zipgod24/aetherflow/internal/bus"
	"github.com/zipgod24/aetherflow/internal/config"
	otelsetup "github.com/zipgod24/aetherflow/internal/otel"
)

// Outbound represents a message to publish.
type Outbound struct {
	RoutingKey string
	Payload    any
	Headers    map[string]any
}

// HandleFunc processes one inbound delivery and returns 0..N outbound messages.
// Returning a non-nil error nacks the inbound delivery to DLX.
type HandleFunc func(ctx context.Context, in bus.Delivery, log *slog.Logger) ([]Outbound, error)

// Spec describes a concrete agent.
type Spec struct {
	Name        string
	QueueName   string
	RoutingKeys []string
	Handle      HandleFunc
}

// Run boots OTel, dials the bus, subscribes, and blocks until SIGINT/SIGTERM.
func Run(spec Spec) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log = log.With("service", spec.Name)
	slog.SetDefault(log)

	cfg := config.Load()

	shutdown, err := otelsetup.Setup(ctx, spec.Name, cfg.ServiceNamespace, cfg.OTLPEndpoint)
	if err != nil {
		log.Error("otel setup failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		c, cncl := context.WithTimeout(context.Background(), 3*time.Second)
		defer cncl()
		_ = shutdown(c)
	}()

	b, err := bus.Dial(ctx, bus.Config{
		URL:      cfg.RabbitMQURL,
		Exchange: cfg.RabbitMQExchange,
		DLX:      cfg.RabbitMQDLX,
		Prefetch: cfg.RabbitMQPrefetch,
	}, log)
	if err != nil {
		log.Error("bus dial failed", "err", err)
		os.Exit(1)
	}
	defer b.Close()

	wrapped := func(ctx context.Context, d bus.Delivery) error {
		dlog := log
		if incID, _ := d.Headers["incident_id"].(string); incID != "" {
			dlog = dlog.With("incident_id", incID)
		}
		if tid, _ := d.Headers["traceparent"].(string); tid != "" {
			dlog = dlog.With("traceparent", tid)
		}
		out, err := spec.Handle(ctx, d, dlog)
		if err != nil {
			return err
		}
		for _, o := range out {
			if o.Headers == nil {
				o.Headers = map[string]any{}
			}
			// Propagate incident_id / traceparent if the handler didn't set them.
			if _, ok := o.Headers["incident_id"]; !ok {
				if v, ok := d.Headers["incident_id"]; ok {
					o.Headers["incident_id"] = v
				}
			}
			if _, ok := o.Headers["traceparent"]; !ok {
				if v, ok := d.Headers["traceparent"]; ok {
					o.Headers["traceparent"] = v
				}
			}
			if perr := b.Publish(ctx, o.RoutingKey, o.Payload, o.Headers); perr != nil {
				dlog.Error("publish failed", "routing_key", o.RoutingKey, "err", perr)
				return perr
			}
		}
		return nil
	}

	log.Info("agent starting",
		"queue", spec.QueueName,
		"routing_keys", spec.RoutingKeys,
		"prefetch", cfg.RabbitMQPrefetch,
	)
	if err := b.Subscribe(ctx, spec.QueueName, spec.RoutingKeys, wrapped); err != nil {
		log.Error("subscribe failed", "err", err)
		os.Exit(1)
	}
	log.Info("agent stopped cleanly")
}
