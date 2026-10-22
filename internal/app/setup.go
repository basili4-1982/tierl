package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

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

	// TODO(#23): replace guestAuth with the session-based authenticator and
	// the real user directory once the auth module lands.
	deps, err := newDeps(pool, cfg, log, guestAuth{}, noUsers{})
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	return deps, pool.Close, nil
}

// newDeps assembles the API handler on top of an open pool.
func newDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, auth api.Authenticator, users tierlist.Users) (web.Deps, error) {
	limiter, err := ratelimit.NewMemory(ratelimit.Config{
		Burst:    cfg.CreateTierlistBurst,
		Interval: cfg.CreateTierlistInterval,
	})
	if err != nil {
		return web.Deps{}, fmt.Errorf("rate limiter: %w", err)
	}
	service := tierlist.NewService(postgres.New(pool), users, tierlist.ServiceDeps{})
	handler := api.New(api.Deps{
		Tierlists: service,
		Auth:      auth,
		Log:       log,
		CreateTierlistLimit: ratelimit.Middleware(limiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "create_tierlist")),
	})
	return web.Deps{API: handler, Ready: pool.Ping}, nil
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
