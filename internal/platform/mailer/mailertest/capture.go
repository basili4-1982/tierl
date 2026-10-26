// Package mailertest provides an in-memory mailer.Sender for tests.
package mailertest

import (
	"context"
	"sync"

	"github.com/basili4-1982/tierl/internal/platform/mailer"
)

// Capture records every message it is asked to send. It is safe for
// concurrent use.
type Capture struct {
	mu   sync.Mutex
	msgs []mailer.Message
	err  error
}

var _ mailer.Sender = (*Capture)(nil)

// Send implements mailer.Sender.
func (c *Capture) Send(_ context.Context, m mailer.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.msgs = append(c.msgs, m)
	return nil
}

// SetError makes subsequent Send calls fail with err; nil clears it.
func (c *Capture) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

// Messages returns a copy of the captured messages.
func (c *Capture) Messages() []mailer.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mailer.Message(nil), c.msgs...)
}

// Last returns the most recent message.
func (c *Capture) Last() (mailer.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return mailer.Message{}, false
	}
	return c.msgs[len(c.msgs)-1], true
}

// Len reports how many messages were captured.
func (c *Capture) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

// Reset drops all captured messages and the injected error.
func (c *Capture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = nil
	c.err = nil
}
