package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
)

// CSRFCookieName is an optional companion cookie. Validation does not depend
// on it: the token is self-contained and also returned by GET /csrf and
// GET /auth/me.
const CSRFCookieName = "tierl_csrf"

// CSRFHeader is the header that carries the CSRF token on unsafe requests.
const CSRFHeader = "X-CSRF-Token"

// Accounts is the subset of the auth service used by the email endpoints.
// *auth.Service implements it.
type Accounts interface {
	VerifyEmail(ctx context.Context, token string) error
	ResendVerification(ctx context.Context, email string) error
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
}

type tokenRequest struct {
	Token string `json:"token"`
}

type emailRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	OldPassword     string `json:"old_password"`
	NewPassword     string `json:"new_password"`
}

type csrfTokenDTO struct {
	CSRFToken string `json:"csrf_token"`
}

type passwordChangedDTO struct {
	CSRFToken       string `json:"csrf_token"`
	RevokedSessions int64  `json:"revoked_sessions"`
}

type userDTO struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	CSRFToken     string    `json:"csrf_token,omitempty"`
}

func toUserDTO(u *auth.User, csrfToken string) userDTO {
	return userDTO{
		ID:            u.ID,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt.UTC(),
		CSRFToken:     csrfToken,
	}
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// secureRequest reports whether cookies must carry the Secure attribute. It is
// false only for plain-HTTP local development.
func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		if strings.EqualFold(proto, "https") {
			return true
		}
		if strings.EqualFold(proto, "http") {
			return false
		}
	}
	host := r.Host
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return false
	default:
		return true
	}
}

func (a *API) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure вычисляется по схеме запроса
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(a.sessions.TTL() / time.Second),
		Expires:  time.Now().Add(a.sessions.TTL()),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure вычисляется по схеме запроса
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// currentSession resolves the request's session or writes 401.
func (a *API) currentSession(w http.ResponseWriter, r *http.Request) (string, *auth.Session, bool) {
	token := sessionToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
		return "", nil, false
	}
	sess, err := a.sessions.Lookup(r.Context(), token)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
			return "", nil, false
		}
		a.log.ErrorContext(r.Context(), "session lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return "", nil, false
	}
	return token, sess, true
}

func (a *API) getCSRF(w http.ResponseWriter, _ *http.Request) {
	token, err := a.csrf.Token()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	writeJSON(w, http.StatusOK, csrfTokenDTO{CSRFToken: token})
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	u, err := a.authService.Register(r.Context(), auth.RegisterInput{
		Email: req.Email, Password: req.Password, DisplayName: req.DisplayName,
	})
	if err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	token, _, err := a.sessions.Create(r.Context(), u.ID)
	if err != nil {
		a.log.ErrorContext(r.Context(), "create session after register", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, toUserDTO(u, ""))
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	u, err := a.authService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	token, _, err := a.sessions.Create(r.Context(), u.ID)
	if err != nil {
		a.log.ErrorContext(r.Context(), "create session after login", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, toUserDTO(u, ""))
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	token, _, ok := a.currentSession(w, r)
	if !ok {
		return
	}
	if err := a.sessions.Delete(r.Context(), token); err != nil {
		a.log.ErrorContext(r.Context(), "delete session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) logoutAll(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := a.currentSession(w, r)
	if !ok {
		return
	}
	if err := a.sessions.DeleteAll(r.Context(), sess.UserID); err != nil {
		a.log.ErrorContext(r.Context(), "delete all sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := a.currentSession(w, r)
	if !ok {
		return
	}
	u, err := a.authService.Get(r.Context(), sess.UserID)
	if err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	csrfToken, err := a.csrf.Token()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u, csrfToken))
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := a.currentSession(w, r)
	if !ok {
		return
	}
	var req changePasswordRequest
	if !decode(w, r, &req) {
		return
	}
	oldPassword := req.CurrentPassword
	if oldPassword == "" {
		oldPassword = req.OldPassword
	}
	if err := a.authService.ChangePassword(r.Context(), sess.UserID, oldPassword, req.NewPassword); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	a.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// changeMyPassword handles POST /me/password. It keeps the current session
// open and closes every other session of the user.
func (a *API) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	token, sess, ok := a.currentSession(w, r)
	if !ok {
		return
	}
	var req changePasswordRequest
	if !decode(w, r, &req) {
		return
	}
	oldPassword := req.CurrentPassword
	if oldPassword == "" {
		oldPassword = req.OldPassword
	}
	revoked, err := a.authService.ChangePasswordKeepingCurrent(r.Context(), sess.UserID, token, oldPassword, req.NewPassword)
	if err != nil {
		var ve *auth.ValidationError
		if errors.As(err, &ve) && ve.Field == "password" {
			fields := map[string]string{ve.Field: ve.Reason}
			// The contract names the new password new_password; keep both keys
			// so a client can map the error onto either input.
			fields["new_password"] = ve.Reason
			writeError(w, http.StatusBadRequest, "validation_failed", "Проверьте поля формы",
				map[string]any{"fields": fields})
			return
		}
		a.writeAuthError(w, r, err)
		return
	}
	csrfToken, err := a.csrf.Token()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	writeJSON(w, http.StatusOK, passwordChangedDTO{CSRFToken: csrfToken, RevokedSessions: revoked})
}

// verifyEmail handles POST /auth/verify-email.
func (a *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decode(w, r, &req) {
		return
	}
	if err := a.accounts.VerifyEmail(r.Context(), req.Token); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resendVerification handles POST /auth/resend-verification. The answer does
// not depend on whether the email exists or is already verified.
func (a *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.allowEmail(w, "resend_verification", req.Email) {
		return
	}
	if err := a.accounts.ResendVerification(r.Context(), req.Email); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// forgotPassword handles POST /auth/forgot-password. Like resend-verification,
// it always answers 202 for a well-formed email.
func (a *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.allowEmail(w, "forgot_password", req.Email) {
		return
	}
	if err := a.accounts.ForgotPassword(r.Context(), req.Email); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// resetPassword handles POST /auth/reset-password.
func (a *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if err := a.accounts.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowEmail applies the per-email rate limit in addition to the per-IP
// middleware. A malformed or empty email is still counted, so the endpoint
// cannot be used to probe addresses quickly.
func (a *API) allowEmail(w http.ResponseWriter, scope, email string) bool {
	if a.emailLimiter == nil {
		return true
	}
	d := a.emailLimiter.Allow(ratelimit.EmailKey(scope, email))
	if d.Allowed {
		return true
	}
	ratelimit.WriteLimited(w, d.RetryAfter)
	return false
}

func (a *API) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка недействительна или устарела", nil)
	case errors.Is(err, auth.ErrNotConfigured):
		a.log.ErrorContext(r.Context(), "auth email flow is not configured",
			"path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "Этот email уже зарегистрирован", nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль", nil)
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
	case errors.As(err, &ve):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы",
			map[string]any{"fields": map[string]string{ve.Field: ve.Reason}})
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы", nil)
	default:
		a.log.ErrorContext(r.Context(), "auth request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}
