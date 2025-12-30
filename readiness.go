package main

import (
	"fmt"
	"net/http"
)

func handleReadiness(apiServer *APIServer) apiFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if r.Method == http.MethodGet {
			if !apiServer.isShuttingDown.Load() {
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

		return fmt.Errorf("method not allowed: %s", r.Method)
	}
}
