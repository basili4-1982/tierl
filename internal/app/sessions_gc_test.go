package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/auth/authtest"
)

func TestStartSessionGC(t *testing.T) {
	store := authtest.NewSessions()
	now := time.Now().UTC()
	stale := &auth.Session{
		TokenHash:         bytes.Repeat([]byte{1}, 32),
		UserID:            "u1",
		CSRFSecret:        bytes.Repeat([]byte{2}, 32),
		CreatedAt:         now.Add(-2 * time.Hour),
		LastSeenAt:        now.Add(-2 * time.Hour),
		ExpiresAt:         now.Add(-time.Hour),
		AbsoluteExpiresAt: now.Add(-30 * time.Minute),
	}
	if err := store.Create(t.Context(), stale); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewSessionService(store, auth.SessionOptions{})
	stop := startSessionGC(svc, 5*time.Millisecond, discardLog())
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for store.Len() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("session cleanup did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
