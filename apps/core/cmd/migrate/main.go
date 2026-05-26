package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

const localDockerComposeDSN = "postgres://lenzcore:lenzcore123@localhost:5432/lenzcore?sslmode=disable" // #nosec G101 -- local Docker Compose default; production must set DATABASE_URL.

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = localDockerComposeDSN
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		log.Fatal(err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		// #nosec G706 -- migration directory text is sanitized before logging.
		log.Fatalf("no .up.sql migrations found in %s", safeLogValue(dir))
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
			log.Fatal(err)
		}
		if exists {
			fmt.Printf("skip %s\n", version)
			continue
		}
		body, err := os.ReadFile(file) // #nosec G304,G703 -- migrations are operator-selected trusted deployment artifacts.
		if err != nil {
			log.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			log.Fatal(err)
		}
		if _, err := tx.Exec(string(body)); err != nil { // #nosec G701 -- migration SQL is a trusted deployment artifact, not user input.
			_ = tx.Rollback()
			// #nosec G706 -- migration version text is sanitized before logging.
			log.Fatalf("apply %s: %v", safeLogValue(version), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			log.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("applied %s\n", version)
	}
}

func safeLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}
