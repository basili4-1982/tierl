// Package config loads application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
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

	defaultAuthEmailBurst    = 5
	defaultAuthEmailInterval = time.Minute

	defaultStorageFSPath = "./data/media"
	defaultStoragePublic = "/media"

	defaultAppBaseURL  = "http://localhost:8080"
	defaultSMTPPort    = 587
	defaultSMTPFrom    = "Tierl <no-reply@tierl.local>"
	defaultSMTPTimeout = 10 * time.Second
	defaultMailWorkers = 2
	defaultMailQueue   = 128
	defaultMailLang    = "ru"
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
	// AuthEmailBurst and AuthEmailInterval limit resend-verification and
	// forgot-password requests per IP (TIERL_RATELIMIT_AUTH_EMAIL_*,
	// defaults 5 per minute).
	AuthEmailBurst    int
	AuthEmailInterval time.Duration

	// AppBaseURL is the public origin of the frontend, used in email links
	// (TIERL_APP_BASE_URL, default http://localhost:8080).
	AppBaseURL string
	// Mail configures transactional email (TIERL_SMTP_*, TIERL_MAIL_*).
	Mail MailConfig

	Storage StorageConfig
}

// MailConfig configures transactional email.
type MailConfig struct {
	// Host is the SMTP server; empty selects the log sender (TIERL_SMTP_HOST).
	Host string
	// Port is the SMTP port (TIERL_SMTP_PORT, default 587).
	Port int
	// Username and Password enable PLAIN auth when Username is set
	// (TIERL_SMTP_USERNAME, TIERL_SMTP_PASSWORD).
	Username string
	Password string
	// From is the envelope sender (TIERL_SMTP_FROM).
	From string
	// TLS requires the server to offer STARTTLS; a plaintext fallback is an
	// error (TIERL_SMTP_TLS, default true).
	TLS bool
	// Timeout bounds one delivery (TIERL_SMTP_TIMEOUT, default 10s).
	Timeout time.Duration
	// Workers is the number of asynchronous delivery goroutines
	// (TIERL_MAIL_WORKERS, default 2).
	Workers int
	// QueueSize is the buffer between HTTP handlers and workers
	// (TIERL_MAIL_QUEUE, default 128).
	QueueSize int
	// Lang is the letter language: ru (default) or en (TIERL_MAIL_LANG).
	Lang string
}

// Storage backends.
const (
	StorageFS = "fs"
	StorageS3 = "s3"
)

// StorageConfig selects and configures the image storage (internal/media).
type StorageConfig struct {
	// Type is "fs" (default) or "s3" (TIERL_STORAGE_TYPE).
	Type string
	// FSPath is the directory for the fs backend (TIERL_STORAGE_FS_PATH).
	FSPath string
	// PublicURL is the base URL images are served from (TIERL_STORAGE_PUBLIC_URL).
	PublicURL string

	S3Endpoint  string // TIERL_S3_ENDPOINT, host:port without scheme
	S3Bucket    string // TIERL_S3_BUCKET
	S3AccessKey string // TIERL_S3_ACCESS_KEY
	S3SecretKey string // TIERL_S3_SECRET_KEY
	S3Region    string // TIERL_S3_REGION
	S3UseSSL    bool   // TIERL_S3_USE_SSL
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
		AuthEmailBurst:         defaultAuthEmailBurst,
		AuthEmailInterval:      defaultAuthEmailInterval,
		AppBaseURL:             strings.TrimRight(valueOr(getenv("TIERL_APP_BASE_URL"), defaultAppBaseURL), "/"),
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
	if raw := strings.TrimSpace(getenv("TIERL_RATELIMIT_AUTH_EMAIL_BURST")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_RATELIMIT_AUTH_EMAIL_BURST: %w", err)
		}
		if n <= 0 {
			return Config{}, errors.New("TIERL_RATELIMIT_AUTH_EMAIL_BURST: must be positive")
		}
		cfg.AuthEmailBurst = n
	}
	if raw := strings.TrimSpace(getenv("TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL: %w", err)
		}
		if d <= 0 {
			return Config{}, errors.New("TIERL_RATELIMIT_AUTH_EMAIL_INTERVAL: must be positive")
		}
		cfg.AuthEmailInterval = d
	}

	if u, err := url.Parse(cfg.AppBaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, fmt.Errorf("TIERL_APP_BASE_URL: %q is not an absolute http(s) URL", cfg.AppBaseURL)
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

	st, err := loadStorage(getenv)
	if err != nil {
		return Config{}, err
	}
	cfg.Storage = st

	mailCfg, err := loadMail(getenv)
	if err != nil {
		return Config{}, err
	}
	cfg.Mail = mailCfg

	return cfg, nil
}

