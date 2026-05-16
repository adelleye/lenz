package httpmiddleware

import (
	"net/http"
)

func RealIPWithContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr // already set by chi middleware.RealIP
		if ip != "" {
			// add this to the context
		}
		next.ServeHTTP(w, r)
	})
}
