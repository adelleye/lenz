package authn

import (
	"net/http"
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
			for _, scope := range scopes {
				token := VerifyTokens(r, getTokenFromQuery, getTokenFromHeader)
				if token == "" && scope == AuthOptionalScope {
					h.ServeHTTP(w, r)
					return
				}

				// check authn and authz based off the authentication flow
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
