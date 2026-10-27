package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"code.sarnas.ru/tierl/internal/auth"
	"code.sarnas.ru/tierl/internal/media"
	"code.sarnas.ru/tierl/internal/platform/config"
	"code.sarnas.ru/tierl/internal/platform/mailer"
	"code.sarnas.ru/tierl/internal/platform/ratelimit"
	"code.sarnas.ru/tierl/internal/storage/postgres"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/web"
	"code.sarnas.ru/tierl/internal/web/api"
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

	bundle, err := buildAuth(pool, cfg, log)
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	deps, err := buildDeps(pool, cfg, log, sessionAuth{bundle.sessions}, bundle.users, bundle)
	if err != nil {
		bundle.closeMail()
		pool.Close()
		return web.Deps{}, nil, err
	}
	return deps, func() {
		closeMedia(deps)
		bundle.closeMail()
		pool.Close()
	}, nil
}

// mailShutdownTimeout bounds the delivery drain on shutdown.
const mailShutdownTimeout = 5 * time.Second

// newDeps assembles the API handler on top of an open pool. The supplied
// authenticator and user directory drive authorization of tier lists; the auth
// endpoints always use the real Postgres session store. The returned func
// drains the async mail queue and is always safe to call.
func newDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authenticator api.Authenticator, users tierlist.Users) (web.Deps, func(), error) {
	bundle, err := buildAuth(pool, cfg, log)
	if err != nil {
		return web.Deps{}, nil, err
	}
	deps, err := buildDeps(pool, cfg, log, authenticator, users, bundle)
	if err != nil {
		bundle.closeMail()
		return web.Deps{}, nil, err
	}
	return deps, func() {
		closeMedia(deps)
		bundle.closeMail()
	}, nil
}

// closeMedia releases a filesystem storage opened by buildDeps.
func closeMedia(deps web.Deps) {
	if c, ok := deps.API.(interface{ CloseMedia() error }); ok {
		_ = c.CloseMedia()
	}
}

func buildDeps(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, authenticator api.Authenticator, users tierlist.Users, bundle *authBundle) (web.Deps, error) {
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
	store := postgres.New(pool)
	service := tierlist.NewService(store, users, tierlist.ServiceDeps{})
	imageStorage, err := buildMedia(cfg.Storage)
	if err != nil {
		return web.Deps{}, err
	}
	handler := api.New(api.Deps{
		Media:       imageStorage,
		Tierlists:   service,
		Auth:        authenticator,
		Accounts:    bundle.service,
		Log:         log,
		Store:       store,
		Results:     ogResultReader{pool: pool, store: store, users: users},
		Users:       users,
		AuthService: bundle.service,
		Sessions:    bundle.sessions,
		CreateTierlistLimit: ratelimit.Middleware(limiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "create_tierlist")),
		ResendVerificationLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "resend_verification")),
		ForgotPasswordLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "forgot_password")),
		VerifyEmailLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "verify_email")),
		ResetPasswordLimit: ratelimit.Middleware(emailLimiter,
			ratelimit.IPKey([]byte(cfg.IPHashSalt), "reset_password")),
		EmailLimiter: emailLimiter,
	})
	return web.Deps{API: handler, Ready: pool.Ping}, nil
}

// buildMedia selects the image storage backend from configuration. An empty
// type leaves media disabled (POST /media answers 503).
func buildMedia(st config.StorageConfig) (media.Storage, error) {
	if st.Type == "" {
		return nil, nil
	}
	switch st.Type {
	case config.StorageFS:
		dir := st.FSPath
		if dir == "" {
			dir = "./data/media"
		}
		base := st.PublicURL
		if base == "" {
			base = "/media"
		}
		fs, err := media.NewFS(dir, base)
		if err != nil {
			return nil, fmt.Errorf("media storage: %w", err)
		}
		return fs, nil
	case config.StorageS3:
		s3, err := media.NewS3(media.S3Options{
			Endpoint: st.S3Endpoint, Bucket: st.S3Bucket,
			AccessKey: st.S3AccessKey, SecretKey: st.S3SecretKey,
			Region: st.S3Region, UseSSL: st.S3UseSSL, PublicURL: st.PublicURL,
		})
		if err != nil {
			return nil, fmt.Errorf("media storage: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s3.EnsureBucket(ctx); err != nil {
			return nil, fmt.Errorf("media storage: %w", err)
		}
		return s3, nil
	default:
		return nil, fmt.Errorf("media storage: unknown type %q", st.Type)
	}
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

// authBundle wires the auth service, its Postgres session store, the one-time
// token store, the mail queue and the user directory used by tier list author
// checks.
type authBundle struct {
	service   *auth.Service
	sessions  *auth.SessionManager
	users     authUsers
	closeMail func()
}

func buildAuth(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger) (*authBundle, error) {
	base, err := mailer.NewSender(mailer.Config{
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
	mail, err := mailer.NewAsync(base, mailer.AsyncConfig{
		Workers:   cfg.Mail.Workers,
		QueueSize: cfg.Mail.QueueSize,
		Timeout:   cfg.Mail.Timeout,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("mailer queue: %w", err)
	}
	closeMail := func() {
		ctx, cancel := context.WithTimeout(context.Background(), mailShutdownTimeout)
		defer cancel()
		if err := mail.Close(ctx); err != nil {
			log.Warn("mail queue shutdown incomplete", slog.Any("error", err),
				slog.Uint64("dropped", mail.Stats().Dropped))
		}
	}

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
	service, err := auth.NewService(userStore, auth.Options{
		Hash:     params,
		Sessions: sessions,
		Tokens:   postgres.NewTokenStore(pool),
		Mail:     mail,
		BaseURL:  cfg.AppBaseURL,
		Lang:     mailer.ParseLang(cfg.Mail.Lang),
		Log:      log,
	})
	if err != nil {
		closeMail()
		return nil, fmt.Errorf("auth: %w", err)
	}
	return &authBundle{
		service: service, sessions: sessions,
		users: authUsers{users: userStore}, closeMail: closeMail,
	}, nil
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
