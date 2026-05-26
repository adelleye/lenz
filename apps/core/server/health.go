package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const readyzTimeout = 2 * time.Second

type dbPinger interface {
	PingContext(context.Context) error
}

func registerHealthRoutes(r chi.Router, pinger dbPinger) {
	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	})

	r.Get("/api/v1/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")

		ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
		defer cancel()

		if pinger == nil || pinger.PingContext(ctx) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status": "not_ready"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ready"}`))
	})
}
