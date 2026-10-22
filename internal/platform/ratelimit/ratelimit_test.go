package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func TestNewMemoryInvalid(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"zero burst", Config{Interval: time.Second}},
		{"zero interval", Config{Burst: 1}},
		{"negative ttl", Config{Burst: 1, Interval: time.Second, IdleTTL: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewMemory(tt.cfg); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestNewMemoryDefaultClock(t *testing.T) {
	m, err := NewMemory(Config{Burst: 1, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Allow("k").Allowed || m.Allow("k").Allowed {
		t.Fatal("unexpected decisions")
	}
}

func TestAllowSequence(t *testing.T) {
	type step struct {
		advance time.Duration
		key     string
		allowed bool
		retry   time.Duration
	}
	tests := []struct {
		name  string
		burst int
		steps []step
	}{
		{
			name: "burst then block", burst: 2,
			steps: []step{
				{key: "a", allowed: true},
				{key: "a", allowed: true},
				{key: "a", retry: time.Minute},
			},
		},
		{
			name: "refill after interval", burst: 1,
			steps: []step{
				{key: "a", allowed: true},
				{advance: 30 * time.Second, key: "a", retry: 30 * time.Second},
				{advance: 30 * time.Second, key: "a", allowed: true},
			},
		},
		{
			name: "keys are independent", burst: 1,
			steps: []step{
				{key: "a", allowed: true},
				{key: "b", allowed: true},
				{key: "a", retry: time.Minute},
			},
		},
		{
			name: "refill capped at burst", burst: 2,
			steps: []step{
				{key: "a", allowed: true},
				{advance: 10 * time.Hour, key: "a", allowed: true},
				{key: "a", allowed: true},
				{key: "a", retry: time.Minute},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newClock()
			m, err := NewMemory(Config{Burst: tt.burst, Interval: time.Minute, IdleTTL: 100 * time.Hour, Now: clk.Now})
			if err != nil {
				t.Fatal(err)
			}
			for i, s := range tt.steps {
				clk.Advance(s.advance)
				d := m.Allow(s.key)
				if d.Allowed != s.allowed || d.RetryAfter != s.retry {
					t.Fatalf("step %d: got %+v, want allowed=%v retry=%v", i, d, s.allowed, s.retry)
				}
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	clk := newClock()
	m, err := NewMemory(Config{Burst: 1, Interval: time.Minute, IdleTTL: time.Hour, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	m.Allow("old")
	clk.Advance(30 * time.Minute)
	m.Allow("fresh")
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
	clk.Advance(31 * time.Minute) // old idle 61m, fresh idle 31m
	m.Allow("other")
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2 (fresh, other)", m.Len())
	}
	if !m.Allow("old").Allowed {
		t.Fatal("expired key must start with a full bucket")
	}
}

func TestDefaultIdleTTL(t *testing.T) {
	clk := newClock()
	m, _ := NewMemory(Config{Burst: 2, Interval: time.Minute, Now: clk.Now})
	if m.ttl != 2*time.Minute {
		t.Fatalf("ttl = %v", m.ttl)
	}
}

func TestConcurrent(t *testing.T) {
	clk := newClock()
	m, _ := NewMemory(Config{Burst: 50, Interval: time.Hour, Now: clk.Now})
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.Allow("k").Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
			m.Len()
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Fatalf("allowed = %d, want 50", allowed)
	}
}

func TestHashIP(t *testing.T) {
	salt := []byte("salt")
	tests := []struct {
		name      string
		a, b      string
		salta, sb []byte
		wantEqual bool
	}{
		{"port ignored", "1.2.3.4:5555", "1.2.3.4", salt, salt, true},
		{"ipv6 canonical", "[2001:DB8::1]:80", "2001:db8:0:0:0:0:0:1", salt, salt, true},
		{"spaces trimmed", " 1.2.3.4 ", "1.2.3.4", salt, salt, true},
		{"different ip", "1.2.3.4", "1.2.3.5", salt, salt, false},
		{"different salt", "1.2.3.4", "1.2.3.4", salt, []byte("other"), false},
		{"unparsable passes through", "garbage", "garbage", salt, salt, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha, hb := HashIP(tt.salta, tt.a), HashIP(tt.sb, tt.b)
			if (ha == hb) != tt.wantEqual {
				t.Fatalf("equal=%v, want %v", ha == hb, tt.wantEqual)
			}
			if len(ha) != 64 {
				t.Fatalf("len = %d", len(ha))
			}
		})
	}
	if HashIP(salt, "1.2.3.4") == "1.2.3.4" {
		t.Fatal("ip must not be stored in clear")
	}
}

func TestKeys(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "1.2.3.4:1000"
	k := IPKey([]byte("s"), "login")(r)
	if k != "login:ip:"+HashIP([]byte("s"), "1.2.3.4") {
		t.Fatalf("key = %q", k)
	}
	if got := EmailKey("reset", "  Anna@Example.COM "); got != "reset:email:anna@example.com" {
		t.Fatalf("key = %q", got)
	}
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name      string
		key       KeyFunc
		calls     int
		wantLast  int
		wantRetry string
		wantNext  int
	}{
		{"within limit", func(*http.Request) string { return "k" }, 1, 200, "", 1},
		{"blocked", func(*http.Request) string { return "k" }, 2, 429, "60", 1},
		{"empty key skips", func(*http.Request) string { return "" }, 5, 200, "", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newClock()
			m, _ := NewMemory(Config{Burst: 1, Interval: time.Minute, Now: clk.Now})
			next := 0
			h := Middleware(m, tt.key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				next++
				w.WriteHeader(http.StatusOK)
			}))
			var rec *httptest.ResponseRecorder
			for range tt.calls {
				rec = httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			}
			if rec.Code != tt.wantLast || next != tt.wantNext {
				t.Fatalf("code=%d next=%d", rec.Code, next)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantRetry {
				t.Fatalf("Retry-After = %q", got)
			}
			if tt.wantLast != 429 {
				return
			}
			var body struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != "rate_limited" || body.Message == "" || body.Details["retry_after"] != float64(60) {
				t.Fatalf("body = %+v", body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type = %q", ct)
			}
		})
	}
}

func TestWriteLimitedRounding(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "1"},
		{time.Millisecond, "1"},
		{1500 * time.Millisecond, "2"},
		{30 * time.Second, "30"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		WriteLimited(rec, tt.in)
		if got := rec.Header().Get("Retry-After"); got != tt.want {
			t.Errorf("%v: Retry-After = %q, want %q", tt.in, got, tt.want)
		}
	}
}
