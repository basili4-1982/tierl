package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/config"
	"github.com/basili4-1982/tierl/internal/platform/mailer"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/web"
	"github.com/basili4-1982/tierl/internal/web/api"
)

// setup opens the database and builds the HTTP dependencies. Without
// TIERL_DATABASE_URL the service serves only the static frontend and probes.
// The returned func releases resources and is always safe to call.
func setup(ctx context.Context, cfg config.Config, log *slog.Logger) (web.Deps, func(), error) {
	if cfg.DatabaseURL == "" {
		log.Warn("TIERL_DATABASE_URL is empty: the JSON API is disabled")
		return web.Deps{}, func() {}, nil
	}

	if err := postgres.MigrateOnStart(cfg.MigrateOnStart, cfg.DatabaseURL); err != nil {
		return web.Deps{}, nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return web.Deps{}, nil, fmt.Errorf("database: %w", err)
	}

	// TODO(#23): replace guestAuth with the session-based authenticator and
	// noUsers with the real user directory once the sessions module lands.
	// Email confirmation and password reset are already wired below.
	deps, err := newDeps(pool, cfg, log, guestAuth{}, noUsers{})
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	return deps, pool.Close, nil
}

// newDeps assembles the API handler on top of an open pool.
func newDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authn api.Authenticator, users tierlist.Users) (web.Deps, error) {
	limiter, err := ratelimit.NewMemory(ratelimit.Config{
		Burst:    cfg.CreateTierlistBurst,
		Interval: cfg.CreateTierlistInterval,
	})
	if err != nil {
		return web.Deps{}, fmt.Errorf("rate limiter: %w", err)
	}
	emailLimiter, err := ratelimit.NewMemory(ratelimit.Config{
		Burst:    emailBurst(cfg),
		Interval: emailInterval(cfg),
	})
	if err != nil {
		return web.Deps{}, fmt.Errorf("email rate limiter: %w", err)
	}

	accounts, err := newAccounts(pool, cfg, log)
	if err != nil {
		return web.Deps{}, err
	}

	service := tierlist.NewService(postgres.New(pool), users, tierlist.ServiceDeps{})
	handler := api.New(api.Deps{
		Tierlists: service,
		Auth:      authn,
		Accounts:  accounts,
		Log:       log,
		CreateTierlistLimit: ratelimit.Middleware(limiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "create_tierlist")),
		ResendVerificationLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "resend_verification")),
		ForgotPasswordLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "forgot_password")),
	})
	return web.Deps{API: handler, Ready: pool.Ping}, nil
}

// newAccounts builds the auth service with the one-time token store, mailer
// and session revoker.
func newAccounts(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger) (*auth.Service, error) {
	mail, err := mailer.NewSender(mailer.Config{
		Host:     cfg.Mail.Host,
		Port:     cfg.Mail.Port,
		Username: cfg.Mail.Username,
		Password: cfg.Mail.Password,
		From:     cfg.Mail.From,
		TLS:      cfg.Mail.TLS,
		Timeout:  cfg.Mail.Timeout,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("mailer: %w", err)
	}
	svc, err := auth.NewService(postgres.NewUserStore(pool), auth.Options{
		Hash: auth.HashParams{
			MemoryKiB:   cfg.Argon2MemoryKiB,
			Iterations:  cfg.Argon2Iterations,
			Parallelism: cfg.Argon2Parallelism,
		},
		Tokens:   postgres.NewTokenStore(pool),
		Mail:     mail,
		Sessions: postgres.NewSessionRevoker(pool),
		BaseURL:  cfg.AppBaseURL,
		Lang:     mailer.ParseLang(cfg.Mail.Lang),
		Log:      log,
	})
	if err != nil {
		return nil, fmt.Errorf("auth service: %w", err)
	}
	return svc, nil
}

// emailBurst and emailInterval apply defaults for configs built directly in
// tests, without config.Load.
func emailBurst(cfg config.Config) int {
	if cfg.AuthEmailBurst > 0 {
		return cfg.AuthEmailBurst
	}
	return 5
}

func emailInterval(cfg config.Config) time.Duration {
	if cfg.AuthEmailInterval > 0 {
		return cfg.AuthEmailInterval
	}
	return time.Minute
}

// guestAuth is a placeholder authenticator: nobody is signed in, so endpoints
// that need a user answer 401. TODO(#23): replace with session authentication.
type guestAuth struct{}

func (guestAuth) UserID(*http.Request) (string, bool) { return "", false }

// noUsers is a placeholder user directory. TODO(#23): replace with the auth module.
type noUsers struct{}

func (noUsers) Profile(context.Context, string) (tierlist.Profile, error) {
	return tierlist.Profile{}, tierlist.ErrNotFound
}
