package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/config"
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

	bundle, err := buildAuth(pool, cfg)
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	deps, err := buildDeps(pool, cfg, log, sessionAuth{bundle.sessions}, bundle.users, bundle)
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	return deps, pool.Close, nil
}

// newDeps assembles the API handler on top of an open pool. The supplied
// authenticator and user directory drive authorization of tier lists; the auth
// endpoints always use the real Postgres session store.
func newDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authenticator api.Authenticator, users tierlist.Users) (web.Deps, error) {
	bundle, err := buildAuth(pool, cfg)
	if err != nil {
		return web.Deps{}, err
	}
	return buildDeps(pool, cfg, log, authenticator, users, bundle)
}

func buildDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authenticator api.Authenticator, users tierlist.Users, bundle *authBundle) (web.Deps, error) {
	limiter, err := ratelimit.NewMemory(ratelimit.Config{
		Burst:    cfg.CreateTierlistBurst,
		Interval: cfg.CreateTierlistInterval,
	})
	if err != nil {
		return web.Deps{}, fmt.Errorf("rate limiter: %w", err)
	}
	store := postgres.New(pool)
	service := tierlist.NewService(store, users, tierlist.ServiceDeps{})
	handler := api.New(api.Deps{
		Tierlists:   service,
		Auth:        authenticator,
		Log:         log,
		Store:       store,
		Results:     ogResultReader{pool: pool, store: store, users: users},
		Users:       users,
		AuthService: bundle.service,
		Sessions:    bundle.sessions,
		CreateTierlistLimit: ratelimit.Middleware(limiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "create_tierlist")),
	})
	return web.Deps{API: handler, Ready: pool.Ping}, nil
}

// authBundle wires the auth service, its Postgres session store and the user
// directory used by tier list author checks.
type authBundle struct {
	service  *auth.Service
	sessions *auth.SessionManager
	users    authUsers
}

func buildAuth(pool *pgxpool.Pool, cfg config.Config) (*authBundle, error) {
	userStore := postgres.NewUserStore(pool)
	sessionStore := postgres.NewSessionStore(pool)
	sessions := auth.NewSessionManager(sessionStore, auth.SessionOptions{})

	params := auth.DefaultHashParams()
	if cfg.Argon2MemoryKiB != 0 || cfg.Argon2Iterations != 0 || cfg.Argon2Parallelism != 0 {
		params = auth.HashParams{
			MemoryKiB:   cfg.Argon2MemoryKiB,
			Iterations:  cfg.Argon2Iterations,
			Parallelism: cfg.Argon2Parallelism,
		}
	}
	service, err := auth.NewService(userStore, auth.Options{Hash: params, Sessions: sessions})
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	return &authBundle{service: service, sessions: sessions, users: authUsers{users: userStore}}, nil
}

// authUsers adapts the auth user directory to tierlist.Users.
type authUsers struct {
	users *postgres.UserStore
}

func (u authUsers) Profile(ctx context.Context, userID string) (tierlist.Profile, error) {
	account, err := u.users.GetByID(ctx, userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		return tierlist.Profile{}, tierlist.ErrNotFound
	}
	if err != nil {
		return tierlist.Profile{}, err
	}
	return tierlist.Profile{DisplayName: account.DisplayName, EmailVerified: account.EmailVerified}, nil
}

// sessionAuth authenticates requests by the server-side session cookie.
type sessionAuth struct {
	sessions *auth.SessionManager
}

func (s sessionAuth) UserID(r *http.Request) (string, bool) {
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	sess, err := s.sessions.Lookup(r.Context(), c.Value)
	if err != nil {
		return "", false
	}
	return sess.UserID, true
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
