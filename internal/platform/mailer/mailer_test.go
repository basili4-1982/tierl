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