// loadMail reads the SMTP settings. An empty host is valid: email is then only
// logged (development mode without a mail server).
func loadMail(getenv func(string) string) (MailConfig, error) {
	m := MailConfig{
		Host:      strings.TrimSpace(getenv("TIERL_SMTP_HOST")),
		Port:      defaultSMTPPort,
		Username:  strings.TrimSpace(getenv("TIERL_SMTP_USERNAME")),
		Password:  getenv("TIERL_SMTP_PASSWORD"),
		From:      valueOr(getenv("TIERL_SMTP_FROM"), defaultSMTPFrom),
		TLS:       true,
		Timeout:   defaultSMTPTimeout,
		Workers:   defaultMailWorkers,
		QueueSize: defaultMailQueue,
		Lang:      strings.ToLower(valueOr(getenv("TIERL_MAIL_LANG"), defaultMailLang)),
	}
	if raw := strings.TrimSpace(getenv("TIERL_SMTP_PORT")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 65535 {
			return MailConfig{}, fmt.Errorf("TIERL_SMTP_PORT: %q is not a port number", raw)
		}
		m.Port = n
	}
	if raw := strings.TrimSpace(getenv("TIERL_SMTP_TLS")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return MailConfig{}, fmt.Errorf("TIERL_SMTP_TLS: %w", err)
		}
		m.TLS = b
	}
	if raw := strings.TrimSpace(getenv("TIERL_SMTP_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return MailConfig{}, fmt.Errorf("TIERL_SMTP_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return MailConfig{}, errors.New("TIERL_SMTP_TIMEOUT: must be positive")
		}
		m.Timeout = d
	}
	for _, v := range []struct {
		name string
		dst  *int
	}{
		{"TIERL_MAIL_WORKERS", &m.Workers},
		{"TIERL_MAIL_QUEUE", &m.QueueSize},
	} {
		raw := strings.TrimSpace(getenv(v.name))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return MailConfig{}, fmt.Errorf("%s: must be a positive integer", v.name)
		}
		*v.dst = n
	}
	if m.Lang != "ru" && m.Lang != "en" {
		return MailConfig{}, fmt.Errorf("TIERL_MAIL_LANG: %q is not ru or en", m.Lang)
	}
	if _, err := mail.ParseAddress(m.From); err != nil {
		return MailConfig{}, fmt.Errorf("TIERL_SMTP_FROM: %w", err)
	}
	return m, nil
}

func loadStorage(getenv func(string) string) (StorageConfig, error) {
	st := StorageConfig{
		Type:        strings.ToLower(strings.TrimSpace(valueOr(getenv("TIERL_STORAGE_TYPE"), StorageFS))),
		FSPath:      valueOr(getenv("TIERL_STORAGE_FS_PATH"), defaultStorageFSPath),
		PublicURL:   valueOr(getenv("TIERL_STORAGE_PUBLIC_URL"), defaultStoragePublic),
		S3Endpoint:  strings.TrimSpace(getenv("TIERL_S3_ENDPOINT")),
		S3Bucket:    strings.TrimSpace(getenv("TIERL_S3_BUCKET")),
		S3AccessKey: getenv("TIERL_S3_ACCESS_KEY"),
		S3SecretKey: getenv("TIERL_S3_SECRET_KEY"),
		S3Region:    strings.TrimSpace(getenv("TIERL_S3_REGION")),
	}
	if raw := strings.TrimSpace(getenv("TIERL_S3_USE_SSL")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return StorageConfig{}, fmt.Errorf("TIERL_S3_USE_SSL: %w", err)
		}
		st.S3UseSSL = b
	}
	switch st.Type {
	case StorageFS:
	case StorageS3:
		for name, v := range map[string]string{
			"TIERL_S3_ENDPOINT": st.S3Endpoint, "TIERL_S3_BUCKET": st.S3Bucket,
			"TIERL_S3_ACCESS_KEY": st.S3AccessKey, "TIERL_S3_SECRET_KEY": st.S3SecretKey,
		} {
			if v == "" {
				return StorageConfig{}, fmt.Errorf("%s: required when TIERL_STORAGE_TYPE=s3", name)
			}
		}
	default:
		return StorageConfig{}, fmt.Errorf("TIERL_STORAGE_TYPE: %q is not fs or s3", st.Type)
	}
	return st, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
