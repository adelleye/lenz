package authn

import (
	"encoding/json"
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
	EnvDevAuthToken     = "LENZ_DEV_AUTH_TOKEN"
	EnvDevInstitutionID = "LENZ_DEV_INSTITUTION_ID"
	EnvDevAuthRoles     = "LENZ_DEV_AUTH_ROLES"
	EnvDevAuthScopes    = "LENZ_DEV_AUTH_SCOPES"

	defaultDevInstitutionID = "11111111-1111-1111-1111-111111111111"
)

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

func validDevelopmentToken(token string) bool {
	expected := strings.TrimSpace(os.Getenv(EnvDevAuthToken))
	return expected != "" && token == expected
}

func developmentPrincipal(token string) (Principal, bool) {
	if !validDevelopmentToken(token) {
		return Principal{}, false
	}
	institutionID := strings.TrimSpace(os.Getenv(EnvDevInstitutionID))
	if institutionID == "" {
		institutionID = defaultDevInstitutionID
	}
	return Principal{
		InstitutionID: institutionID,
		Roles:         envCSV(EnvDevAuthRoles, []string{"developer"}),
		Scopes:        envCSV(EnvDevAuthScopes, []string{"corebanking:read", "corebanking:write"}),
	}, true
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
	return path == "/api/v1/health"
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
}
