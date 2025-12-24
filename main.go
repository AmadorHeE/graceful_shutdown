package main

import (
	"context"
	"net"
	"net/http"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const (
	_shutdownPeriod      = 15 * time.Second
	_shutdownHardPeriod  = 3 * time.Second
	_readinessDrainDelay = 5 * time.Second
)

var isShuttingDown atomic.Bool

func readinessHandler(w http.ResponseWriter, _ *http.Request) {
	if isShuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("shutting down"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func helloWorldHandler(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(2 * time.Second):
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	case <-r.Context().Done():
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("request canceled"))
	}
}

func main() {
	// By default Zap initializes its global logger to a no-op logger, so we need to create one explicitly
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM) // It returns a context that is canceled when one of the specified signals is received
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", readinessHandler) // Setup readiness endpoint
	mux.HandleFunc("/", helloWorldHandler)

	// By creating a separate context for ongoing requests, we can control their lifecycle during shutdown
	ongoingCtx, stopOngoingGracefully := context.WithCancel(context.Background())
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ongoingCtx
		},
	}

	go func() {
		logger.Info("Server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	<-rootCtx.Done() // Block until a signal is received
	stop()           // Stop receiving any more signals

	isShuttingDown.Store(true) // Mark the server as shutting down
	logger.Info("Receiving shutdown signal, shutting down.")

	time.Sleep(_readinessDrainDelay) // Give time for readiness check to propagate
	logger.Info("Readiness check propagated, now waiting for ongoing requests to finish.")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownPeriod)
	defer cancel()

	err = server.Shutdown(shutdownCtx)
	stopOngoingGracefully() // Cancel ongoing requests context
	if err != nil {
		logger.Error("Failed to wait for ongoing requests to finish, waiting for forced cancellation")
		time.Sleep(_shutdownHardPeriod)
	}

	logger.Info("Server shut down gracefully.")
}
