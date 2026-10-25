package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
)

const (
	// fallbackSessionGCInterval is used when Config was built without
	// TIERL_SESSION_GC_INTERVAL (tests, embedders).
	fallbackSessionGCInterval = time.Hour
	// sessionGCTimeout bounds one cleanup query.
	sessionGCTimeout = 10 * time.Second
)

// startSessionGC removes expired and revoked sessions at startup and then
// periodically. The returned func stops the goroutine before the pool closes.
func startSessionGC(sessions *auth.SessionService, interval time.Duration, log *slog.Logger) func() {
	if sessions == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = fallbackSessionGCInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		cleanupSessions(ctx, sessions, log)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanupSessions(ctx, sessions, log)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func cleanupSessions(ctx context.Context, sessions *auth.SessionService, log *slog.Logger) {
	runCtx, cancel := context.WithTimeout(ctx, sessionGCTimeout)
	defer cancel()
	removed, err := sessions.Cleanup(runCtx, time.Now())
	if err != nil {
		if ctx.Err() == nil {
			log.Warn("session cleanup failed", "error", err)
		}
		return
	}
	if removed > 0 {
		log.Info("session cleanup", "removed", removed)
	}
}
