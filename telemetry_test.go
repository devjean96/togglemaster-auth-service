package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPInstrumentationContinuesIncomingTraceWithoutCredentials(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	app := &App{}
	handler := otelhttp.NewHandler(http.HandlerFunc(app.validateKeyHandler), "GET /validate")
	request := httptest.NewRequest(http.MethodGet, "/validate", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected server and business spans, got %d", len(spans))
	}
	for _, span := range spans {
		if span.SpanContext().TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("span %q did not continue incoming trace", span.Name())
		}
		for _, attr := range span.Attributes() {
			if attr.Key == "http.request.header.authorization" {
				t.Fatal("authorization header must not be captured")
			}
		}
	}
}
