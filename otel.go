package main

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type OTelProvider struct {
	propagator     propagation.TextMapPropagator
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider

	shutdownFuncs []func(context.Context) error
}

func NewOTelProvider(ctx context.Context, config Config) (*OTelProvider, error) {
	shutdownFuncs := []func(context.Context) error{}

	propagator := newPropagator()

	tracerProvider, err := newTracerProvider(ctx, config)
	if err != nil {
		return nil, err
	}

	meterProvider, err := newMeterProvider(ctx, config)
	if err != nil {
		return nil, err
	}

	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)

	return &OTelProvider{
		propagator:     propagator,
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
		shutdownFuncs:  shutdownFuncs,
	}, nil
}

// Initialize OpenTelemetry globally for the process
func (p *OTelProvider) Setup() {
	otel.SetTextMapPropagator(p.propagator)  // setup propagator.
	otel.SetTracerProvider(p.tracerProvider) // setup tracer provider.
	otel.SetMeterProvider(p.meterProvider)   // setup meter provider.
}

// shutdown calls cleanup functions registered via shoutdownFuncs.
// the errors from each function are joined and returned as a single error.
func (p *OTelProvider) Shutdown(ctx context.Context) error {
	var err error
	for _, fn := range p.shutdownFuncs {
		err = errors.Join(err, fn(ctx))
	}
	p.shutdownFuncs = nil

	return err
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, config Config) (*trace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(config.TracingEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Resource = service identity
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName("graceful-shutdown"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("environment", config.Env),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(exporter),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(0.1))),
	)
	return tp, nil
}

func newMeterProvider(ctx context.Context, config Config) (*metric.MeterProvider, error) {
	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(config.MetricsEndpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Resource = service identity
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName("graceful-shutdown"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("environment", config.Env),
		),
	)
	if err != nil {
		return nil, err
	}

	reader := metric.NewPeriodicReader(
		exporter,
		metric.WithInterval(30*time.Second),
	)

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader),
	)

	return mp, nil

}
