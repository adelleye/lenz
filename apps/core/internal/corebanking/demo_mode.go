package corebanking

import (
	"fmt"
	"os"
	"strings"
)

const EnvDemoMode = "LENZ_DEMO_MODE"

func DemoRoutesEnabled() (bool, error) {
	return DemoRoutesEnabledFromEnv(os.Getenv)
}

func DemoRoutesEnabledFromEnv(getenv func(string) string) (bool, error) {
	if !envBool(getenv(EnvDemoMode)) {
		return false, nil
	}
	env := effectiveAppEnv(getenv)
	if env == "" || env == "development" || env == "dev" || env == "local" || env == "test" {
		return true, nil
	}
	if env == "production" || env == "prod" {
		return false, fmt.Errorf("%s=true is not allowed when APP_ENV/ENV is production", EnvDemoMode)
	}
	return false, fmt.Errorf("%s=true is only allowed for local/dev/test environments", EnvDemoMode)
}

func envBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func effectiveAppEnv(getenv func(string) string) string {
	env := strings.TrimSpace(getenv("APP_ENV"))
	if env == "" {
		env = strings.TrimSpace(getenv("ENV"))
	}
	return strings.ToLower(env)
}
