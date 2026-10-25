package mailer_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/platform/mailer"
	"github.com/basili4-1982/tierl/internal/platform/mailer/mailertest"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestAsyncSenderDeliversAndDrains(t *testing.T) {
	var delivered atomic.Int64
	capture := &mailertest.Capture{}
	inner := senderFunc(func(ctx context.Context, m mailer.Message) error {
		if err := capture.Send(ctx, m); err != nil {
			return err
		}
		delivered.Add(1)
		return nil
	})
	s, err := mailer.NewAsync(inner, mailer.AsyncConfig{Workers: 2, QueueSize: 8, Timeout: time.Second}, discardLog())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for range 5 {
		if err := s.Send(ctx, mailer.Message{To: "anna@example.com", Subject: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if delivered.Load() != 5 || capture.Len() != 5 {
		t.Fatalf("delivered = %d, captured = %d", delivered.Load(), capture.Len())
	}
	stats := s.Stats()
	if stats.Enqueued != 5 || stats.Delivered != 5 || stats.Failed != 0 || stats.Dropped != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestAsyncSenderDetachesFromRequestContext(t *testing.T) {
	capture := &mailertest.Capture{}
	s, err := mailer.NewAsync(capture, mailer.AsyncConfig{Workers: 1, QueueSize: 2}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the HTTP request is already gone when the worker sends
	if err := s.Send(ctx, mailer.Message{To: "anna@example.com", Subject: "s", Text: "token=secret"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if capture.Len() != 1 {
		t.Fatalf("captured = %d, want 1", capture.Len())
	}
}

func TestAsyncSenderQueueFullIsVisible(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	blocking := &blockingSender{started: make(chan struct{}), release: make(chan struct{})}
	s, err := mailer.NewAsync(blocking, mailer.AsyncConfig{Workers: 1, QueueSize: 1, Timeout: time.Hour}, log)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	secret := "token=super-secret-token"
	if err := s.Send(ctx, mailer.Message{To: "a@example.com", Subject: "one", Text: secret}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	if err := s.Send(ctx, mailer.Message{To: "a@example.com", Subject: "two", Text: secret}); err != nil {
		t.Fatal(err)
	}
	err = s.Send(ctx, mailer.Message{To: "a@example.com", Subject: "three", Text: secret})
	if !errors.Is(err, mailer.ErrQueueFull) {
		t.Fatalf("third send = %v, want ErrQueueFull", err)
	}
	stats := s.Stats()
	if stats.Enqueued != 2 || stats.Dropped != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	out := logs.String()
	if !strings.Contains(out, "queue is full") {
		t.Fatalf("overflow must be logged: %s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("log leaked the message body: %s", out)
	}

	close(blocking.release)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats = s.Stats()
	if stats.Delivered != 2 || stats.Failed != 0 {
		t.Fatalf("after drain: %+v", stats)
	}

	// Sending after close is an explicit error, not a panic.
	if err := s.Send(ctx, mailer.Message{To: "a@example.com", Subject: "late"}); !errors.Is(err, mailer.ErrClosed) {
		t.Fatalf("send after close = %v", err)
	}
	if got := s.Stats().Dropped; got != 2 {
		t.Fatalf("dropped = %d, want 2", got)
	}
}

func TestAsyncSenderCloseTimeoutAndFailureStats(t *testing.T) {
	blocking := &blockingSender{started: make(chan struct{}), release: make(chan struct{})}
	s, err := mailer.NewAsync(blocking, mailer.AsyncConfig{Workers: 1, QueueSize: 1, Timeout: time.Hour}, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), mailer.Message{To: "a@example.com", Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	<-blocking.started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close = %v, want deadline", err)
	}
	close(blocking.release)
	// A second close waits for the same drain.
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestAsyncSenderCountsFailures(t *testing.T) {
	var logs bytes.Buffer
	inner := senderFunc(func(context.Context, mailer.Message) error { return errors.New("smtp down") })
	s, err := mailer.NewAsync(inner, mailer.AsyncConfig{Workers: 1}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), mailer.Message{To: "a@example.com", Subject: "s", Text: "token=x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats := s.Stats()
	if stats.Failed != 1 || stats.Delivered != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if !strings.Contains(logs.String(), "delivery failed") || strings.Contains(logs.String(), "token=x") {
		t.Fatalf("bad log: %s", logs.String())
	}
}

func TestAsyncSenderDefaults(t *testing.T) {
	capture := &mailertest.Capture{}
	// Zero config and a nil logger must select safe defaults.
	s, err := mailer.NewAsync(capture, mailer.AsyncConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), mailer.Message{To: "a@example.com", Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if capture.Len() != 1 {
		t.Fatalf("captured = %d", capture.Len())
	}
}

func TestNewAsyncRejectsNilSender(t *testing.T) {
	if _, err := mailer.NewAsync(nil, mailer.AsyncConfig{}, nil); err == nil {
		t.Fatal("nil inner sender must fail")
	}
}

type senderFunc func(context.Context, mailer.Message) error

func (f senderFunc) Send(ctx context.Context, m mailer.Message) error { return f(ctx, m) }

// blockingSender blocks every delivery until release is closed.
type blockingSender struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (b *blockingSender) Send(ctx context.Context, _ mailer.Message) error {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
