package sqlite

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoProcessesMigrateTheSameDatabaseAtTheSameTime(t *testing.T) {
	for round := range 5 {
		path := filepath.Join(t.TempDir(), "app.db")
		var ready sync.WaitGroup
		ready.Add(4)
		start := make(chan struct{})
		errs := make(chan error, 4)
		for range 4 {
			go func() {
				ready.Done()
				<-start
				errs <- EnsureSchema(t.Context(), path)
			}()
		}
		ready.Wait()
		close(start)

		for range 4 {
			assert.NoError(t, <-errs, "round %d: a concurrent migration failed", round)
		}
		assert.Equal(t, []string{"jobs", "schema_migrations", "users"}, tableNames(t, path))
	}
}

func TestSchemaReadyReportsTheAppliedMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	ready, err := SchemaReady(t.Context(), path)
	require.NoError(t, err)
	assert.False(t, ready, "an empty database holds no schema")

	require.NoError(t, EnsureSchema(t.Context(), path))

	ready, err = SchemaReady(t.Context(), path)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestEnsureSchemaFailsWhenTheDatabaseCannotBePrepared(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	assert.Error(t, EnsureSchema(t.Context(), filepath.Join(blocker, "app.db")))
}
