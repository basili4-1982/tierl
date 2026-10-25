// Package realip restores the client IP behind a trusted reverse proxy.
//
// X-Forwarded-For is attacker-controlled, so it is read only when the direct
// peer (r.RemoteAddr before this middleware) is listed in TIERL_TRUSTED_PROXIES.
// The rightmost address that is not itself a trusted proxy is the client, which
// keeps a spoofed prefix ("1.2.3.4, real-client") from winning.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Proxy rewrites r.RemoteAddr for requests that arrive from trusted peers.
type Proxy struct {
	trusted []netip.Prefix
}

// New parses a list of IPs and CIDRs. An empty list disables the middleware:
// X-Forwarded-For is then ignored completely.
func New(spec []string) (*Proxy, error) {
	p := &Proxy{}
	for _, raw := range spec {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := parsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("realip: %q: %w", raw, err)
		}
		p.trusted = append(p.trusted, prefix)
	}
	return p, nil
}

func parsePrefix(raw string) (netip.Prefix, error) {
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// Middleware replaces RemoteAddr with the client IP from X-Forwarded-For when
// the direct peer is trusted. Without trusted proxies it is a no-op.
func (p *Proxy) Middleware(next http.Handler) http.Handler {
	if p == nil || len(p.trusted) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if client := p.clientIP(r); client != "" {
			r = r.Clone(r.Context())
			r.RemoteAddr = client
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the client address or "" when the header must be ignored.
func (p *Proxy) clientIP(r *http.Request) string {
	peer, err := parseAddr(r.RemoteAddr)
	if err != nil || !p.trustedAddr(peer) {
		return ""
	}
	var chain []netip.Addr
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(header, ",") {
			if addr, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
				chain = append(chain, addr.Unmap())
			}
		}
	}
	if len(chain) == 0 {
		return ""
	}
	// Walk from the closest hop: trusted proxies are skipped, the first
	// untrusted address is the client. Everything to its left may be spoofed.
	for i := len(chain) - 1; i >= 0; i-- {
		if !p.trustedAddr(chain[i]) {
			return chain[i].String()
		}
	}
	return chain[0].String()
}

func (p *Proxy) trustedAddr(addr netip.Addr) bool {
	for _, prefix := range p.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// parseAddr accepts both "host:port" and a bare address.
func parseAddr(remote string) (netip.Addr, error) {
	remote = strings.TrimSpace(remote)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	addr, err := netip.ParseAddr(remote)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}
