package platform

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Endpoint, resource attributes and sampling come from the standard OTEL_* env vars.
// Telemetry is disabled when OTEL_EXPORTER_OTLP_ENDPOINT is not set.
func setupTelemetry(ctx context.Context, name string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName(name)))
	if err != nil {
		return nil, err
	}
	res, err = resource.Merge(res, resource.Environment())
	if err != nil {
		return nil, err
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

func instrumentHandler(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "http",
		otelhttp.WithFilter(func(r *http.Request) bool { return !isInternalPath(r.URL.Path) }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.Pattern == "" {
				return r.Method
			}
			// "POST /orders" or "/api/orders/"
			if _, route, ok := strings.Cut(r.Pattern, " "); ok {
				return r.Method + " " + route
			}
			return r.Method + " " + r.Pattern
		}),
	)
}

func InstrumentTransport(rt http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(rt)
}

// otelhttp puts http.route on metrics only
func setSpanRoute(r *http.Request) {
	if r.Pattern == "" {
		return
	}
	route := r.Pattern
	if _, after, ok := strings.Cut(route, " "); ok {
		route = after
	}
	trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(route))
}

func isInternalPath(p string) bool {
	return p == "/healthz" || p == "/readyz" || strings.HasPrefix(p, "/admin/")
}
