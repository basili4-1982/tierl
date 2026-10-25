// Package anonsession issues and reads the anonymous participant cookie from
// ADR-002. The cookie holds a random 32-byte token; the database stores only
// its SHA-256 hash, so a database leak does not reveal working sessions.
package anonsession

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CookieName is the anonymous session cookie (api/openapi.yaml).
const CookieName = "tierl_anon"

// tokenBytes is the entropy of a session token (ADR-002: 32 bytes).
const tokenBytes = 32

// MaxAge is how long a participant keeps the same anonymous session.
const MaxAge = 365 * 24 * time.Hour

// Session is the anonymous identity of a request.
type Session struct {
	// Token is the raw cookie value; it is only sent back to the client.
	Token string
	// Hash is the SHA-256 of Token, the only value stored in Postgres.
	Hash string
	// Fresh is true when the token was just generated for this request.
	Fresh bool
}

// New returns a session with a fresh random token.
func New(r io.Reader) (Session, error) {
	if r == nil {
		r = rand.Reader
	}
	var b [tokenBytes]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Session{}, fmt.Errorf("anonsession: random token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	return Session{Token: token, Hash: HashToken(token), Fresh: true}, nil
}

// FromRequest returns the session of the request. It reports false when the
// cookie is missing or malformed. The existing token is not replaced here so
// read-only endpoints never issue cookies.
func FromRequest(r *http.Request) (Session, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || !validToken(c.Value) {
		return Session{}, false
	}
	return Session{Token: c.Value, Hash: HashToken(c.Value)}, true
}

// Ensure returns the session of the request, generating a new one when the
// cookie is missing or malformed. SetCookie must be called when Fresh is true
// and the request succeeds.
func Ensure(r *http.Request, rand io.Reader) (Session, error) {
	if s, ok := FromRequest(r); ok {
		return s, nil
	}
	return New(rand)
}

// HashToken returns the hex SHA-256 of a token. The database never stores the
// token itself.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SetCookie writes the anonymous session cookie. It is HttpOnly (no JS
// access), Secure and SameSite=Lax (ADR-002), and lives under the whole site.
func SetCookie(w http.ResponseWriter, s Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    s.Token,
		Path:     "/",
		MaxAge:   int(MaxAge / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// validToken accepts exactly the format New produces (32 bytes, base64url
// without padding). Anything else is treated as a stale or forged cookie.
func validToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == tokenBytes
}
