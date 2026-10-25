package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
)

// Request bodies and responses of the auth endpoints (openapi.yaml).

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenRequest struct {
	Token string `json:"token"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
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

// register creates an account and opens a session (201).
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.allowByEmail(w, r, "register", req.Email) {
		return
	}
	u, err := a.authSvc.Register(r.Context(), auth.RegisterInput{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	a.openSession(w, r, u, http.StatusCreated)
}

// login checks the credentials and opens a session (200).
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.allowByEmail(w, r, "login", req.Email) {
		return
	}
	u, err := a.authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	a.openSession(w, r, u, http.StatusOK)
}

// logout closes the current session and clears the cookie (204). It is
// idempotent: an expired or already revoked session also ends with 204 and a
// cleared cookie, so the SPA can always sign out.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		if err := a.sessions.Logout(r.Context(), cookie.Value); err != nil {
			a.log.ErrorContext(r.Context(), "logout", "error", err)
		}
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// logoutAll closes every session of the user (204).
func (a *API) logoutAll(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if err := a.sessions.LogoutAll(r.Context(), userID); err != nil {
		a.log.ErrorContext(r.Context(), "logout all", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// me returns the signed-in user and the CSRF token of the session (200).
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	u, err := a.authSvc.Get(r.Context(), userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
			return
		}
		a.log.ErrorContext(r.Context(), "get current user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	csrfToken := ""
	if sess, ok := a.csrf.Session(r); ok {
		csrfToken = encodeCSRFSecret(sess.CSRFSecret)
	}
	writeJSON(w, http.StatusOK, toUserDTO(u, csrfToken))
}

// verifyEmail is a stub until TIERL-2 adds one-time email tokens and the
// Mailer: every token is reported as invalid, which is allowed by the
// contract (400 invalid_token).
func (a *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decode(w, r, &req) {
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка недействительна или устарела", nil)
}

// forgotPassword always answers 202, whether or not the email is registered.
func (a *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.allowByEmail(w, r, "forgot", req.Email) {
		return
	}
	if !a.resets.MailConfigured() {
		writeError(w, http.StatusServiceUnavailable, "mail_not_configured", "Отправка писем не настроена", nil)
		return
	}
	if err := a.resets.Request(r.Context(), req.Email); err != nil {
		// Delivery or storage problems must not reveal whether the address
		// exists; log them and keep the uniform answer.
		a.log.ErrorContext(r.Context(), "request password reset", "error", err)
	}
	w.WriteHeader(http.StatusAccepted)
}

// resetPassword sets a new password by a one-time token (204).
func (a *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.resets.MailConfigured() {
		writeError(w, http.StatusServiceUnavailable, "mail_not_configured", "Отправка писем не настроена", nil)
		return
	}
	if err := a.resets.Reset(r.Context(), req.Token, req.NewPassword); err != nil {
		a.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openSession creates a session, sets the cookie and writes the user.
func (a *API) openSession(w http.ResponseWriter, r *http.Request, u *auth.User, status int) {
	token, sess, err := a.sessions.Create(r.Context(), u.ID)
	if err != nil {
		a.log.ErrorContext(r.Context(), "create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	a.setSessionCookie(w, token)
	writeJSON(w, status, toUserDTO(u, encodeCSRFSecret(sess.CSRFSecret)))
}

// setSessionCookie writes tierl_session with the attributes of SPEC section 6.
func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is true outside local development (TIERL_COOKIE_SECURE)
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie.
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is true outside local development (TIERL_COOKIE_SECURE)
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// writeAuthError maps auth service errors to the contract.
func (a *API) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы",
			map[string]any{"fields": map[string]string{verr.Field: validationMessage(verr)}})
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "Этот email уже зарегистрирован", nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль", nil)
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка недействительна или устарела", nil)
	case errors.Is(err, auth.ErrMailNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "mail_not_configured", "Отправка писем не настроена", nil)
	default:
		a.log.ErrorContext(r.Context(), "auth request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}

// validationMessage returns a Russian message for the fields of the contract.
func validationMessage(e *auth.ValidationError) string {
	switch e.Field {
	case "email":
		return "Некорректный email"
	case "password":
		return "Длина пароля от 10 до 128 символов"
	case "display_name":
		return "Имя от 1 до 50 символов"
	default:
		return e.Reason
	}
}
