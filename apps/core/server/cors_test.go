package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSUsesExplicitSafeDevDefaults(t *testing.T) {
	corsMiddleware, err := newCORSFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	handler := corsMiddleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/accounts/demo/balance", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type, idempotency-key, x-institution-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("expected safe dev origin to be allowed, headers=%v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/v1/accounts/demo/balance", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpectedly allowed unconfigured origin: headers=%v", rec.Header())
	}
}

func TestCORSRejectsWildcardProductionOrigin(t *testing.T) {
	_, err := newCORSFromEnv(func(key string) string {
		switch key {
		case "APP_ENV":
			return "production"
		case EnvCORSAllowedOrigins:
			return "*"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("expected wildcard production CORS to fail")
	}
}
