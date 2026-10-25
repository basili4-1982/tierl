package mailer

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRenderLanguages(t *testing.T) {
	link := "https://tierl.example/verify-email?token=abc"
	for _, kind := range []Kind{VerifyEmail, ResetPassword} {
		for _, lang := range []Lang{LangRU, LangEN} {
			msg, err := Render(lang, kind, Data{Name: "Анна", URL: link, TTL: FormatTTL(lang, 24*time.Hour)})
			if err != nil {
				t.Fatalf("%s/%s: %v", kind, lang, err)
			}
			if msg.Subject == "" || !strings.Contains(msg.Text, link) || !strings.Contains(msg.HTML, link) {
				t.Fatalf("%s/%s: empty subject or link missing: %+v", kind, lang, msg)
			}
			if !strings.Contains(msg.Text, "Анна") || !strings.Contains(msg.HTML, "Анна") {
				t.Fatalf("%s/%s: name missing", kind, lang)
			}
			if lang == LangRU && !strings.Contains(msg.Text, "24 часа") {
				t.Errorf("%s: TTL not localized: %q", kind, msg.Text)
			}
			if lang == LangEN && !strings.Contains(msg.Text, "24 hours") {
				t.Errorf("%s: TTL not localized: %q", kind, msg.Text)
			}
		}
	}
	ru, _ := Render(LangRU, VerifyEmail, Data{URL: link})
	en, _ := Render(LangEN, VerifyEmail, Data{URL: link})
	if ru.Subject == en.Subject {
		t.Fatal("ru and en subjects must differ")
	}
}

