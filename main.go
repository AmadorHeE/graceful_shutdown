package main

import (
	"context"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const (
	_shutdownPeriod      = 15 * time.Second
	_shutdownHardPeriod  = 3 * time.Second
	_readinessDrainDelay = 5 * time.Second
)

func main() {
	app, err := NewAPIServer()
	if err != nil {
		panic(err)
	}

	logger := app.Logger

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM) // It returns a context that is canceled when one of the specified signals is received
	defer stop()

	// By creating a separate context for the api server, we can control their lifecycle during shutdown
	ongoingCtx, stopOngoingGracefully := context.WithCancel(context.Background())

	go func() {
		logger.Info("Starting API server", zap.Int("port", app.Config.Port))
		if err := app.Run(ongoingCtx); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	<-rootCtx.Done() // Block until a signal is received
	stop()           // Stop receiving any more signals

	app.InitiateShutdown() // Mark the server as shutting down
	logger.Info("Receiving shutdown signal, shutting down.")

	time.Sleep(_readinessDrainDelay) // Give time for readiness check to propagate
	logger.Info("Readiness check propagated, now waiting for ongoing requests to finish.")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownPeriod)
	defer cancel()

	err = app.Shutdown(shutdownCtx)
	if err != nil {
		logger.Error("Failed to wait for ongoing requests to finish, waiting for forced cancellation")
	}
	stopOngoingGracefully() // Cancel ongoing requests context

	// Shutdown application resources
	err = app.ShutdownResources(shutdownCtx)
	if err != nil {
		logger.Error("Failed to shut down api server resources", zap.Error(err))
	}

	time.Sleep(_shutdownHardPeriod)

	logger.Info("Server shut down gracefully.")
}
