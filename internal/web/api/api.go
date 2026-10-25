// Package api implements the JSON HTTP API described in api/openapi.yaml.
// Handlers only translate HTTP to service calls and back: business rules live
// in internal/tierlist and there is no SQL here.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// Prefix is the path prefix of every API route (servers.url in the contract).
const Prefix = "/api/v1"

// maxBodyBytes bounds a JSON request body.
const maxBodyBytes = 1 << 20

// Authenticator identifies the signed-in user of a request.
type Authenticator interface {
	// UserID returns the ID of the signed-in user and true, or false for guests.
	UserID(r *http.Request) (string, bool)
}

// Middleware wraps a handler, for example with a rate limiter.
type Middleware func(http.Handler) http.Handler

// Deps are the dependencies of the API handler.
type Deps struct {
	Tierlists *tierlist.Service
	Auth      Authenticator
	Log       *slog.Logger
	// Accounts serves the email confirmation and password reset endpoints;
	// nil leaves them unregistered (for example without a database).
	Accounts Accounts
	// CreateTierlistLimit wraps POST /tierlists; nil means no extra middleware.
	CreateTierlistLimit Middleware
	// ResendVerificationLimit wraps POST /auth/resend-verification.
	ResendVerificationLimit Middleware
	// ForgotPasswordLimit wraps POST /auth/forgot-password.
	ForgotPasswordLimit Middleware
}

// API is the HTTP handler of the JSON API.
type API struct {
	tierlists *tierlist.Service
	auth      Authenticator
	accounts  Accounts
	log       *slog.Logger
	mux       *http.ServeMux
}

// New builds the handler with all routes registered under Prefix.
func New(d Deps) *API {
	a := &API{tierlists: d.Tierlists, auth: d.Auth, accounts: d.Accounts, log: d.Log, mux: http.NewServeMux()}
	if a.log == nil {
		a.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	create := http.Handler(http.HandlerFunc(a.createTierlist))
	if d.CreateTierlistLimit != nil {
		create = d.CreateTierlistLimit(create)
	}
	a.mux.Handle("POST "+Prefix+"/tierlists", create)
	a.mux.HandleFunc("GET "+Prefix+"/tierlists/{slug}", a.getTierlist)
	a.mux.HandleFunc("PATCH "+Prefix+"/tierlists/{slug}", a.updateTierlist)
	a.mux.HandleFunc("DELETE "+Prefix+"/tierlists/{slug}", a.deleteTierlist)
	a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/close", a.closeTierlist)

	if a.accounts != nil {
		a.mux.Handle("POST "+Prefix+"/auth/verify-email", http.HandlerFunc(a.verifyEmail))
		a.mux.Handle("POST "+Prefix+"/auth/reset-password", http.HandlerFunc(a.resetPassword))
		a.mux.Handle("POST "+Prefix+"/auth/resend-verification",
			withMiddleware(http.HandlerFunc(a.resendVerification), d.ResendVerificationLimit))
		a.mux.Handle("POST "+Prefix+"/auth/forgot-password",
			withMiddleware(http.HandlerFunc(a.forgotPassword), d.ForgotPasswordLimit))
	}

	a.mux.HandleFunc(Prefix+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	})
	return a
}

// withMiddleware wraps h when mw is not nil.
func withMiddleware(h http.Handler, mw Middleware) http.Handler {
	if mw == nil {
		return h
	}
	return mw(h)
}

// ServeHTTP implements http.Handler.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

// errorBody is the Error schema of the contract.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, errorBody{Code: code, Message: message, Details: details})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requireUser returns the signed-in user or writes 401.
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := a.auth.UserID(r)
	if !ok || id == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
		return "", false
	}
	return id, true
}

// decode reads a JSON body into dst and writes 400 on any syntax problem.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return false
	}
	return true
}

// writeServiceError maps service and domain errors to HTTP responses.
func (a *API) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.Is(err, tierlist.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	case errors.Is(err, tierlist.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "Нет прав на этот опрос", nil)
	case errors.Is(err, tierlist.ErrEmailNotVerified):
		writeError(w, http.StatusForbidden, "email_not_verified", "Подтвердите email, чтобы создавать опросы", nil)
	case errors.Is(err, domain.ErrStructureLocked):
		writeError(w, http.StatusConflict, "tierlist_locked", "Опрос уже проходили, структуру менять нельзя", nil)
	case errors.Is(err, tierlist.ErrSlugTaken):
		writeError(w, http.StatusConflict, "slug_taken", "Этот адрес уже занят", map[string]any{"fields": map[string]string{"slug": "уже занят"}})
	case errors.Is(err, domain.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_status", "Недопустимая смена статуса опроса", nil)
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы",
			map[string]any{"fields": map[string]string{verr.Field: verr.Reason}})
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы", nil)
	default:
		a.log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}
