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

func CoreAuthn(scopes ...AuthScope) {

}

func Authentication(scopes ...AuthScope) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r.URL.Path) {
				h.ServeHTTP(w, r)
				return
			}

			token := VerifyTokens(r, getTokenFromQuery, getTokenFromHeader)
			if !requiresAuth(scopes) && token == "" {
				h.ServeHTTP(w, r)
				return
			}
			if !validDevelopmentToken(token) {
				writeUnauthorized(w)
				return
			}

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

func getTokenFromQuery(r *http.Request) string {
	return r.URL.Query().Get("access_token")
}

func getTokenFromHeader(r *http.Request) string {
	bearer := r.Header.Get("Authorization")
	if len(bearer) > 7 && strings.ToUpper(bearer[0:6]) == "BEARER" {
		return bearer[7:]
	}

	return ""
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
	expected := strings.TrimSpace(os.Getenv("LENZ_DEV_AUTH_TOKEN"))
	return expected != "" && token == expected
}

func isPublicPath(path string) bool {
	return path == "/api/v1/health"
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
}
