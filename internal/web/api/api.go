// Package api implements the JSON HTTP API described in api/openapi.yaml.
// Handlers only translate HTTP to service calls and back: business rules live
// in internal/tierlist and internal/auth and there is no SQL here.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
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

// SessionAuthenticator resolves the tierl_session cookie to a user. It
// implements Authenticator on top of the server-side sessions (ADR-003).
type SessionAuthenticator struct {
	Sessions *auth.SessionService
}

var _ Authenticator = SessionAuthenticator{}

// UserID implements Authenticator. Unknown, expired and revoked sessions look
// like a guest request.
func (s SessionAuthenticator) UserID(r *http.Request) (string, bool) {
	if s.Sessions == nil {
		return "", false
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	sess, err := s.Sessions.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return "", false
	}
	return sess.UserID, true
}

// guestAuthenticator is the no-op default when the API is built without an
// authenticator: every request is anonymous and authentication-required
// endpoints answer 401 instead of panicking on a nil interface.
type guestAuthenticator struct{}

var _ Authenticator = guestAuthenticator{}

// UserID implements Authenticator.
func (guestAuthenticator) UserID(*http.Request) (string, bool) { return "", false }

// Middleware wraps a handler, for example with a rate limiter.
type Middleware func(http.Handler) http.Handler

// Deps are the dependencies of the API handler.
type Deps struct {
	Tierlists *tierlist.Service
	Auth      Authenticator
	Log       *slog.Logger
	// CreateTierlistLimit wraps POST /tierlists; nil means no extra middleware.
	CreateTierlistLimit Middleware

	// AuthService serves the auth endpoints. When any of AuthService, Sessions
	// or Resets is nil the /auth routes are not registered (static-only and
	// integration setups); GET /csrf keeps working.
	AuthService *auth.Service
	Sessions    *auth.SessionService
	Resets      *auth.PasswordResetService
	// AuthLimit is the per-email limiter of login, registration and
	// forgot-password (reset-password has no email in the body and is limited
	// per IP by AuthLimitMiddleware).
	AuthLimit           ratelimit.Limiter
	AuthLimitMiddleware Middleware
	// CSRF issues and verifies tokens; nil builds a guest-only service.
	CSRF *CSRF
	// CSRFExempt adds public state-changing route patterns beyond login and
	// registration, for example "POST /api/v1/tierlists/{slug}/answers" (#48).
	CSRFExempt []string
	// SecureCookies sets the Secure attribute of the session cookie. It is
	// true in production and false for local HTTP development.
	SecureCookies bool
}

// API is the HTTP handler of the JSON API.
type API struct {
	tierlists     *tierlist.Service
	auth          Authenticator
	authSvc       *auth.Service
	sessions      *auth.SessionService
	resets        *auth.PasswordResetService
	authLimit     ratelimit.Limiter
	csrf          *CSRF
	csrfExempt    *routeSet
	authRequired  *routeSet
	secureCookies bool
	log           *slog.Logger
	mux           *http.ServeMux
	handler       http.Handler
}

