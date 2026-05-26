package authn

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

type AuthScope string

const (
	AuthRequiredScope AuthScope = "Auth.scopes"
	AuthOptionalScope AuthScope = "AuthOptional.scopes"
)

const (
	EnvDevAuthToken     = "LENZ_DEV_AUTH_TOKEN" // #nosec G101 -- environment variable name, not a credential value.
	EnvDevInstitutionID = "LENZ_DEV_INSTITUTION_ID"
	EnvDevActorType     = "LENZ_DEV_ACTOR_TYPE"
	EnvDevActorID       = "LENZ_DEV_ACTOR_ID"
	EnvDevAuthRoles     = "LENZ_DEV_AUTH_ROLES"
	EnvDevAuthScopes    = "LENZ_DEV_AUTH_SCOPES"
	EnvAppEnv           = "APP_ENV"
	EnvEnv              = "ENV"

	defaultDevInstitutionID = "11111111-1111-1111-1111-111111111111"
)

func ValidateDevelopmentAuthGuard(getenv func(string) string, scopes ...AuthScope) error {
	if !requiresAuth(scopes) {
		return nil
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if productionEnv(getenv(EnvAppEnv)) || productionEnv(getenv(EnvEnv)) {
		return fmt.Errorf("%s development-token auth is disabled in production; production requires real auth/RBAC before enabling non-health routes", EnvDevAuthToken)
	}
	return nil
}

func Authentication(scopes ...AuthScope) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r.URL.Path) {
				h.ServeHTTP(w, r)
				return
			}

			token := VerifyTokens(r, getTokenFromHeader)
			if !requiresAuth(scopes) && token == "" {
				h.ServeHTTP(w, r)
				return
			}
			principal, ok := developmentPrincipal(token)
			if !ok {
				writeUnauthorized(w)
				return
			}
			principal.SourceIP = requestSourceIP(r)
			principal.UserAgent = strings.TrimSpace(r.UserAgent())

			r = RequestWithPrincipal(r, principal)
			h.ServeHTTP(w, r)
		})
	}
}

func VerifyTokens(r *http.Request, fn ...func(r *http.Request) string) string {
	for _, fn := range fn {
		if t := fn(r); t != "" {
			return t
		}
	}

	return ""
}

func getTokenFromHeader(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer")], "Bearer") {
		return ""
	}
	if value[len("Bearer")] != ' ' {
		return ""
	}
	return strings.TrimSpace(value[len("Bearer "):])
}

func requiresAuth(scopes []AuthScope) bool {
	for _, scope := range scopes {
		if scope == AuthRequiredScope {
			return true
		}
	}
	return false
}

func productionEnv(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "production" || value == "prod"
}

func validDevelopmentToken(token string) bool {
	expected := strings.TrimSpace(os.Getenv(EnvDevAuthToken))
	token = strings.TrimSpace(token)
	return expected != "" &&
		len(token) == len(expected) &&
		subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func developmentPrincipal(token string) (Principal, bool) {
	if !validDevelopmentToken(token) {
		return Principal{}, false
	}
	institutionID := strings.TrimSpace(os.Getenv(EnvDevInstitutionID))
	if institutionID == "" {
		institutionID = defaultDevInstitutionID
	}
	actorType := strings.TrimSpace(os.Getenv(EnvDevActorType))
	if actorType == "" {
		actorType = "dev_user"
	}
	actorID := strings.TrimSpace(os.Getenv(EnvDevActorID))
	if actorID == "" {
		actorID = "dev-user"
	}
	return Principal{
		InstitutionID: institutionID,
		ActorType:     actorType,
		ActorID:       actorID,
		Roles:         envCSV(EnvDevAuthRoles, []string{"developer"}),
		Scopes:        envCSV(EnvDevAuthScopes, []string{"corebanking:read", "corebanking:write"}),
	}, true
}

func requestSourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func envCSV(name string, fallback []string) []string {
	raw := os.Getenv(name)
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func isPublicPath(path string) bool {
	return path == "/api/v1/health" || path == "/api/v1/readyz"
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
}
