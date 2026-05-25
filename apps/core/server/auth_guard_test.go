package server

import (
	"lenz-core/apps/auth/authn"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestWithAuthnAllowsDevelopmentDevToken(t *testing.T) {
	t.Setenv(authn.EnvAppEnv, "development")
	t.Setenv(authn.EnvDevAuthToken, "test-token")

	err := WithAuthn(authn.AuthRequiredScope)(&Server{router: chi.NewRouter()})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWithAuthnRejectsProductionDevAuth(t *testing.T) {
	t.Setenv(authn.EnvAppEnv, "production")
	t.Setenv(authn.EnvDevAuthToken, "test-token")

	err := WithAuthn(authn.AuthRequiredScope)(&Server{router: chi.NewRouter()})
	if err == nil {
		t.Fatal("expected production dev auth guard to fail")
	}
	if !strings.Contains(err.Error(), "production requires real auth/RBAC") {
		t.Fatalf("expected clear production auth error, got %v", err)
	}
}
