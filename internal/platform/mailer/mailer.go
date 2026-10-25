// Package mailer delivers transactional email through SMTP or, when no SMTP
// host is configured, logs that a message was skipped.
//
// Message bodies are never logged: they contain one-time links with secrets.
// The log sender records only the recipient and the subject.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
)

// Message is one transactional email in both supported formats.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers a message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Config configures the SMTP sender.
type Config struct {
	// Host and Port address the SMTP server. An empty Host disables SMTP and
	// selects the log sender.
	Host string
	Port int
	// Username and Password enable PLAIN authentication when Username is set.
	Username string
	Password string
	// From is the envelope and header sender, for example
	// "Tierl <no-reply@tierl.local>".
	From string
	// TLS upgrades the connection with STARTTLS when the server offers it.
	TLS bool
	// Timeout bounds one delivery; the zero value means DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout is the delivery timeout used when Config.Timeout is zero.
const DefaultTimeout = 10 * time.Second

// NewSender returns an SMTP sender for a configured host and a log sender
// otherwise. The log sender is the development mode (Mailpit stays available
// through docker compose; this mode is for running without any SMTP server).
func NewSender(cfg Config, log *slog.Logger) (Sender, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return LogSender{Log: log}, nil
	}
	return NewSMTP(cfg)
}

// LogSender is a Sender that does not deliver anything. It exists so local
// runs without SMTP keep working; it never writes the message body (which
// holds the one-time link) to the log.
type LogSender struct {
	Log *slog.Logger
}

// Send implements Sender.
func (s LogSender) Send(_ context.Context, m Message) error {
	log := s.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log.Info("email not delivered: SMTP is not configured",
		slog.String("to", m.To), slog.String("subject", m.Subject))
	return nil
}

// ParseFrom validates a From value and returns the bare address and the value
// to put into the header.
func ParseFrom(from string) (addr, header string, err error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", errors.New("mailer: from address is empty")
	}
	if strings.ContainsAny(from, "\r\n") {
		return "", "", errors.New("mailer: from address contains a line break")
	}
	a, err := mail.ParseAddress(from)
	if err != nil {
		return "", "", fmt.Errorf("mailer: invalid from address: %w", err)
	}
	return a.Address, from, nil
}
