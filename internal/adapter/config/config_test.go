package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
}

func TestParseUsesPortableROMDefaults(t *testing.T) {
	unsetEnv(t, "ROM_LIBRARY_PATH", "ROM_CACHE_PATH", "ROM_SOURCES", "ROM_CACHE_TTL")

	cfg, err := Parse()

	require.NoError(t, err)
	assert.Equal(t, "./data/roms", cfg.ROM.Library)
	assert.Equal(t, "./data/romcache", cfg.ROM.Cache)
	assert.Equal(t, "archive.org", cfg.ROM.Sources)
	assert.Equal(t, 24*time.Hour, cfg.ROM.CacheTTL)
}

func TestParseUsesDefaults(t *testing.T) {
	unsetEnv(t, "HTTP_ADDR", "DATABASE_PATH", "LOG_LEVEL", "LOG_PRETTY", "DB_SEED_ENABLED")

	cfg, err := Parse()

	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.HTTP.Addr)
	assert.Equal(t, "./data/app.db", cfg.Database.Path)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
	assert.False(t, cfg.Seed.Enabled)
}

func TestParseReadsTheEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("DATABASE_PATH", "/tmp/users.db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_PRETTY", "true")
	t.Setenv("DB_SEED_ENABLED", "true")

	cfg, err := Parse()

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9000", cfg.HTTP.Addr)
	assert.Equal(t, "/tmp/users.db", cfg.Database.Path)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.Pretty)
	assert.True(t, cfg.Seed.Enabled)
}

func TestParseRejectsInvalidBooleanSettings(t *testing.T) {
	for _, key := range []string{"LOG_PRETTY", "DB_SEED_ENABLED"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "invalid")
			_, err := Parse()
			assert.ErrorContains(t, err, "invalid")
		})
	}
}

func TestGetReturnsTheParsedConfiguration(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":7000")
	unsetEnv(t, "DATABASE_PATH")

	cfg := Get()

	assert.Equal(t, ":7000", cfg.HTTP.Addr)
	assert.Equal(t, "./data/app.db", cfg.Database.Path)
}

func TestParseUsesTheJobDefaults(t *testing.T) {
	unsetEnv(t, "JOBS_MAX_ATTEMPTS", "JOBS_RETRY_DELAY", "JOBS_RETENTION", "JOBS_TIMEOUT")

	cfg, err := Parse()

	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Jobs.MaxAttempts)
	assert.Equal(t, 30*time.Second, cfg.Jobs.RetryDelay)
	assert.Equal(t, 30*24*time.Hour, cfg.Jobs.Retention)
	assert.Equal(t, 6*time.Hour, cfg.Jobs.Timeout)
}

func TestParseReadsTheJobSettings(t *testing.T) {
	t.Setenv("JOBS_MAX_ATTEMPTS", "5")
	t.Setenv("JOBS_RETRY_DELAY", "1m")
	t.Setenv("JOBS_RETENTION", "48h")
	t.Setenv("JOBS_TIMEOUT", "2h")

	cfg, err := Parse()

	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Jobs.MaxAttempts)
	assert.Equal(t, time.Minute, cfg.Jobs.RetryDelay)
	assert.Equal(t, 48*time.Hour, cfg.Jobs.Retention)
	assert.Equal(t, 2*time.Hour, cfg.Jobs.Timeout)
}

func TestParseRejectsJobSettingsThatCannotWork(t *testing.T) {
	tests := map[string]string{
		"JOBS_MAX_ATTEMPTS": "0",
		"JOBS_RETRY_DELAY":  "0s",
		"JOBS_RETENTION":    "-1h",
		"JOBS_TIMEOUT":      "0s",
	}
	for key, value := range tests {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			_, err := Parse()
			assert.ErrorContains(t, err, key)
		})
	}
}
