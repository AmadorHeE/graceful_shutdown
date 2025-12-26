package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

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
		Config: config,
		Logger: logger,
	}, nil
}

func (a *APIServer) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", makeHTTPHandlerFunc(a.handleReadiness)) // Setup readiness endpoint
	mux.HandleFunc("/", makeHTTPHandlerFunc(a.handleHelloWorld))       // Setup hello world endpoint

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.Config.Port),
		Handler: otelhttp.NewHandler(mux, "http.server"),
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

func (a *APIServer) handleReadiness(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		return a.handleGetReadiness(w, r)
	}

	return fmt.Errorf("method not allowed: %s", r.Method)
}

func (a *APIServer) handleGetReadiness(w http.ResponseWriter, _ *http.Request) error {
	if !a.isShuttingDown.Load() {
		WriteJSON(
			w,
			http.StatusOK,
			GetReadinessResponse{
				Message: "ok",
			},
		)
		return nil
	}

	return APIError{
		Code:    503,
		Message: "the server is shutting down",
	}
}

func (a *APIServer) handleHelloWorld(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		return a.handleGetHelloWorld(w, r)
	}

	return fmt.Errorf("method not allowed: %s", r.Method)
}

func (a *APIServer) handleGetHelloWorld(w http.ResponseWriter, r *http.Request) error {
	select {
	case <-time.After(2 * time.Second):
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
		return nil
	case <-r.Context().Done():
		return APIError{
			Code:    http.StatusServiceUnavailable,
			Message: "request canceled",
		}
	}
}
