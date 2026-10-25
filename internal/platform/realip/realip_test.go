package realip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func remoteAfter(t *testing.T, spec []string, remote, xff string) string {
	t.Helper()
	p, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	h := p.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestNewInvalid(t *testing.T) {
	for _, spec := range [][]string{{"not-an-ip"}, {"10.0.0.0/99"}, {"10.0.0.1/abc"}} {
		if _, err := New(spec); err == nil {
			t.Errorf("New(%v) = nil error", spec)
		}
	}
	if _, err := New(nil); err != nil {
		t.Fatalf("empty spec: %v", err)
	}
}

func TestForwardedForIgnoredWithoutTrust(t *testing.T) {
	const peer = "198.51.100.20:4444"
	if got := remoteAfter(t, nil, peer, "203.0.113.9"); got != peer {
		t.Fatalf("untrusted remote = %q, want %q", got, peer)
	}
	// A peer outside the trusted ranges is treated the same way.
	if got := remoteAfter(t, []string{"10.0.0.0/8"}, peer, "203.0.113.9"); got != peer {
		t.Fatalf("untrusted peer = %q, want %q", got, peer)
	}
}

func TestForwardedForFromTrustedProxy(t *testing.T) {
	got := remoteAfter(t, []string{"10.0.0.0/8"}, "10.0.0.5:4444", "203.0.113.9")
	if got != "203.0.113.9" {
		t.Fatalf("client = %q", got)
	}
	// The closest untrusted hop wins; a spoofed left prefix is ignored.
	got = remoteAfter(t, []string{"10.0.0.0/8"}, "10.0.0.5:4444", "1.2.3.4, 203.0.113.9, 10.0.0.5")
	if got != "203.0.113.9" {
		t.Fatalf("spoofed chain = %q", got)
	}
	// Several hops: 10.0.0.7 is trusted too, so 203.0.113.9 is the client.
	got = remoteAfter(t, []string{"10.0.0.0/8"}, "10.0.0.5:4444", "203.0.113.9, 10.0.0.7")
	if got != "203.0.113.9" {
		t.Fatalf("multi hop = %q", got)
	}
}

func TestForwardedForEdgeCases(t *testing.T) {
	// Bare trusted IP without a CIDR.
	if got := remoteAfter(t, []string{"10.0.0.5"}, "10.0.0.5:1", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("bare ip = %q", got)
	}
	// Invalid entries are skipped.
	if got := remoteAfter(t, []string{"10.0.0.0/8"}, "10.0.0.5:1", "garbage, 203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("invalid entry = %q", got)
	}
	// No usable header leaves RemoteAddr untouched.
	const peer = "10.0.0.5:1"
	if got := remoteAfter(t, []string{"10.0.0.0/8"}, peer, ""); got != peer {
		t.Fatalf("empty header = %q", got)
	}
	if got := remoteAfter(t, []string{"10.0.0.0/8"}, peer, "garbage"); got != peer {
		t.Fatalf("garbage header = %q", got)
	}
	// IPv6.
	if got := remoteAfter(t, []string{"2001:db8::/32"}, "[2001:db8::1]:1", "2001:db8::9, 2001:db8::1"); got != "2001:db8::9" {
		t.Fatalf("ipv6 = %q", got)
	}
	// Several X-Forwarded-For headers are concatenated in order.
	p, err := New([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	h := p.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1"
	req.Header.Add("X-Forwarded-For", "1.2.3.4")
	req.Header.Add("X-Forwarded-For", "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.9" {
		t.Fatalf("multiple headers = %q", got)
	}
}
