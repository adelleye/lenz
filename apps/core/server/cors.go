package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/cors"
)

const EnvCORSAllowedOrigins = "LENZ_CORS_ALLOWED_ORIGINS"

var defaultDevAllowedOrigins = []string{
	"http://localhost:3000",
	"http://localhost:5173",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:5173",
}

func newCORSFromEnv(getenv func(string) string) (*cors.Cors, error) {
	origins := splitCSV(getenv(EnvCORSAllowedOrigins))
	if len(origins) == 0 {
		origins = append([]string(nil), defaultDevAllowedOrigins...)
	}
	if isProductionEnv(getenv) {
		for _, origin := range origins {
			if origin == "*" {
				return nil, fmt.Errorf("%s cannot contain wildcard origins in production", EnvCORSAllowedOrigins)
			}
		}
	}
	return cors.New(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Idempotency-Key",
			"X-Institution-ID",
			"X-Request-ID",
		},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: false,
	}), nil
}

func splitCSV(raw string) []string {
	var values []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

func isProductionEnv(getenv func(string) string) bool {
	env := strings.TrimSpace(getenv("APP_ENV"))
	if env == "" {
		env = strings.TrimSpace(getenv("ENV"))
	}
	env = strings.ToLower(env)
	return env == "production" || env == "prod"
}
