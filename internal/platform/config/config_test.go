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
		"TIERL_IP_HASH_SALT":     "salt",

		"TIERL_RATELIMIT_CREATE_TIERLIST_BURST":    "3",
		"TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL": "2m",
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
	if cfg.IPHashSalt != "salt" || cfg.CreateTierlistBurst != 3 || cfg.CreateTierlistInterval != 2*time.Minute {
		t.Errorf("unexpected rate limit cfg: %+v", cfg)
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CreateTierlistBurst != 10 || cfg.CreateTierlistInterval != 30*time.Second {
		t.Errorf("defaults = %d / %v", cfg.CreateTierlistBurst, cfg.CreateTierlistInterval)
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
		"empty salt with db":   {"TIERL_DATABASE_URL": "postgres://x"},
		"blank salt with db":   {"TIERL_DATABASE_URL": "postgres://x", "TIERL_IP_HASH_SALT": "  "},
		"bad burst":            {"TIERL_RATELIMIT_CREATE_TIERLIST_BURST": "many"},
		"zero burst":           {"TIERL_RATELIMIT_CREATE_TIERLIST_BURST": "0"},
		"bad interval":         {"TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL": "soon"},
		"zero interval":        {"TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL": "0s"},
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

func TestLoad_SessionAndAuthDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 720*time.Hour || cfg.SessionMaxAge != 2160*time.Hour {
		t.Errorf("session lifetimes = %v / %v", cfg.SessionTTL, cfg.SessionMaxAge)
	}
	if !cfg.CookieSecure || cfg.PublicBaseURL != "http://localhost:8080" || cfg.CSRFKey != "" {
		t.Errorf("cookie/csrf defaults = %+v", cfg)
	}
	if cfg.AuthBurst != 10 || cfg.AuthInterval != 30*time.Second {
		t.Errorf("auth limits = %d / %v", cfg.AuthBurst, cfg.AuthInterval)
	}
	if cfg.Env != "production" || cfg.MailDev || len(cfg.TrustedProxies) != 0 || cfg.SessionGCInterval != time.Hour {
		t.Errorf("env/proxy/gc defaults = %+v", cfg)
	}
	if cfg.DevMailEnabled() {
		t.Error("development mailer must be off by default")
	}
}

func TestLoad_SessionAndAuthOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TIERL_DATABASE_URL":            "postgres://x",
		"TIERL_IP_HASH_SALT":            "salt",
		"TIERL_SESSION_TTL":             "2h",
		"TIERL_SESSION_MAX_AGE":         "48h",
		"TIERL_COOKIE_SECURE":           "false",
		"TIERL_PUBLIC_BASE_URL":         "https://tierl.example",
		"TIERL_CSRF_KEY":                "0123456789abcdef",
		"TIERL_RATELIMIT_AUTH_BURST":    "4",
		"TIERL_RATELIMIT_AUTH_INTERVAL": "15s",
		"TIERL_SESSION_GC_INTERVAL":     "5m",
		"TIERL_ENV":                     "dev",
		"TIERL_TRUSTED_PROXIES":         "10.0.0.0/8, 203.0.113.7",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 2*time.Hour || cfg.SessionMaxAge != 48*time.Hour || cfg.CookieSecure {
		t.Errorf("session = %+v", cfg)
	}
	if cfg.PublicBaseURL != "https://tierl.example" || cfg.CSRFKey != "0123456789abcdef" {
		t.Errorf("url/csrf = %+v", cfg)
	}
	if cfg.AuthBurst != 4 || cfg.AuthInterval != 15*time.Second {
		t.Errorf("auth limits = %d / %v", cfg.AuthBurst, cfg.AuthInterval)
	}
	if cfg.SessionGCInterval != 5*time.Minute || !cfg.DevMailEnabled() {
		t.Errorf("gc/env = %v / %+v", cfg.SessionGCInterval, cfg)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "10.0.0.0/8" || cfg.TrustedProxies[1] != "203.0.113.7" {
		t.Errorf("trusted proxies = %v", cfg.TrustedProxies)
	}
}

func TestLoad_DevMailEnabled(t *testing.T) {
	for name, tt := range map[string]struct {
		vars map[string]string
		want bool
	}{
		"production default": {nil, false},
		"explicit env":       {map[string]string{"TIERL_ENV": "dev"}, true},
		"dev upper case":     {map[string]string{"TIERL_ENV": " DEV "}, true},
		"mail dev flag":      {map[string]string{"TIERL_MAIL_DEV": "true"}, true},
		"other env":          {map[string]string{"TIERL_ENV": "staging"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(env(tt.vars))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.DevMailEnabled(); got != tt.want {
				t.Errorf("DevMailEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_SessionAndAuthInvalid(t *testing.T) {
	tests := map[string]map[string]string{
		"bad session ttl":    {"TIERL_SESSION_TTL": "soon"},
		"zero session ttl":   {"TIERL_SESSION_TTL": "0s"},
		"bad max age":        {"TIERL_SESSION_MAX_AGE": "soon"},
		"max age below ttl":  {"TIERL_SESSION_TTL": "2h", "TIERL_SESSION_MAX_AGE": "1h"},
		"bad cookie secure":  {"TIERL_COOKIE_SECURE": "maybe"},
		"short csrf key":     {"TIERL_CSRF_KEY": "short"},
		"bad auth burst":     {"TIERL_RATELIMIT_AUTH_BURST": "many"},
		"zero auth burst":    {"TIERL_RATELIMIT_AUTH_BURST": "0"},
		"bad auth interval":  {"TIERL_RATELIMIT_AUTH_INTERVAL": "soon"},
		"zero auth interval": {"TIERL_RATELIMIT_AUTH_INTERVAL": "0s"},
		"bad mail dev":       {"TIERL_MAIL_DEV": "maybe"},
		"bad gc interval":    {"TIERL_SESSION_GC_INTERVAL": "soon"},
		"zero gc interval":   {"TIERL_SESSION_GC_INTERVAL": "0s"},
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(vars)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestLoad_Storage(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Type != StorageFS || cfg.Storage.FSPath != "./data/media" || cfg.Storage.PublicURL != "/media" {
		t.Errorf("defaults: %+v", cfg.Storage)
	}

	cfg, err = Load(env(map[string]string{
		"TIERL_STORAGE_TYPE": "S3", "TIERL_S3_ENDPOINT": "minio:9000", "TIERL_S3_BUCKET": "b",
		"TIERL_S3_ACCESS_KEY": "a", "TIERL_S3_SECRET_KEY": "s", "TIERL_S3_USE_SSL": "true", "TIERL_S3_REGION": "eu",
	}))
	if err != nil {
		t.Fatal(err)
	}
	st := cfg.Storage
	if st.Type != StorageS3 || st.S3Endpoint != "minio:9000" || st.S3Bucket != "b" ||
		st.S3AccessKey != "a" || st.S3SecretKey != "s" || !st.S3UseSSL || st.S3Region != "eu" {
		t.Errorf("s3: %+v", st)
	}
}

func TestLoad_StorageInvalid(t *testing.T) {
	full := map[string]string{
		"TIERL_STORAGE_TYPE": "s3", "TIERL_S3_ENDPOINT": "e", "TIERL_S3_BUCKET": "b",
		"TIERL_S3_ACCESS_KEY": "a", "TIERL_S3_SECRET_KEY": "s",
	}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range full {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	tests := map[string]map[string]string{
		"unknown type":  {"TIERL_STORAGE_TYPE": "ftp"},
		"bad ssl":       with("TIERL_S3_USE_SSL", "maybe"),
		"no endpoint":   with("TIERL_S3_ENDPOINT", ""),
		"no bucket":     with("TIERL_S3_BUCKET", ""),
		"no access key": with("TIERL_S3_ACCESS_KEY", ""),
		"no secret key": with("TIERL_S3_SECRET_KEY", ""),
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(vars)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
