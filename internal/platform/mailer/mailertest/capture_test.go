package mailertest

import (
	"context"
	"errors"
	"testing"

	"github.com/basili4-1982/tierl/internal/platform/mailer"
)

func TestCapture(t *testing.T) {
	c := &Capture{}
	ctx := context.Background()
	if _, ok := c.Last(); ok || c.Len() != 0 {
		t.Fatal("a new capture must be empty")
	}
	if err := c.Send(ctx, mailer.Message{To: "a@b.c", Subject: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, mailer.Message{To: "a@b.c", Subject: "two"}); err != nil {
		t.Fatal(err)
	}
	if c.Len() != 2 {
		t.Fatalf("len = %d", c.Len())
	}
	if msgs := c.Messages(); len(msgs) != 2 || msgs[1].Subject != "two" {
		t.Fatalf("messages = %+v", msgs)
	}
	if last, ok := c.Last(); !ok || last.Subject != "two" {
		t.Fatalf("last = %+v %v", last, ok)
	}

	boom := errors.New("boom")
	c.SetError(boom)
	if err := c.Send(ctx, mailer.Message{To: "a@b.c"}); !errors.Is(err, boom) {
		t.Fatalf("send = %v", err)
	}
	c.Reset()
	if c.Len() != 0 {
		t.Fatalf("reset left %d messages", c.Len())
	}
	if err := c.Send(ctx, mailer.Message{To: "a@b.c"}); err != nil {
		t.Fatalf("reset must clear the error: %v", err)
	}
}
