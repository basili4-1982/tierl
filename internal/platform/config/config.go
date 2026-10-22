// Package config loads application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second

	defaultCreateTierlistBurst    = 10
	defaultCreateTierlistInterval = 30 * time.Second
)

// Config holds all runtime settings of the service.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	// MigrateOnStart applies pending migrations at startup (TIERL_MIGRATE_ON_START, default false).
	MigrateOnStart  bool
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	// Argon2 cost parameters for password hashing (ADR-003).
	Argon2MemoryKiB   uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8

	// IPHashSalt salts the IP hash used as a rate limit key (TIERL_IP_HASH_SALT).
	// It is required whenever the database is enabled.
	IPHashSalt string
	// CreateTierlistBurst is how many tier lists one IP may create in a row
	// (TIERL_RATELIMIT_CREATE_TIERLIST_BURST, default 10).
	CreateTierlistBurst int
	// CreateTierlistInterval is the refill time of one creation token
	// (TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL, default 30s).
	CreateTierlistInterval time.Duration
}

// Load reads settings through getenv (os.Getenv in production) so tests need no real environment.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:        valueOr(getenv("TIERL_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseURL:     getenv("TIERL_DATABASE_URL"),
		ShutdownTimeout: defaultShutdownTimeout,
		// Keep in sync with auth.DefaultHashParams (config must not import auth).
		Argon2MemoryKiB:   64 * 1024,
		Argon2Iterations:  3,
		Argon2Parallelism: 2,

		IPHashSalt:             getenv("TIERL_IP_HASH_SALT"),
		CreateTierlistBurst:    defaultCreateTierlistBurst,
		CreateTierlistInterval: defaultCreateTierlistInterval,
	}

	for _, v := range []struct {
		name string
		bits int
		set  func(uint64)
	}{
		{"TIERL_ARGON2_MEMORY_KIB", 32, func(n uint64) { cfg.Argon2MemoryKiB = uint32(n) }},  //nolint:gosec // ParseUint bitSize bounds n
		{"TIERL_ARGON2_ITERATIONS", 32, func(n uint64) { cfg.Argon2Iterations = uint32(n) }}, //nolint:gosec // ParseUint bitSize bounds n
		{"TIERL_ARGON2_PARALLELISM", 8, func(n uint64) { cfg.Argon2Parallelism = uint8(n) }}, //nolint:gosec // ParseUint bitSize bounds n
	} {
		raw := strings.TrimSpace(getenv(v.name))
		if raw == "" {
			continue
		}
		n, err := strconv.ParseUint(raw, 10, v.bits)
		if err != nil || n == 0 {
			return Config{}, fmt.Errorf("%s: must be a positive integer that fits %d bits", v.name, v.bits)
		}
		v.set(n)
	}

	if raw := strings.TrimSpace(getenv("TIERL_RATELIMIT_CREATE_TIERLIST_BURST")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_RATELIMIT_CREATE_TIERLIST_BURST: %w", err)
		}
		if n <= 0 {
			return Config{}, errors.New("TIERL_RATELIMIT_CREATE_TIERLIST_BURST: must be positive")
		}
		cfg.CreateTierlistBurst = n
	}
	if raw := strings.TrimSpace(getenv("TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL: %w", err)
		}
		if d <= 0 {
			return Config{}, errors.New("TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL: must be positive")
		}
		cfg.CreateTierlistInterval = d
	}

	if raw := strings.TrimSpace(getenv("TIERL_MIGRATE_ON_START")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_MIGRATE_ON_START: %w", err)
		}
		cfg.MigrateOnStart = b
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(valueOr(getenv("TIERL_LOG_LEVEL"), defaultLogLevel))); err != nil {
		return Config{}, fmt.Errorf("TIERL_LOG_LEVEL: %w", err)
	}
	cfg.LogLevel = level

	if raw := getenv("TIERL_SHUTDOWN_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_SHUTDOWN_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return Config{}, errors.New("TIERL_SHUTDOWN_TIMEOUT: must be positive")
		}
		cfg.ShutdownTimeout = d
	}

	if cfg.DatabaseURL != "" && strings.TrimSpace(cfg.IPHashSalt) == "" {
		return Config{}, errors.New("TIERL_IP_HASH_SALT: required when TIERL_DATABASE_URL is set")
	}

	return cfg, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
