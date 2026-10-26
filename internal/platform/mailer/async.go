package mailer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Async queue defaults.
const (
	DefaultWorkers   = 2
	DefaultQueueSize = 128
)

// ErrQueueFull is returned by AsyncSender.Send when the buffer is full, so the
// caller (and the metrics) know a letter was dropped instead of silently
// waiting for the SMTP server.
var ErrQueueFull = errors.New("mailer: outbound queue is full")

// ErrClosed is returned when a message is sent to a closed AsyncSender.
var ErrClosed = errors.New("mailer: sender is closed")

// AsyncConfig configures AsyncSender. Zero values select the defaults.
type AsyncConfig struct {
	// Workers is the number of delivery goroutines (default DefaultWorkers).
	Workers int
	// QueueSize is the buffer between the HTTP handlers and the workers
	// (default DefaultQueueSize).
	QueueSize int
	// Timeout bounds one delivery in a worker (default DefaultTimeout).
	Timeout time.Duration
}

// AsyncStats is a snapshot of the queue counters.
type AsyncStats struct {
	// Enqueued is how many messages were accepted into the buffer.
	Enqueued uint64
	// Delivered is how many messages the inner sender accepted.
	Delivered uint64
	// Failed is how many deliveries returned an error.
	Failed uint64
	// Dropped is how many messages were rejected because the buffer was full
	// or the sender was closed.
	Dropped uint64
}

// AsyncSender decouples HTTP handlers from SMTP: Send only appends to a
// bounded buffer, while a fixed number of workers deliver with their own
// timeout. That keeps the response time of "forgot password" independent of
// whether the address exists and of how slow the mail server is.
//
// A full buffer is not silent: the message is counted in Dropped and the
// event is logged (without the body, so no token leaks).
type AsyncSender struct {
	inner   Sender
	log     *slog.Logger
	timeout time.Duration
	ch      chan Message

	mu      sync.RWMutex
	closing bool

	wg        sync.WaitGroup
	closed    chan struct{}
	closeOnce sync.Once

	enqueued  atomic.Uint64
	delivered atomic.Uint64
	failed    atomic.Uint64
	dropped   atomic.Uint64
}

var _ Sender = (*AsyncSender)(nil)

// NewAsync starts the workers and returns an asynchronous sender. Close must
// be called to drain the buffer and stop the goroutines.
func NewAsync(inner Sender, cfg AsyncConfig, log *slog.Logger) (*AsyncSender, error) {
	if inner == nil {
		return nil, errors.New("mailer: async sender needs an inner sender")
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultQueueSize
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &AsyncSender{
		inner:   inner,
		log:     log,
		timeout: cfg.Timeout,
		ch:      make(chan Message, cfg.QueueSize),
		closed:  make(chan struct{}),
	}
	for range cfg.Workers {
		s.wg.Add(1)
		go s.worker()
	}
	return s, nil
}

// Send implements Sender. It never talks to SMTP and returns immediately.
func (s *AsyncSender) Send(_ context.Context, m Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closing {
		s.dropped.Add(1)
		s.log.Error("mailer: sender is closed; message dropped",
			slog.String("to", m.To), slog.String("subject", m.Subject))
		return ErrClosed
	}
	select {
	case s.ch <- m:
		s.enqueued.Add(1)
		return nil
	default:
		s.dropped.Add(1)
		s.log.Error("mailer: outbound queue is full; message dropped",
			slog.String("to", m.To), slog.String("subject", m.Subject))
		return ErrQueueFull
	}
}

// Stats returns the queue counters.
func (s *AsyncSender) Stats() AsyncStats {
	return AsyncStats{
		Enqueued:  s.enqueued.Load(),
		Delivered: s.delivered.Load(),
		Failed:    s.failed.Load(),
		Dropped:   s.dropped.Load(),
	}
}

// Close stops accepting messages, drains the buffer and waits for the workers
// or for ctx to end. It is safe to call more than once.
func (s *AsyncSender) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		close(s.ch)
		s.mu.Unlock()
		go func() {
			s.wg.Wait()
			close(s.closed)
		}()
	})
	select {
	case <-s.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *AsyncSender) worker() {
	defer s.wg.Done()
	for m := range s.ch {
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		err := s.inner.Send(ctx, m)
		cancel()
		if err != nil {
			s.failed.Add(1)
			s.log.Error("mailer: delivery failed",
				slog.String("to", m.To), slog.String("subject", m.Subject), slog.Any("error", err))
			continue
		}
		s.delivered.Add(1)
	}
}
