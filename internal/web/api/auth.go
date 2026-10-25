package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
)

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

// writeAuthError maps auth errors to the Error schema.
func (a *API) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка недействительна или устарела", nil)
	case errors.Is(err, auth.ErrNotConfigured):
		a.log.ErrorContext(r.Context(), "auth email flow is not configured",
			slog.String("path", r.URL.Path))
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы",
			map[string]any{"fields": map[string]string{verr.Field: verr.Reason}})
	default:
		a.log.ErrorContext(r.Context(), "auth request failed",
			slog.String("path", r.URL.Path), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}
