// Package otel wires up OpenTelemetry tracing for AetherFlow services.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is empty the package returns a no-op
// shutdown function so dev builds without Jaeger run fine.
package otel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// ShutdownFunc flushes pending spans and closes the exporter.
type ShutdownFunc func(ctx context.Context) error

// Setup installs a global TracerProvider + Propagators.
// service is the OTel service.name (e.g. "retriever-agent").
func Setup(ctx context.Context, service, namespace, endpoint string) (ShutdownFunc, error) {
	if endpoint == "" {
		// No-op: rely on the default no-op TracerProvider.
		return func(context.Context) error { return nil }, nil
	}

	// otlptracehttp expects a host[:port] (no scheme).
	host := endpoint
	insecure := false
	if strings.HasPrefix(endpoint, "http://") {
		host = strings.TrimPrefix(endpoint, "http://")
		insecure = true
	} else if strings.HasPrefix(endpoint, "https://") {
		host = strings.TrimPrefix(endpoint, "https://")
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(service),
			semconv.ServiceNamespace(namespace),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}