// New builds the handler with all routes registered under Prefix.
func New(d Deps) *API {
	if d.Auth == nil {
		if d.Sessions != nil {
			d.Auth = SessionAuthenticator{Sessions: d.Sessions}
		} else {
			d.Auth = guestAuthenticator{}
		}
	}
	if d.CSRF == nil {
		d.CSRF = NewCSRF(d.Sessions, nil)
	}
	a := &API{
		tierlists:     d.Tierlists,
		auth:          d.Auth,
		authSvc:       d.AuthService,
		sessions:      d.Sessions,
		resets:        d.Resets,
		authLimit:     d.AuthLimit,
		csrf:          d.CSRF,
		secureCookies: d.SecureCookies,
		log:           d.Log,
		mux:           http.NewServeMux(),
	}
	if a.log == nil {
		a.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	a.csrfExempt = newRouteSet(append(append([]string{}, defaultCSRFExempt...), d.CSRFExempt...))
	a.authRequired = newRouteSet(authRequiredRoutes)

	a.mux.HandleFunc("GET "+Prefix+"/csrf", a.csrf.Handler())

	create := http.Handler(http.HandlerFunc(a.createTierlist))
	if d.CreateTierlistLimit != nil {
		create = d.CreateTierlistLimit(create)
	}
	a.mux.Handle("POST "+Prefix+"/tierlists", create)
	a.mux.HandleFunc("GET "+Prefix+"/tierlists/{slug}", a.getTierlist)
	a.mux.HandleFunc("PATCH "+Prefix+"/tierlists/{slug}", a.updateTierlist)
	a.mux.HandleFunc("DELETE "+Prefix+"/tierlists/{slug}", a.deleteTierlist)
	a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/close", a.closeTierlist)

	if d.AuthService != nil && d.Sessions != nil && d.Resets != nil {
		limited := func(h http.Handler) http.Handler {
			if d.AuthLimitMiddleware == nil {
				return h
			}
			return d.AuthLimitMiddleware(h)
		}
		// Login and registration are in defaultCSRFExempt: they are public and
		// do not act on an existing session.
		a.mux.Handle("POST "+Prefix+"/auth/register", limited(http.HandlerFunc(a.register)))
		a.mux.Handle("POST "+Prefix+"/auth/login", limited(http.HandlerFunc(a.login)))
		a.mux.HandleFunc("POST "+Prefix+"/auth/logout", a.logout)
		a.mux.Handle("POST "+Prefix+"/auth/logout-all", limited(http.HandlerFunc(a.logoutAll)))
		a.mux.HandleFunc("GET "+Prefix+"/auth/me", a.me)
		a.mux.Handle("POST "+Prefix+"/auth/verify-email", limited(http.HandlerFunc(a.verifyEmail)))
		a.mux.Handle("POST "+Prefix+"/auth/forgot-password", limited(http.HandlerFunc(a.forgotPassword)))
		a.mux.Handle("POST "+Prefix+"/auth/reset-password", limited(http.HandlerFunc(a.resetPassword)))
	}

	a.mux.HandleFunc(Prefix+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	})

	// One CSRF guard around the whole mux: new routes are protected by
	// default, public ones are listed in defaultCSRFExempt/CSRFExempt.
	a.handler = a.withSession(a.guardCSRF(a.mux))
	return a
}

// ServeHTTP implements http.Handler.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.handler.ServeHTTP(w, r) }

// sessionContextKey carries the session loaded once by withSession.
type sessionContextKey struct{}

// withSession resolves the tierl_session cookie once per request and puts the
// session into the context. Both requireUser and CSRF verification reuse it,
// so an authenticated request does not read the session twice.
func (a *API) withSession(next http.Handler) http.Handler {
	if a.sessions == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
			if sess, err := a.sessions.Authenticate(r.Context(), cookie.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sess))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sessionFromContext returns the session loaded by withSession.
func sessionFromContext(ctx context.Context) (*auth.Session, bool) {
	sess, ok := ctx.Value(sessionContextKey{}).(*auth.Session)
	return sess, ok && sess != nil
}

// guardCSRF enforces the token for every unsafe method except the explicit
// public routes. Routes that require authentication pass a guest through so
// the handler answers 401 (there is no session to protect); with a session the
// token is mandatory.
func (a *API) guardCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isUnsafeMethod(r.Method) || a.csrfExempt.has(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := sessionFromContext(r.Context()); !ok && a.authRequired.has(r) {
			if id, signedIn := a.auth.UserID(r); !signedIn || id == "" {
				next.ServeHTTP(w, r)
				return
			}
		}
		if !a.csrf.Verify(r, r.Header.Get(CSRFHeader)) {
			writeError(w, http.StatusForbidden, "csrf_failed", "Неверный CSRF-токен", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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

// requireUser returns the signed-in user or writes 401. The session loaded by
// withSession is reused, so there is no second database read.
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if sess, ok := sessionFromContext(r.Context()); ok {
		return sess.UserID, true
	}
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

// allowByEmail applies the per-email rate limit and writes 429 when exhausted.
func (a *API) allowByEmail(w http.ResponseWriter, _ *http.Request, scope, email string) bool {
	if a.authLimit == nil {
		return true
	}
	d := a.authLimit.Allow(ratelimit.EmailKey(scope, email))
	if d.Allowed {
		return true
	}
	ratelimit.WriteLimited(w, d.RetryAfter)
	return false
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
