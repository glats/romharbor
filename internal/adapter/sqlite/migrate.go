package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	_ "github.com/amacneil/dbmate/v2/pkg/driver/sqlite"

	"app.ddd/internal/adapter/sqlite/migrations"
)

const (
	// schemaAttempts is how many times a migration is tried before the schema
	// is checked for another process that applied it.
	schemaAttempts = 5
	// schemaRetry is the wait between two attempts.
	schemaRetry = 200 * time.Millisecond
)

func newDBMate(path string) (*dbmate.DB, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	u, err := url.Parse("sqlite:" + path)
	if err != nil {
		return nil, fmt.Errorf("parse database path: %w", err)
	}

	db := dbmate.New(u)
	db.FS = migrations.Files
	db.MigrationsDir = []string{"."}
	db.AutoDumpSchema = false
	db.Log = io.Discard

	return db, nil
}

// Migrate and Rollback check ctx before they start; dbmate cannot interrupt a
// migration that is already running, and doing so would leave it half applied.
func Migrate(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	db, err := newDBMate(path)
	if err != nil {
		return err
	}

	if err := db.Migrate(); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

func Rollback(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	db, err := newDBMate(path)
	if err != nil {
		return err
	}

	if err := db.Rollback(); err != nil {
		return fmt.Errorf("roll back migration: %w", err)
	}

	return nil
}

// EnsureSchema applies the pending migrations and waits for the schema to be
// in place. The web server and the download worker may apply the same
// migrations at the same time; the process that loses that race waits for the
// winner instead of failing.
func EnsureSchema(ctx context.Context, path string) error {
	var err error
	for attempt := range schemaAttempts {
		if attempt > 0 {
			if waitErr := wait(ctx, schemaRetry); waitErr != nil {
				return waitErr
			}
		}
		if err = Migrate(ctx, path); err == nil {
			return nil
		}
	}

	ready, readyErr := SchemaReady(ctx, path)
	if readyErr == nil && ready {
		return nil
	}

	return err
}

// SchemaReady reports whether the database holds every embedded migration.
func SchemaReady(ctx context.Context, path string) (bool, error) {
	versions, err := migrationVersions()
	if err != nil {
		return false, err
	}
	if len(versions) == 0 {
		return false, nil
	}

	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		return false, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var tables int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tables); err != nil {
		return false, fmt.Errorf("read the applied migrations: %w", err)
	}
	if tables == 0 {
		return false, nil
	}

	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return false, fmt.Errorf("read the applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return false, fmt.Errorf("read the applied migrations: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read the applied migrations: %w", err)
	}

	for _, version := range versions {
		if !applied[version] {
			return false, nil
		}
	}

	return true, nil
}

// migrationVersions returns the version of every embedded migration, the same
// version dbmate records in the schema migrations table.
func migrationVersions() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("read the embedded migrations: %w", err)
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := leadingDigits(entry.Name())
		if version == "" {
			continue
		}
		versions = append(versions, version)
	}

	return versions, nil
}

func leadingDigits(name string) string {
	end := 0
	for end < len(name) && name[end] >= '0' && name[end] <= '9' {
		end++
	}
	return name[:end]
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
