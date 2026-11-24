package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	HTTP     HTTPConfig     `envPrefix:"HTTP_"`
	Database DatabaseConfig `envPrefix:"DATABASE_"`
	Log      LogConfig      `envPrefix:"LOG_"`
	Seed     SeedConfig     `envPrefix:"DB_SEED_"`
	ROM      ROMConfig      `envPrefix:"ROM_"`
	Jobs     JobsConfig     `envPrefix:"JOBS_"`
}

type HTTPConfig struct {
	Addr string `env:"ADDR" envDefault:":8080"`
}

type DatabaseConfig struct {
	Path string `env:"PATH" envDefault:"./data/app.db"`
}

type LogConfig struct {
	Level  string `env:"LEVEL" envDefault:"info"`
	Pretty bool   `env:"PRETTY" envDefault:"false"`
}

type SeedConfig struct {
	Enabled bool `env:"ENABLED" envDefault:"false"`
}

type ROMConfig struct {
	Library  string        `env:"LIBRARY_PATH" envDefault:"./data/roms"`
	Cache    string        `env:"CACHE_PATH" envDefault:"./data/romcache"`
	Sources  string        `env:"SOURCES" envDefault:"archive.org"`
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"24h"`
}

type JobsConfig struct {
	MaxAttempts int           `env:"MAX_ATTEMPTS" envDefault:"3"`
	RetryDelay  time.Duration `env:"RETRY_DELAY" envDefault:"30s"`
	Retention   time.Duration `env:"RETENTION" envDefault:"720h"`
	Timeout     time.Duration `env:"TIMEOUT" envDefault:"6h"`
}

func Parse() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}
	if err := cfg.Jobs.validate(); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}

	return cfg, nil
}

func (j JobsConfig) validate() error {
	if j.MaxAttempts < 1 {
		return errors.New("JOBS_MAX_ATTEMPTS must be at least 1")
	}
	for name, value := range map[string]time.Duration{
		"JOBS_RETRY_DELAY": j.RetryDelay,
		"JOBS_RETENTION":   j.Retention,
		"JOBS_TIMEOUT":     j.Timeout,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}

	return nil
}

func Get() Config {
	cfg, err := Parse()
	if err != nil {
		panic(err)
	}

	return cfg
}
