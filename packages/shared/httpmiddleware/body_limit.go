package httpmiddleware

import "net/http"

const (
	defaultLimitSize int64 = 1 << 20 // 1mb
)

func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > defaultLimitSize {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, defaultLimitSize)
		next.ServeHTTP(w, r)
	})
}
