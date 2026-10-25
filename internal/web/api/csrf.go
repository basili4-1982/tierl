package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// csrfCookieName is the double-submit CSRF cookie (api/openapi.yaml).
const csrfCookieName = "tierl_csrf"

type csrfTokenDTO struct {
	Token string `json:"csrf_token"`
}

// getCSRF issues a random token in an HttpOnly cookie and returns the same
// value. Unsafe requests must echo it in X-CSRF-Token.
func (a *API) getCSRF(w http.ResponseWriter, _ *http.Request) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, csrfTokenDTO{Token: token})
}

// csrfOK implements double-submit checking: the token from GET /csrf must be
// present both as a cookie and as X-CSRF-Token. SameSite=Lax already blocks
// cross-site cookie sends; this is the second line of defence.
func csrfOK(r *http.Request) bool {
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	got := r.Header.Get("X-CSRF-Token")
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(c.Value)) == 1
}
