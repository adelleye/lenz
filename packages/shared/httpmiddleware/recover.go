package httpmiddleware

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

func Recover(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil && err != http.ErrAbortHandler {
				defer r.Body.Close()

				logEntry := middleware.GetLogEntry(r)
				if logEntry != nil {
					logEntry.Panic(err, debug.Stack())
				} else {
					debug.PrintStack()
				}

				log.Printf("Recovered from panic: %v\n%s", err, string(debug.Stack()))
				// #nosec G706 -- request fields are sanitized before logging.
				log.Printf("Request: %s %s", safeLogValue(r.Method), safeLogValue(r.URL.RequestURI()))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"message":    "internal_server_error",
					"request_id": requestID(r),
				})
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

func requestID(r *http.Request) string {
	if requestID := strings.TrimSpace(middleware.GetReqID(r.Context())); requestID != "" {
		return requestID
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	return "unknown"
}

func safeLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}
