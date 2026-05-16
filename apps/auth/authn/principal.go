package authn

import (
	"context"
	"net/http"
)

type principalContextKey struct{}

// Principal is the authenticated caller identity attached to request context.
// The current implementation is intentionally dev-token backed, but handlers
// should depend on this shape instead of caller-supplied tenant headers.
type Principal struct {
	InstitutionID string
	Roles         []string
	Scopes        []string
}

func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func RequestWithPrincipal(r *http.Request, principal Principal) *http.Request {
	return r.WithContext(ContextWithPrincipal(r.Context(), principal))
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok || principal.InstitutionID == "" {
		return Principal{}, false
	}
	return principal, true
}

func PrincipalFromRequest(r *http.Request) (Principal, bool) {
	return PrincipalFromContext(r.Context())
}
