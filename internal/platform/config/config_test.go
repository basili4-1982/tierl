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

func TestLoad_Mail(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppBaseURL != "http://localhost:8080" {
		t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
	}
	m := cfg.Mail
	if m.Host != "" || m.Port != 587 || m.From != "Tierl <no-reply@tierl.local>" ||
		!m.TLS || m.Timeout != 10*time.Second || m.Lang != "ru" {
		t.Errorf("mail defaults: %+v", m)
	}
	if cfg.AuthEmailBurst != 5 || cfg.AuthEmailInterval != time.Minute {
		t.Errorf("auth email limits: %d / %v", cfg.AuthEmailBurst, cfg.AuthEmailInterval)
	}

	cfg, err = Load(env(map[string]string{
		"TIERL_APP_BASE_URL":                  "https://tierl.example/",
		"TIERL_SMTP_HOST":                     "mailpit",
		"TIERL_SMTP_PORT":                     "1025",
		"TIERL_SMTP_USERNAME":                 "u",
		"TIERL_SMTP_PASSWORD":                 "p",
		"TIERL_SMTP_FROM":                     "Tierl <hi@example.com>",
		"TIERL_SMTP_TLS":                      "false",
		"TIERL_SMTP_TIMEOUT":                  "3s",
		"TIERL_MAIL_LANG":                     "EN",
		"TIERL_RATELIMIT_AUTH_EMAIL_BURST":    "2",
		"TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL": "90s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppBaseURL != "https://tierl.example" {
		t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
	}
	m = cfg.Mail
	if m.Host != "mailpit" || m.Port != 1025 || m.Username != "u" || m.Password != "p" ||
		m.From != "Tierl <hi@example.com>" || m.TLS || m.Timeout != 3*time.Second || m.Lang != "en" {
		t.Errorf("mail overrides: %+v", m)
	}
	if cfg.AuthEmailBurst != 2 || cfg.AuthEmailInterval != 90*time.Second {
		t.Errorf("auth email limits: %d / %v", cfg.AuthEmailBurst, cfg.AuthEmailInterval)
	}
}

func TestLoad_MailInvalid(t *testing.T) {
	tests := map[string]map[string]string{
		"relative base url":   {"TIERL_APP_BASE_URL": "/relative"},
		"bad base scheme":     {"TIERL_APP_BASE_URL": "ftp://example.com"},
		"bad smtp port":       {"TIERL_SMTP_PORT": "0"},
		"huge smtp port":      {"TIERL_SMTP_PORT": "70000"},
		"bad smtp tls":        {"TIERL_SMTP_TLS": "maybe"},
		"bad smtp timeout":    {"TIERL_SMTP_TIMEOUT": "soon"},
		"zero smtp timeout":   {"TIERL_SMTP_TIMEOUT": "0s"},
		"bad from":            {"TIERL_SMTP_FROM": "not-an-address"},
		"bad lang":            {"TIERL_MAIL_LANG": "de"},
		"bad email burst":     {"TIERL_RATELIMIT_AUTH_EMAIL_BURST": "many"},
		"zero email burst":    {"TIERL_RATELIMIT_AUTH_EMAIL_BURST": "0"},
		"bad email interval":  {"TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL": "soon"},
		"zero email interval": {"TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL": "0s"},
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(vars)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
