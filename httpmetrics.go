package main

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type HTTPMetrics struct {
	requestCount     metric.Int64Counter
	requestDurantion metric.Int64Histogram
}

func NewHTTPMetrics(meter metric.Meter) (HTTPMetrics, error) {
	count, err := meter.Int64Counter(
		"http.server.request.count",
		metric.WithUnit("1"),
		metric.WithDescription("Number of HTTP server requests"),
	)
	if err != nil {
		return HTTPMetrics{}, err
	}

	duration, err := meter.Int64Histogram(
		"http.server.request.duration",
		metric.WithUnit("Duration of HTTP server request in ms"),
		metric.WithDescription("ms"),
	)
	if err != nil {
		return HTTPMetrics{}, err
	}

	return HTTPMetrics{
		requestCount:     count,
		requestDurantion: duration,
	}, nil
}

func (m HTTPMetrics) RecordRequestCount(ctx context.Context, r *http.Request, statusCode int) {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", r.Method),
		attribute.Int("http.status_code", statusCode),
	}

	m.requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (m HTTPMetrics) RecordRequestDuration(ctx context.Context, r *http.Request, start time.Time, statusCode int) {
	elapsed := time.Since(start).Milliseconds()
	attrs := []attribute.KeyValue{
		attribute.String("http.method", r.Method),
		attribute.Int("http.status_code", statusCode),
	}

	m.requestDurantion.Record(ctx, elapsed, metric.WithAttributes(attrs...))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func WrapResponseWriter(w http.ResponseWriter) *statusRecorder {
	if r, ok := w.(*statusRecorder); ok {
		return r
	}
	return &statusRecorder{
		ResponseWriter: w,
	}
}

func HTTPMetricsMiddleware(httpMetrics HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rec := WrapResponseWriter(w)

			next.ServeHTTP(rec, r)

			httpMetrics.RecordRequestCount(
				r.Context(),
				r,
				rec.status,
			)
			httpMetrics.RecordRequestDuration(
				r.Context(),
				r,
				start,
				rec.status,
			)
		})
	}
}
