package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPSender delivers messages through an SMTP server. It supports STARTTLS
// and PLAIN authentication, which covers Mailpit locally and a regular relay
// in production.
type SMTPSender struct {
	addr     string
	host     string
	fromAddr string
	fromHead string
	username string
	password string
	useTLS   bool
	timeout  time.Duration
}

// NewSMTP validates the config and builds an SMTP sender.
func NewSMTP(cfg Config) (*SMTPSender, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("mailer: smtp host is required")
	}
	if strings.ContainsAny(host, " \r\n") {
		return nil, errors.New("mailer: invalid smtp host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("mailer: smtp port %d is out of range", cfg.Port)
	}
	fromAddr, fromHead, err := ParseFrom(cfg.From)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &SMTPSender{
		addr:     net.JoinHostPort(host, fmt.Sprint(cfg.Port)),
		host:     host,
		fromAddr: fromAddr,
		fromHead: fromHead,
		username: cfg.Username,
		password: cfg.Password,
		useTLS:   cfg.TLS,
		timeout:  timeout,
	}, nil
}

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	if err := checkHeader("to", m.To); err != nil {
		return err
	}
	if err := checkHeader("subject", m.Subject); err != nil {
		return err
	}
	timeout := s.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline); left < timeout {
			timeout = left
		}
	}
	if timeout <= 0 {
		return errors.New("mailer: context deadline exceeded")
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("mailer: dial %s: %w", s.addr, err)
	}
	defer func() { _ = conn.Close() }()
	// Abort a blocked read/write as soon as the caller cancels.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("mailer: smtp handshake: %w", err)
	}
	defer func() { _ = c.Close() }()

	if s.useTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("mailer: server does not offer STARTTLS, refusing to send plaintext")
		}
		tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12} //nolint:gosec // ServerName is the configured SMTP host
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("mailer: starttls: %w", err)
		}
	}
	if s.username != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
				return fmt.Errorf("mailer: auth: %w", err)
			}
		}
	}

	if err := c.Mail(s.fromAddr); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("mailer: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := w.Write(s.encode(m)); err != nil {
		return fmt.Errorf("mailer: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: finish message: %w", err)
	}
	// The server has already accepted the message; a failed QUIT does not undo it.
	_ = c.Quit()
	return nil
}

// checkHeader rejects values that would inject extra headers.
func checkHeader(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("mailer: %s contains a line break", name)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("mailer: %s is empty", name)
	}
	return nil
}

// encode renders the RFC 5322 message with a text and an HTML alternative.
func (s *SMTPSender) encode(m Message) []byte {
	boundary := "tierl-" + randomHex(12)
	var b strings.Builder
	header := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	header("From", s.fromHead)
	header("To", m.To)
	header("Subject", encodeSubject(m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", randomHex(16), s.host))
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	part := func(contentType, body string) {
		b.WriteString("--")
		b.WriteString(boundary)
		b.WriteString("\r\n")
		header("Content-Type", contentType+`; charset="utf-8"`)
		header("Content-Transfer-Encoding", "8bit")
		b.WriteString("\r\n")
		// net/smtp's DATA writer normalizes line endings and dot-stuffs.
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
		b.WriteString("\r\n")
	}
	part("text/plain", m.Text)
	part("text/html", m.HTML)
	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("--\r\n")
	return []byte(b.String())
}

// encodeSubject RFC 2047-encodes a non-ASCII subject.
func encodeSubject(subject string) string {
	for _, r := range subject {
		if r > 127 {
			return mime.BEncoding.Encode("utf-8", subject)
		}
	}
	return subject
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// The value is cosmetic (boundary, Message-ID); a timestamp keeps
		// messages distinct even if the entropy source fails.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
