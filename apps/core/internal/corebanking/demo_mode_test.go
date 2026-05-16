package corebanking

import "testing"

func TestDemoModeIsDisabledByDefault(t *testing.T) {
	enabled, err := DemoRoutesEnabledFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected demo routes to be disabled by default")
	}
}

func TestDemoModeCannotRunInProduction(t *testing.T) {
	_, err := DemoRoutesEnabledFromEnv(func(key string) string {
		switch key {
		case EnvDemoMode:
			return "true"
		case "APP_ENV":
			return "production"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("expected production demo mode to fail")
	}
}

func TestDemoModeCanRunLocally(t *testing.T) {
	enabled, err := DemoRoutesEnabledFromEnv(func(key string) string {
		switch key {
		case EnvDemoMode:
			return "true"
		case "APP_ENV":
			return "development"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("expected local demo mode to be enabled")
	}
}
