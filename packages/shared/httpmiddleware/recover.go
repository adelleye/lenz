package httpmiddleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
)

func Recover(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil && err != http.ErrAbortHandler {
				defer r.Body.Close()

				logEntry := middleware.GetLogEntry(r)
				if logEntry != nil {
					logEntry.Panic(err, debug.Stack())
				} else {
					debug.PrintStack()
				}

				log.Printf("Recovered from panic: %v\n%s", err, string(debug.Stack()))
				log.Printf("Request: %s %s\n", r.Method, r.URL.RequestURI())

				// TODO: use default error handler
				render.Status(r, http.StatusInternalServerError)
				render.JSON(w, r, map[string]string{
					"error": http.StatusText(http.StatusInternalServerError),
				})
			}
		}()
	})
}
