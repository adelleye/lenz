package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequiredAuthRejectsMissingToken(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	handler := Authentication(AuthRequiredScope)(okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/accounts/demo/balance", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequiredAuthAcceptsConfiguredDevelopmentToken(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	handler := Authentication(AuthRequiredScope)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/demo/balance", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequiredAuthFailsClosedWithoutConfiguredToken(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "")
	handler := Authentication(AuthRequiredScope)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/demo/balance", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHealthPathIsPublic(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "")
	handler := Authentication(AuthRequiredScope)(okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected public health check, got %d", rec.Code)
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
