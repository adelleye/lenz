package config

import (
	"fmt"
	"lenz-core/packages/shared/utils"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

const (
	driverName = "postgres"

	envDBMaxOpenConns    = "DB_MAX_OPEN_CONNS"
	envDBMaxIdleConns    = "DB_MAX_IDLE_CONNS"
	envDBConnMaxLifetime = "DB_CONN_MAX_LIFETIME"
	envDBConnMaxIdleTime = "DB_CONN_MAX_IDLE_TIME"

	defaultDBMaxOpenConns    = 5
	defaultDBMaxIdleConns    = 2
	defaultDBConnMaxLifetime = 30 * time.Minute
	defaultDBConnMaxIdleTime = 5 * time.Minute
)

type DB struct {
	DBConn *sqlx.DB
}

type dbPoolConfig struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

func NewDB() (DB, error) {
	vp := viper.New()
	vp.AutomaticEnv()

	dataSourceName := vp.GetString(utils.EnvDatabaseURL)
	if dataSourceName == "" {
		dataSourceName = "postgres://lenzcore:lenzcore123@localhost:5432/lenzcore?sslmode=disable"
		log.Printf("DATABASE_URL not set, using local Docker Compose default")
	}

	poolConfig, err := parseDBPoolConfig(vp.GetString)
	if err != nil {
		return DB{}, err
	}

	db, err := sqlx.Connect(driverName, dataSourceName)
	if err != nil {
		return DB{}, fmt.Errorf("connect database: %s", sanitizeDatabaseError(err, dataSourceName))
	}

	db.SetMaxOpenConns(poolConfig.maxOpenConns)
	db.SetMaxIdleConns(poolConfig.maxIdleConns)
	db.SetConnMaxLifetime(poolConfig.connMaxLifetime)
	db.SetConnMaxIdleTime(poolConfig.connMaxIdleTime)

	return DB{DBConn: db}, nil
}

func parseDBPoolConfig(getenv func(string) string) (dbPoolConfig, error) {
	cfg := dbPoolConfig{
		maxOpenConns:    defaultDBMaxOpenConns,
		maxIdleConns:    defaultDBMaxIdleConns,
		connMaxLifetime: defaultDBConnMaxLifetime,
		connMaxIdleTime: defaultDBConnMaxIdleTime,
	}

	var err error
	if cfg.maxOpenConns, err = parseIntEnv(getenv, envDBMaxOpenConns, cfg.maxOpenConns, 1); err != nil {
		return dbPoolConfig{}, err
	}
	if cfg.maxIdleConns, err = parseIntEnv(getenv, envDBMaxIdleConns, cfg.maxIdleConns, 0); err != nil {
		return dbPoolConfig{}, err
	}
	if cfg.maxIdleConns > cfg.maxOpenConns {
		return dbPoolConfig{}, fmt.Errorf("%s must be less than or equal to %s", envDBMaxIdleConns, envDBMaxOpenConns)
	}
	if cfg.connMaxLifetime, err = parseDurationEnv(getenv, envDBConnMaxLifetime, cfg.connMaxLifetime); err != nil {
		return dbPoolConfig{}, err
	}
	if cfg.connMaxIdleTime, err = parseDurationEnv(getenv, envDBConnMaxIdleTime, cfg.connMaxIdleTime); err != nil {
		return dbPoolConfig{}, err
	}

	return cfg, nil
}

func parseIntEnv(getenv func(string) string, name string, fallback int, min int) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < min {
		return 0, fmt.Errorf("%s must be an integer >= %d", name, min)
	}
	return value, nil
}

func parseDurationEnv(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative duration like 30m or 1h", name)
	}
	return value, nil
}

func sanitizeDatabaseError(err error, dataSourceName string) string {
	if err == nil {
		return ""
	}

	message := err.Error()
	if dataSourceName == "" {
		return message
	}

	message = strings.ReplaceAll(message, dataSourceName, "[redacted DATABASE_URL]")
	if dsn, err := url.Parse(dataSourceName); err == nil && dsn.User != nil {
		if password, ok := dsn.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
		}
	}
	return message
}
