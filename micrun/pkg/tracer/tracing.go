package tracer

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config describes the OTLP tracer configuration that shimv2 can consume.
type Config struct {
	// ServiceName is required and becomes the service.name resource attribute.
	ServiceName string
	// Endpoint holds the OTLP gRPC collector address (host:port).
	Endpoint string
	// Insecure toggles grpc.WithTransportCredentials(insecure.NewCredentials()) when true.
	Insecure bool
	// Headers allows additional metadata to be sent with every exporter connection.
	Headers map[string]string
	// Sampler overrides the default sampling decision when non-nil.
	Sampler sdktrace.Sampler
	// Attributes are appended to the resource created for the tracer.
	Attributes []attribute.KeyValue
}

// NewConfig returns a Config pre-populated with a conservative sampler and service name.
func NewConfig(serviceName string) Config {
	return Config{
		ServiceName: serviceName,
		Sampler:     sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0)),
	}
}

// NewTracerProvider builds and returns an OTEL TracerProvider according to cfg.
// The caller is responsible for calling Shutdown when the shim exits.
func NewTracerProvider(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("tracer: service name is required")
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(defaultAttributes(cfg)...),
	)
	if err != nil {
		return nil, fmt.Errorf("tracer: resource creation: %w", err)
	}

	exp, err := buildExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	bsp := sdktrace.NewBatchSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(selectSampler(cfg.Sampler)),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}

func defaultAttributes(cfg Config) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.ServiceName),
		attribute.String("process.pid", fmt.Sprintf("%d", os.Getpid())),
	}
	attrs = append(attrs, cfg.Attributes...)
	return attrs
}

func selectSampler(cfg sdktrace.Sampler) sdktrace.Sampler {
	if cfg == nil {
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))
	}
	return cfg
}

func buildExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{}
	if cfg.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
		} else {
			opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithBlock()))
		}
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}

	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("tracer: OTLP exporter creation: %w", err)
	}
	return exp, nil
}

// Note: this package only wires up the tracer provider; the shim must still register
// interceptors and ensure trace context flows through ttrpc metadata before using the provider.
