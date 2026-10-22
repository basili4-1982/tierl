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

	return cfg, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
