package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseDBPoolConfigDefaults(t *testing.T) {
	cfg, err := parseDBPoolConfig(envMap(nil))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.maxOpenConns != defaultDBMaxOpenConns {
		t.Fatalf("maxOpenConns = %d, want %d", cfg.maxOpenConns, defaultDBMaxOpenConns)
	}
	if cfg.maxIdleConns != defaultDBMaxIdleConns {
		t.Fatalf("maxIdleConns = %d, want %d", cfg.maxIdleConns, defaultDBMaxIdleConns)
	}
	if cfg.connMaxLifetime != defaultDBConnMaxLifetime {
		t.Fatalf("connMaxLifetime = %s, want %s", cfg.connMaxLifetime, defaultDBConnMaxLifetime)
	}
	if cfg.connMaxIdleTime != defaultDBConnMaxIdleTime {
		t.Fatalf("connMaxIdleTime = %s, want %s", cfg.connMaxIdleTime, defaultDBConnMaxIdleTime)
	}
}

func TestParseDBPoolConfigFromEnv(t *testing.T) {
	cfg, err := parseDBPoolConfig(envMap(map[string]string{
		envDBMaxOpenConns:    "12",
		envDBMaxIdleConns:    "4",
		envDBConnMaxLifetime: "1h",
		envDBConnMaxIdleTime: "10m",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.maxOpenConns != 12 {
		t.Fatalf("maxOpenConns = %d, want 12", cfg.maxOpenConns)
	}
	if cfg.maxIdleConns != 4 {
		t.Fatalf("maxIdleConns = %d, want 4", cfg.maxIdleConns)
	}
	if cfg.connMaxLifetime != time.Hour {
		t.Fatalf("connMaxLifetime = %s, want 1h", cfg.connMaxLifetime)
	}
	if cfg.connMaxIdleTime != 10*time.Minute {
		t.Fatalf("connMaxIdleTime = %s, want 10m", cfg.connMaxIdleTime)
	}
}

func TestParseDBPoolConfigRejectsInvalidEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "bad max open",
			env:  map[string]string{envDBMaxOpenConns: "many"},
			want: envDBMaxOpenConns,
		},
		{
			name: "negative max idle",
			env:  map[string]string{envDBMaxIdleConns: "-1"},
			want: envDBMaxIdleConns,
		},
		{
			name: "idle exceeds open",
			env: map[string]string{
				envDBMaxOpenConns: "5",
				envDBMaxIdleConns: "6",
			},
			want: envDBMaxIdleConns,
		},
		{
			name: "bad lifetime",
			env:  map[string]string{envDBConnMaxLifetime: "soon"},
			want: envDBConnMaxLifetime,
		},
		{
			name: "negative idle time",
			env:  map[string]string{envDBConnMaxIdleTime: "-1s"},
			want: envDBConnMaxIdleTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDBPoolConfig(envMap(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to mention %s", err.Error(), tt.want)
			}
		})
	}
}

func TestSanitizeDatabaseErrorRedactsDSNAndPassword(t *testing.T) {
	dsn := "postgres://user:secret@localhost:5432/lenzcore?sslmode=disable"
	got := sanitizeDatabaseError(errors.New("failed for "+dsn+" password secret"), dsn)

	if strings.Contains(got, dsn) || strings.Contains(got, "secret") {
		t.Fatalf("expected sanitized error, got %q", got)
	}
}

func envMap(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}
