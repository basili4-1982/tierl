package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/config"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/web"
	"github.com/basili4-1982/tierl/internal/web/api"
)

// fallback values for hand-built Config values in tests and embedders.
const (
	fallbackAuthBurst    = 10
	fallbackAuthInterval = 30 * time.Second
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
	closeAll := pool.Close

	services, err := newServices(pool, cfg, log)
	if err != nil {
		closeAll()
		return web.Deps{}, nil, err
	}
	deps, err := newDeps(pool, cfg, log, api.SessionAuthenticator{Sessions: services.sessions},
		userDirectory{auth: services.auth}, services)
	if err != nil {
		closeAll()
		return web.Deps{}, nil, err
	}
	return deps, closeAll, nil
}

// services are the auth components shared by the HTTP adapters.
type services struct {
	auth     *auth.Service
	sessions *auth.SessionService
	resets   *auth.PasswordResetService
}

// newServices builds the auth service, the session service and password reset
// on top of an open pool.
func newServices(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger) (services, error) {
	users := postgres.NewUserStore(pool)
	params := auth.HashParams{
		MemoryKiB:   cfg.Argon2MemoryKiB,
		Iterations:  cfg.Argon2Iterations,
		Parallelism: cfg.Argon2Parallelism,
	}
	authSvc, err := auth.NewService(users, auth.Options{Hash: params})
	if err != nil {
		return services{}, fmt.Errorf("auth service: %w", err)
	}
	sessions := auth.NewSessionService(postgres.NewSessionStore(pool), auth.SessionOptions{
		TTL:    cfg.SessionTTL,
		MaxAge: cfg.SessionMaxAge,
	})
	resets, err := auth.NewPasswordResetService(users, postgres.NewPasswordResetStore(pool), sessions,
		auth.ResetOptions{
			Hash:   params,
			Mailer: auth.LogMailer{Log: log, BaseURL: cfg.PublicBaseURL},
		})
	if err != nil {
		return services{}, fmt.Errorf("password reset service: %w", err)
	}
	return services{auth: authSvc, sessions: sessions, resets: resets}, nil
}

// newDeps assembles the API handler on top of an open pool.
func newDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authn api.Authenticator, users tierlist.Users, svc services) (web.Deps, error) {
	limiter, err := ratelimit.NewMemory(ratelimit.Config{
		Burst:    cfg.CreateTierlistBurst,
		Interval: cfg.CreateTierlistInterval,
	})
	if err != nil {
		return web.Deps{}, fmt.Errorf("rate limiter: %w", err)
	}

	burst, interval := cfg.AuthBurst, cfg.AuthInterval
	if burst <= 0 {
		burst = fallbackAuthBurst
	}
	if interval <= 0 {
		interval = fallbackAuthInterval
	}
	authLimiter, err := ratelimit.NewMemory(ratelimit.Config{Burst: burst, Interval: interval})
	if err != nil {
		return web.Deps{}, fmt.Errorf("auth rate limiter: %w", err)
	}
	authLimit := ratelimit.Middleware(authLimiter, func(r *http.Request) string {
		return "auth:" + r.URL.Path + ":ip:" + ratelimit.HashIP([]byte(cfg.IPHashSalt), r.RemoteAddr)
	})

	service := tierlist.NewService(postgres.New(pool), users, tierlist.ServiceDeps{})
	handler := api.New(api.Deps{
		Tierlists: service,
		Auth:      authn,
		Log:       log,
		CreateTierlistLimit: ratelimit.Middleware(limiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "create_tierlist")),
		AuthService:         svc.auth,
		Sessions:            svc.sessions,
		Resets:              svc.resets,
		AuthLimit:           authLimiter,
		AuthLimitMiddleware: authLimit,
		CSRF:                newCSRF(cfg, svc.sessions),
		SecureCookies:       cfg.CookieSecure,
	})
	return web.Deps{API: handler, Ready: pool.Ping}, nil
}

// newCSRF builds the CSRF service. The signing key is TIERL_CSRF_KEY or, when
// unset, a domain-separated derivative of the required IP hash salt, so all
// replicas agree on guest tokens without another mandatory secret.
func newCSRF(cfg config.Config, sessions *auth.SessionService) *api.CSRF {
	if cfg.CSRFKey != "" {
		return api.NewCSRF(sessions, []byte(cfg.CSRFKey))
	}
	sum := sha256.Sum256([]byte("tierl/csrf/v1|" + cfg.IPHashSalt))
	return api.NewCSRF(sessions, sum[:])
}

// userDirectory adapts auth.User to the tierlist.Users contract.
type userDirectory struct {
	auth *auth.Service
}

var _ tierlist.Users = userDirectory{}

// Profile implements tierlist.Users.
func (d userDirectory) Profile(ctx context.Context, userID string) (tierlist.Profile, error) {
	u, err := d.auth.Get(ctx, userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return tierlist.Profile{}, tierlist.ErrNotFound
		}
		return tierlist.Profile{}, err
	}
	return tierlist.Profile{DisplayName: u.DisplayName, EmailVerified: u.EmailVerified}, nil
}
