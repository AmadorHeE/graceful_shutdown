package main

import (
	"fmt"
	"net/http"
	"time"
)

func handleHelloWorld(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		return handleGetHelloWorld(w, r)
	}

	return fmt.Errorf("method not allowed: %s", r.Method)
}

func handleGetHelloWorld(w http.ResponseWriter, r *http.Request) error {
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
