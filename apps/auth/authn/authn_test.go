package authn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDevelopmentAuthGuardAllowsDevelopment(t *testing.T) {
	err := ValidateDevelopmentAuthGuard(func(key string) string {
		switch key {
		case EnvAppEnv:
			return "development"
		case EnvDevAuthToken:
			return "test-token"
		default:
			return ""
		}
	}, AuthRequiredScope)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentAuthGuardRejectsProductionWithDevToken(t *testing.T) {
	err := ValidateDevelopmentAuthGuard(func(key string) string {
		switch key {
		case EnvAppEnv:
			return "production"
		case EnvDevAuthToken:
			return "test-token"
		default:
			return ""
		}
	}, AuthRequiredScope)
	if err == nil {
		t.Fatal("expected production dev-token auth guard to fail")
	}
	if !strings.Contains(err.Error(), "production requires real auth/RBAC") {
		t.Fatalf("expected clear production auth error, got %v", err)
	}
}

func TestDevelopmentAuthGuardRejectsProductionWithoutConfiguredToken(t *testing.T) {
	err := ValidateDevelopmentAuthGuard(func(key string) string {
		if key == EnvEnv {
			return "production"
		}
		return ""
	}, AuthRequiredScope)
	if err == nil {
		t.Fatal("expected production auth guard to fail without real auth")
	}
	if !strings.Contains(err.Error(), "production requires real auth/RBAC") {
		t.Fatalf("expected clear production auth error, got %v", err)
	}
}

func TestRequiredAuthAttachesDevelopmentPrincipal(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", "99999999-9999-9999-9999-999999999999")
	t.Setenv("LENZ_DEV_ACTOR_TYPE", "staff")
	t.Setenv("LENZ_DEV_ACTOR_ID", "staff-001")
	t.Setenv("LENZ_DEV_AUTH_ROLES", "operator, auditor")
	t.Setenv("LENZ_DEV_AUTH_SCOPES", "accounts:read, transfers:write")

	handler := Authentication(AuthRequiredScope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromRequest(r)
		if !ok {
			t.Fatal("expected principal on request context")
		}
		_ = json.NewEncoder(w).Encode(principal)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/demo/balance", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("User-Agent", "audit-test-agent")
	req.RemoteAddr = "203.0.113.10:4321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var principal Principal
	if err := json.Unmarshal(rec.Body.Bytes(), &principal); err != nil {
		t.Fatal(err)
	}
	if principal.InstitutionID != "99999999-9999-9999-9999-999999999999" {
		t.Fatalf("wrong institution scope: %+v", principal)
	}
	if principal.ActorType != "staff" || principal.ActorID != "staff-001" || principal.SourceIP != "203.0.113.10" || principal.UserAgent != "audit-test-agent" {
		t.Fatalf("wrong actor/request metadata: %+v", principal)
	}
	if len(principal.Roles) != 2 || principal.Roles[0] != "operator" || len(principal.Scopes) != 2 || principal.Scopes[1] != "transfers:write" {
		t.Fatalf("wrong roles/scopes: %+v", principal)
	}
}

func TestRequiredAuthRejectsQueryStringAccessToken(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	handler := Authentication(AuthRequiredScope)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/demo/balance?access_token=test-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected query token to be rejected with 401, got %d", rec.Code)
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