func TestRenderUnknownKindAndFallback(t *testing.T) {
	if _, err := Render(LangRU, Kind("nope"), Data{}); err == nil {
		t.Fatal("unknown kind must fail")
	}
	msg, err := Render(Lang("de"), VerifyEmail, Data{Name: "x", URL: "https://x", TTL: "1 час"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Подтвердите email в Tierl" {
		t.Fatalf("fallback subject = %q", msg.Subject)
	}
}

func TestFormatTTL(t *testing.T) {
	tests := []struct {
		lang Lang
		d    time.Duration
		want string
	}{
		{LangRU, time.Hour, "1 час"},
		{LangRU, 2 * time.Hour, "2 часа"},
		{LangRU, 5 * time.Hour, "5 часов"},
		{LangRU, 11 * time.Hour, "11 часов"},
		{LangRU, 21 * time.Hour, "21 час"},
		{LangRU, 24 * time.Hour, "24 часа"},
		{LangEN, time.Hour, "1 hour"},
		{LangEN, 24 * time.Hour, "24 hours"},
		{LangEN, time.Minute, "1 hour"},
	}
	for _, tt := range tests {
		if got := FormatTTL(tt.lang, tt.d); got != tt.want {
			t.Errorf("FormatTTL(%s, %s) = %q, want %q", tt.lang, tt.d, got, tt.want)
		}
	}
}

func TestParseLang(t *testing.T) {
	for raw, want := range map[string]Lang{"en": LangEN, "EN": LangEN, "": LangRU, "ru": LangRU, "de": LangRU} {
		if got := ParseLang(raw); got != want {
			t.Errorf("ParseLang(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLogSenderDoesNotLogBody(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := LogSender{Log: log}
	const token = "s3cr3t-one-time-token"
	msg, err := Render(LangRU, ResetPassword, Data{Name: "Анна", URL: "https://tierl.example/reset-password?token=" + token, TTL: "1 час"})
	if err != nil {
		t.Fatal(err)
	}
	msg.To = "anna@example.com"
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, token) || strings.Contains(out, "reset-password?token") {
		t.Fatalf("log leaked the token or link: %s", out)
	}
	if !strings.Contains(out, "anna@example.com") {
		t.Fatalf("log should name the recipient: %s", out)
	}
}

func TestNewSenderFactory(t *testing.T) {
	s, err := NewSender(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(LogSender); !ok {
		t.Fatalf("empty host must select the log sender, got %T", s)
	}
	if _, err := NewSender(Config{Host: "smtp.example", Port: 587, From: "Tierl <no-reply@tierl.local>"}, nil); err != nil {
		t.Fatalf("smtp config: %v", err)
	}
	if _, err := NewSender(Config{Host: "smtp.example", Port: 0, From: "x@y.z"}, nil); err == nil {
		t.Fatal("bad port must fail")
	}
	if _, err := NewSMTP(Config{Host: "smtp.example", Port: 25, From: "bad"}); err == nil {
		t.Fatal("bad from must fail")
	}
}

func TestParseFrom(t *testing.T) {
	addr, head, err := ParseFrom(" Tierl <no-reply@tierl.local> ")
	if err != nil || addr != "no-reply@tierl.local" || head != "Tierl <no-reply@tierl.local>" {
		t.Fatalf("got %q %q %v", addr, head, err)
	}
	for _, bad := range []string{"", "Tierl <no-reply@tierl.local>\r\nBcc: x@y.z", "not-an-address"} {
		if _, _, err := ParseFrom(bad); err == nil {
			t.Errorf("ParseFrom(%q) must fail", bad)
		}
	}
}

func TestSMTPSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	var (
		mu  sync.Mutex
		got bytes.Buffer
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveSMTP(ln, &mu, &got)
	}()

	host, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portText)
	s, err := NewSMTP(Config{
		Host: host, Port: port, From: "Tierl <no-reply@tierl.local>",
		TLS: false, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{To: "anna@example.com", Subject: "Привет", Text: "plain body", HTML: "<p>html body</p>"}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fake smtp server did not finish")
	}
	mu.Lock()
	raw := got.String()
	mu.Unlock()
	for _, want := range []string{
		"MAIL FROM:<no-reply@tierl.local>", "RCPT TO:<anna@example.com>",
		"To: anna@example.com", "plain body", "<p>html body</p>", "MIME-Version: 1.0",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("message does not contain %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "Привет") {
		t.Error("non-ASCII subject must be RFC 2047 encoded")
	}
}

func TestSMTPSendRejectsHeaderInjection(t *testing.T) {
	s, err := NewSMTP(Config{Host: "127.0.0.1", Port: 1, From: "x@y.z"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []Message{
		{To: "a@b.c\r\nBcc: evil@x.y", Subject: "s", Text: "t"},
		{To: "a@b.c", Subject: "s\r\nX-Evil: 1", Text: "t"},
		{To: "", Subject: "s", Text: "t"},
	} {
		if err := s.Send(context.Background(), m); err == nil {
			t.Errorf("message %+v must be rejected", m)
		}
	}
}

func TestSMTPHonoursContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	// Accept but never answer, so the client blocks until the context ends.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
	}()
	host, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portText)
	s, err := NewSMTP(Config{Host: host, Port: port, From: "x@y.z", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s.Send(ctx, Message{To: "a@b.c", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("send ignored the context for %s", elapsed)
	}
}

// serveSMTP is a minimal SMTP server good enough for one DATA message.
func serveSMTP(ln net.Listener, mu *sync.Mutex, got *bytes.Buffer) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	reply("220 localhost ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250-localhost")
			reply("250 8BITMIME")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			mu.Lock()
			_, _ = got.WriteString(strings.TrimSpace(line) + "\n")
			mu.Unlock()
			reply("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			mu.Lock()
			_, _ = got.WriteString(strings.TrimSpace(line) + "\n")
			mu.Unlock()
			reply("250 OK")
		case cmd == "DATA":
			reply("354 go ahead")
			var data bytes.Buffer
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			mu.Lock()
			got.Write(data.Bytes())
			mu.Unlock()
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 OK")
		}
	}
}

// smtpScript configures the replies of serveScript. Empty values use a
// permissive default.
type smtpScript struct {
	greeting string
	ehlo     string
	mail     string
	rcpt     string
	data     string
	auth     string
	starttls string
}

func scriptValue(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// serveScript answers one SMTP session with the configured replies.
func serveScript(ln net.Listener, script smtpScript, mu *sync.Mutex, got *bytes.Buffer) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	record := func(s string) {
		mu.Lock()
		_, _ = got.WriteString(s + "\n")
		mu.Unlock()
	}
	reply(scriptValue(script.greeting, "220 localhost ESMTP"))
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply(scriptValue(script.ehlo, "250 localhost"))
		case strings.HasPrefix(cmd, "AUTH "):
			record(strings.TrimSpace(line))
			reply(scriptValue(script.auth, "235 authenticated"))
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			record(strings.TrimSpace(line))
			reply(scriptValue(script.mail, "250 OK"))
		case strings.HasPrefix(cmd, "RCPT TO:"):
			record(strings.TrimSpace(line))
			reply(scriptValue(script.rcpt, "250 OK"))
		case cmd == "STARTTLS":
			record(strings.TrimSpace(line))
			reply(scriptValue(script.starttls, "220 go ahead"))
		case cmd == "DATA":
			dataReply := scriptValue(script.data, "354 go ahead")
			reply(dataReply)
			if !strings.HasPrefix(dataReply, "354") {
				continue
			}
			var data bytes.Buffer
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			mu.Lock()
			got.Write(data.Bytes())
			mu.Unlock()
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 OK")
		}
	}
}

func sendToScript(t *testing.T, script smtpScript, cfg Config) (string, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var (
		mu  sync.Mutex
		got bytes.Buffer
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveScript(ln, script, &mu, &got)
	}()

	host, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portText)
	cfg.Host = host
	cfg.Port = port
	if cfg.From == "" {
		cfg.From = "Tierl <no-reply@tierl.local>"
	}
	s, err := NewSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sendErr := s.Send(context.Background(), Message{To: "anna@example.com", Subject: "s", Text: "body"})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scripted smtp server did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	return got.String(), sendErr
}

func TestSMTPRequiresSTARTTLS(t *testing.T) {
	commands, sendErr := sendToScript(t, smtpScript{}, Config{TLS: true})
	if sendErr == nil || !strings.Contains(sendErr.Error(), "STARTTLS") {
		t.Fatalf("send = %v, want a STARTTLS error", sendErr)
	}
	if strings.Contains(commands, "MAIL FROM") || strings.Contains(commands, "RCPT TO") {
		t.Fatalf("nothing may be sent in plaintext: %s", commands)
	}
}

func TestSMTPStageErrors(t *testing.T) {
	tests := map[string]struct {
		script smtpScript
		cfg    Config
		want   string
	}{
		"greeting rejected":  {script: smtpScript{greeting: "554 go away"}, want: "handshake"},
		"mail from rejected": {script: smtpScript{mail: "550 sender rejected"}, want: "MAIL FROM"},
		"rcpt rejected":      {script: smtpScript{rcpt: "550 no such user"}, want: "RCPT TO"},
		"data rejected":      {script: smtpScript{data: "554 message rejected"}, want: "DATA"},
		"auth rejected": {
			script: smtpScript{ehlo: "250-localhost\r\n250 AUTH PLAIN", auth: "535 bad credentials"},
			cfg:    Config{Username: "u", Password: "p"},
			want:   "auth",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, sendErr := sendToScript(t, tt.script, tt.cfg)
			if sendErr == nil || !strings.Contains(sendErr.Error(), tt.want) {
				t.Fatalf("send = %v, want %q", sendErr, tt.want)
			}
		})
	}
}

func TestSMTPDialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portText)
	_ = ln.Close() // the port is now closed

	s, err := NewSMTP(Config{Host: host, Port: port, From: "x@y.z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), Message{To: "a@b.c", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("expected a dial error")
	}
}

func TestSMTPStartTLSError(t *testing.T) {
	commands, sendErr := sendToScript(t, smtpScript{
		ehlo:     "250-localhost\r\n250 STARTTLS",
		starttls: "454 TLS not available",
	}, Config{TLS: true})
	if sendErr == nil || !strings.Contains(sendErr.Error(), "starttls") {
		t.Fatalf("send = %v, want a starttls error", sendErr)
	}
	if strings.Contains(commands, "MAIL FROM") {
		t.Fatalf("message must not be sent after a failed STARTTLS: %s", commands)
	}
}

func TestSMTPPlainAuthIsSkippedWithoutAuthExtension(t *testing.T) {
	commands, sendErr := sendToScript(t, smtpScript{ehlo: "250 localhost"}, Config{Username: "u", Password: "p"})
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}
	if strings.Contains(commands, "AUTH") {
		t.Fatalf("AUTH must not be attempted when the server does not offer it: %s", commands)
	}
	if !strings.Contains(commands, "MAIL FROM") {
		t.Fatal("message was not sent")
	}
}

func TestSMTPExpiredContext(t *testing.T) {
	s, err := NewSMTP(Config{Host: "127.0.0.1", Port: 25, From: "x@y.z"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := s.Send(ctx, Message{To: "a@b.c", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("expected a deadline error")
	}
}

func TestNewSMTPInvalidHost(t *testing.T) {
	if _, err := NewSMTP(Config{Host: "bad host", Port: 25, From: "x@y.z"}); err == nil {
		t.Fatal("a host with a space must be rejected")
	}
	if _, err := NewSMTP(Config{Host: "smtp.example", Port: 70000, From: "x@y.z"}); err == nil {
		t.Fatal("an out-of-range port must be rejected")
	}
}

func TestLogSenderWithNilLogger(t *testing.T) {
	var s LogSender
	msg, err := Render(LangEN, VerifyEmail, Data{Name: "Ann", URL: "https://x", TTL: "1 hour"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("send: %v", err)
	}
}
