package config

import (
	"log/slog"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TIERL_HTTP_ADDR":        "127.0.0.1:9000",
		"TIERL_DATABASE_URL":     "postgres://x",
		"TIERL_LOG_LEVEL":        "debug",
		"TIERL_SHUTDOWN_TIMEOUT": "3s",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.DatabaseURL != "postgres://x" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := map[string]map[string]string{
		"bad log level":        {"TIERL_LOG_LEVEL": "loud"},
		"bad timeout":          {"TIERL_SHUTDOWN_TIMEOUT": "soon"},
		"non-positive timeout": {"TIERL_SHUTDOWN_TIMEOUT": "0s"},
		"bad argon2 memory":    {"TIERL_ARGON2_MEMORY_KIB": "lots"},
		"zero argon2 iters":    {"TIERL_ARGON2_ITERATIONS": "0"},
		"huge argon2 threads":  {"TIERL_ARGON2_PARALLELISM": "300"},
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(vars)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestLoad_MigrateOnStart(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.MigrateOnStart {
		t.Fatalf("default: %v %+v", err, cfg)
	}
	cfg, err = Load(env(map[string]string{"TIERL_MIGRATE_ON_START": "true"}))
	if err != nil || !cfg.MigrateOnStart {
		t.Fatalf("true: %v %+v", err, cfg)
	}
	if _, err = Load(env(map[string]string{"TIERL_MIGRATE_ON_START": "maybe"})); err == nil {
		t.Fatal("want error for invalid value")
	}
}

func TestLoad_Argon2(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Argon2MemoryKiB != 65536 || cfg.Argon2Iterations != 3 || cfg.Argon2Parallelism != 2 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	cfg, err = Load(env(map[string]string{
		"TIERL_ARGON2_MEMORY_KIB": "19456", "TIERL_ARGON2_ITERATIONS": "2", "TIERL_ARGON2_PARALLELISM": "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Argon2MemoryKiB != 19456 || cfg.Argon2Iterations != 2 || cfg.Argon2Parallelism != 1 {
		t.Errorf("unexpected overrides: %+v", cfg)
	}
}
