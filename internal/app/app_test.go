package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/platform/config"
)

func TestRun_ServesAndShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, config.Config{ShutdownTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)), ln)
	}()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestRun_ServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close() // Serve on a closed listener fails immediately.

	err = Run(context.Background(), config.Config{ShutdownTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)), ln)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
