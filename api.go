package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/kelseyhightower/envconfig"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
)

type GetReadinessResponse struct {
	Message string `json:"message"`
}

type apiFunc func(http.ResponseWriter, *http.Request) error

func WriteJSON(w http.ResponseWriter, status int, data any) error {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

func makeHTTPHandlerFunc(fn apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err != nil {
			var apiErr APIError
			if errors.As(err, &apiErr) {
				WriteJSON(w, apiErr.Code, apiErr)
				return
			}

			WriteJSON(w, http.StatusInternalServerError, APIError{
				Code:    http.StatusInternalServerError,
				Message: "internal server error",
			})
		}
	}
}

type APIServer struct {
	isShuttingDown atomic.Bool

	Config Config
	Logger *zap.Logger

	server *http.Server

	OTelProvider *OTelProvider

	shutdownFuncs []func(context.Context) error
}

func NewAPIServer() (*APIServer, error) {
	shutdownFuncs := []func(context.Context) error{}

	// load config from environment variables
	var config Config
	if err := envconfig.Process("gsd", &config); err != nil {
		return nil, err
	}

	// initialize base logger
	logger, err := NewBaseLogger()
	if err != nil {
		return nil, err
	}
	shutdownLogger := func(ctx context.Context) error {
		return logger.Sync()
	}
	shutdownFuncs = append(shutdownFuncs, shutdownLogger)

	// initialize OpenTelemetry
	otelProvider, err := NewOTelProvider(context.Background(), config)
	if err != nil {
		panic(err)
	}
	otelProvider.Setup()
	shutdownFuncs = append(shutdownFuncs, otelProvider.Shutdown)

	return &APIServer{
		Config:       config,
		Logger:       logger,
		OTelProvider: otelProvider,
	}, nil
}

func (a *APIServer) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", makeHTTPHandlerFunc(handleReadiness(a))) // Setup readiness endpoint
	mux.HandleFunc("/", makeHTTPHandlerFunc(handleHelloWorld))          // Setup hello world endpoint

	// setup http metrics
	httpMetrics, err := NewHTTPMetrics(a.OTelProvider.Meter)
	if err != nil {
		return err
	}
	httpMetricsMiddleware := HTTPMetricsMiddleware(httpMetrics)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.Config.Port),
		Handler: httpMetricsMiddleware(otelhttp.NewHandler(mux, "http.server")),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	a.server = server

	return server.ListenAndServe()
}

// Marks the server as shutting down.
func (a *APIServer) InitiateShutdown() {
	a.isShuttingDown.Store(true)
}

// Shutdown the HTTP server.
func (a *APIServer) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

// Shutdown runs all registered shutdown functions and aggregates their errors.
func (a *APIServer) ShutdownResources(ctx context.Context) error {
	var err error
	for _, fn := range a.shutdownFuncs {
		err = errors.Join(err, fn(ctx))
	}
	return err
}
