package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tableNames(t *testing.T, path string) []string {
	t.Helper()

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func appliedMigrations(t *testing.T, path string) int {
	t.Helper()

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count))
	return count
}

func TestMigrateCreatesTheSchemaAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app.db")

	require.NoError(t, Migrate(t.Context(), path))
	require.NoError(t, Migrate(t.Context(), path))

	assert.Equal(t, []string{"jobs", "schema_migrations", "users"}, tableNames(t, path))
	assert.Equal(t, 2, appliedMigrations(t, path))
}

func TestRollbackRemovesTheLastMigrationAndMigrateRestoresIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, Migrate(t.Context(), path))

	require.NoError(t, Rollback(t.Context(), path))
	assert.Equal(t, []string{"schema_migrations", "users"}, tableNames(t, path))

	require.NoError(t, Migrate(t.Context(), path))
	assert.Equal(t, []string{"jobs", "schema_migrations", "users"}, tableNames(t, path))
}

func TestRollbackFailsWhenNothingIsApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, Migrate(t.Context(), path))
	require.NoError(t, Rollback(t.Context(), path))
	require.NoError(t, Rollback(t.Context(), path))

	assert.Error(t, Rollback(t.Context(), path))
}

func TestMigrateFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	assert.Error(t, Migrate(t.Context(), filepath.Join(blocker, "app.db")))
}

func TestMigrateAndRollbackFailWhenTheContextIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "sub", "app.db")

	require.ErrorIs(t, Migrate(ctx, path), context.Canceled)
	require.ErrorIs(t, Rollback(ctx, path), context.Canceled)
	assert.NoDirExists(t, filepath.Dir(path))
}
