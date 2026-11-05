package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type AppMetrics struct {
	validations    metric.Int64Counter
	validationFail metric.Int64Counter
	keysCreated    metric.Int64Counter
	databaseErrors metric.Int64Counter
}

func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = "auth-service"
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(samplerFromEnv()),
	)

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		return nil, err
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(30*time.Second))),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		return errors.Join(
			meterProvider.Shutdown(shutdownCtx),
			tracerProvider.Shutdown(shutdownCtx),
		)
	}, nil
}

func samplerFromEnv() trace.Sampler {
	ratio := 1.0
	if value := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 && parsed <= 1 {
			ratio = parsed
		}
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER"))) {
	case "always_off":
		return trace.NeverSample()
	case "traceidratio":
		return trace.TraceIDRatioBased(ratio)
	case "parentbased_traceidratio", "":
		return trace.ParentBased(trace.TraceIDRatioBased(ratio))
	default:
		return trace.ParentBased(trace.TraceIDRatioBased(ratio))
	}
}

func newAppMetrics() *AppMetrics {
	meter := otel.Meter("togglemaster/auth-service")
	validations, _ := meter.Int64Counter("togglemaster.auth.validations")
	validationFail, _ := meter.Int64Counter("togglemaster.auth.validation_failures")
	keysCreated, _ := meter.Int64Counter("togglemaster.auth.keys_created")
	databaseErrors, _ := meter.Int64Counter("togglemaster.auth.database_errors")
	return &AppMetrics{
		validations:    validations,
		validationFail: validationFail,
		keysCreated:    keysCreated,
		databaseErrors: databaseErrors,
	}
}

func (a *App) recordValidation(ctx context.Context, valid bool) {
	if a.Metrics == nil {
		return
	}
	a.Metrics.validations.Add(ctx, 1, metric.WithAttributes(attribute.Bool("auth.valid", valid)))
	if !valid {
		a.Metrics.validationFail.Add(ctx, 1)
	}
}

func (a *App) recordKeyCreated(ctx context.Context) {
	if a.Metrics != nil {
		a.Metrics.keysCreated.Add(ctx, 1)
	}
}

func (a *App) recordDatabaseError(ctx context.Context, operation string) {
	if a.Metrics != nil {
		a.Metrics.databaseErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("db.operation.name", operation)))
	}
}
