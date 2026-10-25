package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	"github.com/basili4-1982/tierl/internal/auth"
)

// CSRFHeader carries the token on state-changing requests (openapi.yaml).
const CSRFHeader = "X-CSRF-Token"

const (
	csrfGuestNonceBytes = 16
	csrfKeyBytes        = 32
	csrfGuestDomain     = "tierl/csrf/guest/v1\x00"
)

// CSRF issues and verifies CSRF tokens.
//
// A signed-in request gets the secret stored with its session, so the token is
// bound to the session: a token of another session does not pass. Guests get a
// stateless token: a random nonce signed with a server key. The token is
// returned only by GET /csrf (and by the auth endpoints), which the browser
// cannot read cross-origin; a CSRF attacker therefore cannot obtain one.
type CSRF struct {
	sessions *auth.SessionService
	key      []byte
}

// NewCSRF builds a CSRF service. sessions may be nil (guest-only mode); an
// empty key is replaced by a random per-process key.
func NewCSRF(sessions *auth.SessionService, key []byte) *CSRF {
	if len(key) == 0 {
		key = make([]byte, csrfKeyBytes)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			// crypto/rand failing means the process cannot be secure.
			panic("api: csrf key: " + err.Error())
		}
	}
	return &CSRF{sessions: sessions, key: key}
}

// Session returns the live session of the request, if any. The root middleware
// loads it once into the context, so this does not repeat the database read.
func (c *CSRF) Session(r *http.Request) (*auth.Session, bool) {
	if sess, ok := sessionFromContext(r.Context()); ok {
		return sess, true
	}
	if c.sessions == nil {
		return nil, false
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	sess, err := c.sessions.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return nil, false
	}
	return sess, true
}

// Token returns a CSRF token for the request: the session-bound secret when
// the request carries a live session, a signed guest token otherwise.
func (c *CSRF) Token(r *http.Request) (string, error) {
	if sess, ok := c.Session(r); ok {
		return encodeCSRFSecret(sess.CSRFSecret), nil
	}
	nonce := make([]byte, csrfGuestNonceBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(csrfGuestDomain))
	mac.Write(nonce)
	return base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify reports whether token is valid for the request. A request with a live
// session accepts only that session's token; a guest accepts only a signed
// guest token.
func (c *CSRF) Verify(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if sess, ok := c.Session(r); ok {
		want := encodeCSRFSecret(sess.CSRFSecret)
		return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
	}
	return c.verifyGuest(token)
}

func (c *CSRF) verifyGuest(token string) bool {
	nonceText, sigText, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(nonceText)
	if err != nil || len(nonce) != csrfGuestNonceBytes {
		return false
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(sigText)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(csrfGuestDomain))
	mac.Write(nonce)
	return hmac.Equal(sig, mac.Sum(nil))
}

func encodeCSRFSecret(secret []byte) string {
	return base64.RawURLEncoding.EncodeToString(secret)
}

// Handler serves GET /csrf.
func (c *CSRF) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := c.Token(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
			return
		}
		writeJSON(w, http.StatusOK, csrfTokenDTO{CSRFToken: token})
	}
}

// Protect wraps next with CSRF verification for state-changing methods. It is
// the route-level helper for modules that own a public endpoint (for example
// guest submissions in #48).
func (c *CSRF) Protect(next http.Handler) http.Handler {
	return c.ProtectAll(next)
}

// ProtectAll enforces CSRF on POST/PUT/PATCH/DELETE and skips the explicitly
// listed public patterns ("POST /api/v1/auth/login"). The API handler applies
// it once around the root mux, so new routes are protected by default.
func (c *CSRF) ProtectAll(next http.Handler, exempt ...string) http.Handler {
	skip := newRouteSet(exempt)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isUnsafeMethod(r.Method) || skip.has(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !c.Verify(r, r.Header.Get(CSRFHeader)) {
			writeError(w, http.StatusForbidden, "csrf_failed", "Неверный CSRF-токен", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type csrfTokenDTO struct {
	CSRFToken string `json:"csrf_token"`
}
